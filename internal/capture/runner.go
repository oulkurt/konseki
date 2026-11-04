package capture

// 采样循环与时间轴聚合。
//
// 这一层是平台无关的：它只依赖 Screen / OCREngine 两个接口。
// App 启动时调 Start() 起一个 goroutine，退出时调 Stop()。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Config 是采集器的运行参数。
type Config struct {
	Root      string        // 数据根目录
	Heartbeat time.Duration // 无变化时的取样间隔
	Idle      time.Duration // 多久无输入算闲置
	MinGap    time.Duration // 两次截图的硬性最小间隔
}

// DefaultConfig 给出一组适合日常使用的参数。
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Root:      filepath.Join(home, "Library", "Application Support", "Konseki"),
		Heartbeat: 2 * time.Minute,
		Idle:      5 * time.Minute,
		MinGap:    20 * time.Second,
	}
}

// Runner 是采集循环的运行实例。
type Runner struct {
	cfg Config
	st  *Store
	scr Screen
	ocr OCREngine

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	done    chan struct{}

	// 最近一次采集的摘要，给 UI 轮询显示
	lastLastApp   string
	lastLastTitle string
	lastShotAt    time.Time
	sampleCount   int
	blankStreak   int
	lastErr       error
}

// New 创建一个 Runner。root 为空时用默认路径。
func New(cfg Config) (*Runner, error) {
	if cfg.Root == "" {
		cfg.Root = DefaultConfig().Root
	}
	if cfg.Heartbeat == 0 {
		cfg.Heartbeat = 2 * time.Minute
	}
	if cfg.Idle == 0 {
		cfg.Idle = 5 * time.Minute
	}
	if cfg.MinGap == 0 {
		cfg.MinGap = 20 * time.Second
	}
	st, err := NewStore(cfg.Root)
	if err != nil {
		return nil, err
	}
	return &Runner{
		cfg:  cfg,
		st:   st,
		scr:  newScreen(),
		ocr:  newOCR(),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}, nil
}

// Status 是给 UI 看的采集状态快照。
type Status struct {
	Running    bool
	App        string
	Title      string
	LastShot   time.Time
	Samples    int
	Blank      bool
	Permission PermState
	Err        error
}

// Status 返回当前状态，可安全并发调用。
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{
		Running:    r.running,
		App:        r.lastLastApp,
		Title:      r.lastLastTitle,
		LastShot:   r.lastShotAt,
		Samples:    r.sampleCount,
		Blank:      r.blankStreak >= 3,
		Permission: r.scr.Permission(false),
		Err:        r.lastErr,
	}
}

// Start 启动采样。重复调用无副作用。
func (r *Runner) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	stop, done := r.stop, r.done
	r.mu.Unlock()

	go r.loop(stop, done)
}

// Stop 停止采样并等待循环退出。
func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stop)
	done := r.done
	r.mu.Unlock()
	<-done
}

func (r *Runner) loop(stop, done chan struct{}) {
	defer close(done)

	tick := time.NewTicker(5 * time.Second) // 只查窗口信息，很便宜，不截屏
	defer tick.Stop()

	var lastApp, lastTitle string
	var lastShot time.Time
	idleSince := time.Now()

	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			app, title, pid := r.scr.FrontWindow()
			if app == "" {
				continue // 锁屏或权限缺失
			}

			changed := app != lastApp || title != lastTitle
			if changed {
				idleSince = now
				lastApp, lastTitle = app, title
			}
			if now.Sub(idleSince) > r.cfg.Idle && !changed {
				continue
			}
			if !changed && now.Sub(lastShot) < r.cfg.Heartbeat {
				continue
			}
			if now.Sub(lastShot) < r.cfg.MinGap {
				continue
			}

			r.mu.Lock()
			r.lastLastApp, r.lastLastTitle = app, title
			r.mu.Unlock()

			// 先抓帧（全分辨率，内存里），OCR 用原图，磁盘只存缩略图
			frame := r.scr.Grab()
			if frame == nil {
				continue
			}

			// 黑屏（锁屏 / 显示器休眠 / 权限吊销）：跳过，但不动 lastShot，
			// 屏幕恢复后能立刻补上。
			if frame.Blank {
				r.mu.Lock()
				r.blankStreak++
				r.mu.Unlock()
				continue
			}

			r.mu.Lock()
			r.blankStreak = 0
			r.mu.Unlock()

			shotPath := r.st.ShotPath(now)
			var sizeKB int
			var shotRel string

			t0 := time.Now()
			text, n, ocrErr := r.ocr.Recognize(frame)
			ocrMs := int(time.Since(t0).Milliseconds())
			if ocrErr != nil {
				r.mu.Lock()
				r.lastErr = ocrErr
				r.mu.Unlock()
			}

			if platformSaveThumb(frame, shotPath) {
				if fi, e := os.Stat(shotPath); e == nil {
					sizeKB = int(fi.Size() / 1024)
					shotRel = filepath.Join("shots", filepath.Base(shotPath))
				}
			}

			snap := Snapshot{
				TS: now.Format(time.RFC3339), App: app, Title: title, PID: pid,
				OCR: text, OCRLine: n, Shot: shotRel, ImgKB: sizeKB, OCRMs: ocrMs,
			}
			if err := r.st.Append(snap); err != nil {
				r.mu.Lock()
				r.lastErr = err
				r.mu.Unlock()
			}

			lastShot = now
			r.mu.Lock()
			r.lastShotAt = now
			r.sampleCount++
			r.mu.Unlock()
		}
	}
}

// ---------- 时间轴聚合 ----------

// Block 是一个时间段：同一 App、间隔小于 10 分钟的连续采样归并而成。
type Block struct {
	Start, End time.Time
	App        string
	Titles     []string
	Samples    int
}

// Duration 返回这个时间块的跨度。
func (b Block) Duration() time.Duration { return b.End.Sub(b.Start) }

// Timeline 读某一天的数据并聚合成时间块。
// dateKey 形如 2026-11-04；传空字符串表示今天。
func (r *Runner) Timeline(dateKey string) ([]Block, error) {
	if dateKey == "" {
		dateKey = time.Now().Format("2006-01-02")
	}
	snaps, err := r.st.ReadDay(dateKey)
	if err != nil {
		return nil, err
	}
	return Aggregate(snaps), nil
}

// Aggregates 把原始采样聚合成时间块。
//
// 规则：同一 App 且与上一块的间隔小于 10 分钟，就并入同一块。
// 这样一次连续的工作不会因为中间心跳几次就被切碎。
func Aggregate(snaps []Snapshot) []Block {
	var blocks []Block
	for _, s := range snaps {
		ts, err := parseTS(s.TS)
		if err != nil {
			continue
		}
		if n := len(blocks); n > 0 && blocks[n-1].App == s.App && ts.Sub(blocks[n-1].End) < 10*time.Minute {
			b := &blocks[n-1]
			b.End = ts
			b.Samples++
			if s.Title != "" && (len(b.Titles) == 0 || b.Titles[len(b.Titles)-1] != s.Title) {
				b.Titles = append(b.Titles, s.Title)
			}
			continue
		}
		blocks = append(blocks, Block{Start: ts, End: ts, App: s.App, Titles: []string{s.Title}, Samples: 1})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start.Before(blocks[j].Start) })
	return blocks
}

// parseTS 容忍多种时间格式，避免数据源差异导致整段丢记录。
func parseTS(v string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339, time.RFC3339Nano,
		"2006-01-02T15:04:05", "2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", v)
}

// DoctorReport 返回一份人可读的环境检查结果。
func DoctorReport() string {
	return printPermReportFor(newScreen())
}
