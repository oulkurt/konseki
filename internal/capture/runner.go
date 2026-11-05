/*
 * [INPUT]: 依赖平台采集接口、运行设置和事实存储。
 * [OUTPUT]: 提供 Runner、状态快照与时间轴聚合。
 * [POS]: 采集循环的协调边界，隔离平台与界面。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
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
	"strings"
	"sync"
	"time"
)

// Runner 是采集循环的运行实例。
type Runner struct {
	cfg         Settings
	st          *Store
	storage     *Storage
	pausedUntil time.Time
	scr         Screen
	ocr         OCREngine

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	done    chan struct{}

	// 最近一次采集的摘要，给 UI 轮询显示
	lastLastApp   string
	lastLastTitle string
	lastPID       int
	lastShotAt    time.Time
	sampleCount   int
	blankStreak   int
	lastErr       error
}

// New 创建采集循环和存储句柄。
func New(cfg Settings) (*Runner, error) {
	st, err := NewStore(cfg.Root)
	if err != nil {
		return nil, err
	}
	r := &Runner{st: st, storage: NewStorage(cfg.Root), scr: newScreen(), ocr: newOCR()}
	r.Apply(cfg)
	return r, nil
}

func (r *Runner) Apply(s Settings) {
	s.SensitiveApps = append([]string(nil), s.SensitiveApps...)
	s.OCR.Languages = append([]string(nil), s.OCR.Languages...)
	r.mu.Lock()
	r.cfg = s
	r.mu.Unlock()
}

func (r *Runner) Storage() *Storage     { return r.storage }
func (r *Runner) Pause(until time.Time) { r.mu.Lock(); r.pausedUntil = until; r.mu.Unlock() }
func (r *Runner) Resume()               { r.Pause(time.Time{}) }
func (r *Runner) paused(now time.Time) bool {
	if !r.pausedUntil.IsZero() && !now.Before(r.pausedUntil) {
		r.pausedUntil = time.Time{}
	}
	return !r.pausedUntil.IsZero()
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	phase := PhaseOff
	block := BlockNone
	if r.running {
		if r.paused(time.Now()) {
			phase = PhasePaused
		} else if r.blankStreak >= 3 {
			phase = PhaseBlocked
			block = BlockScreenLocked
		} else {
			phase = PhaseActive
		}
	}
	return Status{
		Phase:       phase,
		Block:       block,
		PausedUntil: r.pausedUntil,
		Front:       Front{App: r.lastLastApp, Title: r.lastLastTitle, PID: r.lastPID},
		Usage:       r.storage.Usage(),
		Perms:       Perms{Screen: r.scr.Permission(false), Accessibility: PermUnknown},
		LastShot:    r.lastShotAt,
		LastErr:     r.lastErr,
	}
}

func (r *Runner) RecentSessions(n int) []Session {
	if n <= 0 {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(r.st.root, "sessions"))
	if err != nil {
		return nil
	}
	// 会话按结束日期分文件，文件名排序即时间顺序。从最新的一天往回读，
	// 凑够 n 条就停，不必每次读完全部历史。
	var result []Session
	for i := len(entries) - 1; i >= 0 && len(result) < n; i-- {
		e := entries[i]
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		rows, err := r.st.ReadSessions(strings.TrimSuffix(e.Name(), ".jsonl"))
		if err == nil {
			result = append(result, rows...)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].End.After(result[j].End) })
	if len(result) > n {
		result = result[:n]
	}
	return result
}

func (r *Runner) ClearAll() (Cleared, error) { return r.storage.ClearAll() }

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
			r.mu.Lock()
			paused := r.paused(now)
			heartbeat := r.cfg.Heartbeat
			r.mu.Unlock()
			if paused {
				continue
			}
			app, title, pid := r.scr.FrontWindow()
			r.mu.Lock()
			r.lastLastApp, r.lastLastTitle, r.lastPID = app, title, pid
			r.mu.Unlock()
			if app == "" {
				continue // 锁屏或权限缺失
			}

			changed := app != lastApp || title != lastTitle
			if changed {
				idleSince = now
				lastApp, lastTitle = app, title
			}
			if now.Sub(idleSince) > 5*time.Minute && !changed {
				continue
			}
			if !changed && now.Sub(lastShot) < heartbeat {
				continue
			}
			if now.Sub(lastShot) < 20*time.Second {
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

// OCRIndex 暴露存储层的 OCR 索引，供摘要层取原文。
func (r *Runner) OCRIndex(dateKey string) (map[string]string, error) {
	return r.st.OCRIndex(dateKey)
}

// Snapshots 读回某天的原始采样。
func (r *Runner) Snapshots(dateKey string) ([]Snapshot, error) {
	return r.st.ReadDay(dateKey)
}

// Root 返回数据根目录，导出 skill 时要用。
func (r *Runner) Root() string { r.mu.Lock(); defer r.mu.Unlock(); return r.cfg.Root }
