/*
 * [INPUT]: 依赖采集、配置和摘要服务。
 * [OUTPUT]: 提供应用入口与后台摘要。
 * [POS]: 应用组合根。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"konseki/internal/summary"
	"log"
	"os"
	"time"
)

type app struct {
	runner          *capture.Runner
	appDir          string
	post            func(func())
	scanCredentials func() []providers.Credential

	// ---- 持久层 ----
	cfg    *config.Store
	cfgNow config.Config
	sums   *summary.Store
	gen    *summary.Generator
	llmc   *llm.Client

	// ---- 界面与采集状态 ----
	blocks      []capture.Block
	status      capture.Status
	doctor      string
	showDiag    bool
	lastLoad    time.Time
	err         string
	listState   ui.ListState
	scrollState ui.ScrollState
	expanded    map[string]bool
	dayFilter   string

	// ---- 供应商 ----
	// 当前展开的是哪个供应商的配置面板，空表示都收起。
	openProvider    string
	addProviderOpen bool
	providerFilter  int // 0=全部 1=官方 2=中转 3=本地 4=订阅
	apiKeyDraft     string
	baseURLDraft    string
	modelDraft      string
	testing         bool
	testResult      string
	testOK          bool
	detected        []providers.Credential
	detectDone      bool
	detectStarted   bool
	fetchingModels  bool
	fetchMsg        string

	// ---- 摘要 ----
	// 视图切换：0 = 设置（Capture/Summaries/Providers/Skill），1 = 时间轴
	tab        int
	sumEntries []summary.Entry
	sumErr     string
	sumRunning bool
	// 摘要生成时的进度文字，UI 上直接显示。
	sumProgress string
	// 本次生成的脱敏命中数，给用户一个「遮了多少」的反馈。
	sumRedactions int
	showRedactLog bool

	// ---- Skill ----
	skillExportMsg  string
	skillExportedAt time.Time
}

func (a *app) ensureInit() {
	if a.expanded == nil {
		a.expanded = map[string]bool{}
	}
	if a.dayFilter == "" {
		a.dayFilter = "Today"
	}
	if a.cfg != nil {
		a.cfgNow = a.cfg.Get()
	}
}

// refresh 重新读一次数据和状态。每帧调用，但内部按 2 秒节流，
// 避免每秒都在扫 JSONL。
func (a *app) refresh() {
	a.ensureInit()
	if time.Since(a.lastLoad) < 2*time.Second {
		return
	}
	a.lastLoad = time.Now()
	if a.runner == nil {
		return
	}
	a.status = a.runner.Status()
	a.loadDay()
	a.loadSummaries()
}

// loadDay 读回当天的时间块。
func (a *app) loadDay() {
	key := a.dayKey()
	blocks, err := a.runner.Timeline(key)
	if err != nil {
		if !os.IsNotExist(err) {
			a.err = err.Error()
		}
		a.blocks = nil
		return
	}
	a.err = ""
	a.blocks = blocks
}

// loadSummaries 读回当天的摘要。
func (a *app) loadSummaries() {
	if a.sums == nil {
		return
	}
	ents, err := a.sums.Load(a.dayKey())
	if err != nil {
		a.sumErr = err.Error()
		return
	}
	a.sumErr = ""
	a.sumEntries = ents
}

// dayKey 把 UI 上的筛选词换成日期。
func (a *app) dayKey() string {
	switch a.dayFilter {
	case "Yesterday":
		return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	case "Last 7 Days", "Today":
		return time.Now().Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}

// dayLabel 给摘要生成用：昨天生成昨天，其余都当今天。
func (a *app) dayLabel() string {
	return a.dayKey()
}

// ---------- 摘要生成 ----------

// summarizable 说现在能不能生成摘要。UI 用它决定按钮是否置灰。
func (a *app) summarizable() bool {
	return a.gen != nil && len(a.blocks) > 0 && !a.sumRunning
}

// runSummary 在后台生成摘要，并在界面线程发布结果。
func (a *app) runSummary(force bool) {
	if a.gen == nil || a.sumRunning {
		return
	}
	a.sumRunning = true
	a.sumErr = ""
	a.showRedactLog = false

	key := a.dayKey()
	blocks := a.blocks
	root := a.runner.Root()
	gen := a.gen
	window := time.Duration(a.cfgNow.Summary.WindowMinutes) * time.Minute
	if window <= 0 {
		window = time.Hour
	}

	go func() {
		ocr, err := a.runner.OCRIndex(key)
		if err != nil {
			ocr = nil // OCR 拿不到也能生成，靠窗口标题
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		entries, err := gen.GenerateDay(ctx, blocks, ocr, key, window, force)
		redactions := 0
		if m := gen.Masker(); m != nil {
			redactions = len(m.Hits())
		}
		a.post(func() {
			a.sumRunning = false
			a.sumRedactions = redactions
			if err != nil {
				a.sumErr = err.Error()
				return
			}
			a.sumEntries = entries
			a.stampSkill(root)
		})
	}()
}

// stampSkill 在摘要变化后重新导出 skill。
//
// 这是让外部 Agent 真能读到东西的关键：摘要生成完但没导出，
// 别的工具就还是看不到。导出本身很快（纯写文件），不值得再起
// 一个 goroutine。
func (a *app) stampSkill(root string) {
	_ = root // 保留参数是为了以后按数据目录分桶导出；现在 exporter 自己取路径
	if a.cfgNow.Skill.ExportDir == "" {
		return
	}
	if n, err := a.exportSkill(); err == nil {
		a.skillExportMsg = fmt.Sprintf("已导出 %d 个文件", n)
		a.skillExportedAt = time.Now()
	} else {
		a.skillExportMsg = "导出失败：" + err.Error()
	}
}

func main() {
	appDir := config.AppDir()
	cfg, err := config.Open(appDir)
	if err != nil {
		log.Fatal(err)
	}
	current := cfg.Get()
	mygo.App.SetLocale(current.Language)
	runner, err := capture.New(recorderSettings(current.Recorder, appDir))
	if err != nil {
		log.Fatal(err)
	}
	runner.Pause(current.Recorder.PausedUntil)
	sums, err := summary.NewStore(appDir)
	if err != nil {
		log.Fatal(err)
	}

	a := &app{
		runner:          runner,
		appDir:          appDir,
		cfg:             cfg,
		cfgNow:          cfg.Get(),
		sums:            sums,
		llmc:            llm.New(5 * time.Minute),
		expanded:        map[string]bool{},
		scanCredentials: providers.DetectLocal,
	}
	a.rebuildGenerator()

	// 启动时就清一次过期数据。放在 goroutine 里是因为它要扫目录，
	// 而窗口应该在几百毫秒内出现。
	go func() { _, _ = runner.Storage().Prune(current.Recorder.RetentionDays, time.Now()) }()

	mygo.App.WhenReady(func() {
		warmOSAppIcons()
		if current.Recorder.Enabled {
			runner.Start()
		}

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "痕跡 Konseki",
			TitleBarStyle:   mygo.TitleBarHidden,
			Transparent:     true,
			Vibrancy:        mygo.VibrancyGlass,
			BackgroundColor: "light-dark(#f7f8fa, #0f1115)",
			Width:           780,
			Height:          960,
			MinWidth:        560,
			MinHeight:       520,
			StateKey:        "main",
			Content:         ui.View(a.view),
		})
		a.post = win.Update
		go a.autoSummaryLoop()
	})

	// 自动摘要。走一个独立的 ticker，和采集循环无关：
	// 采集是 5 秒一次的热路径，摘要要跑几分钟，混在一起会让
	// 采样被拖慢。
	//
	// 无条件启动，让循环自己读开关：用户在设置页打开 Auto 之后
	// 不该要求重启 App 才生效。
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	runner.Stop()
}

// autoSummaryLoop 按设置里的间隔自动补摘要。
//
// 只在「有新的采样、且距上次生成超过间隔」时才跑。空转一次要
// 花一次模型的 token，白花钱还会让用户怀疑它在偷跑。
func (a *app) autoSummaryLoop() {
	// tick 用固定的 1 分钟，真正该跑的间隔在循环里按设置判断。
	//
	// 用 Ticker 而不是 time.Sleep(interval)：用户把间隔从 30 分钟
	// 改成 5 分钟时，睡着的 goroutine 叫不醒，得等那一觉睡完。
	// 1 分钟一次的检查几乎不花代价（只读几个字段）。
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	lastRun := time.Time{}

	for range tick.C {
		a.post(func() {
			a.ensureInit()
			if !a.cfgNow.Summary.Auto || a.sumRunning || a.gen == nil {
				return
			}
			if !a.cfgNow.Recorder.Enabled {
				return
			}

			interval := time.Duration(a.cfgNow.Summary.IntervalMinutes) * time.Minute
			if interval < 5*time.Minute {
				interval = 5 * time.Minute
			}
			if !lastRun.IsZero() && time.Since(lastRun) < interval {
				return
			}

			a.loadDay()
			if len(a.blocks) == 0 {
				return
			}
			// 已经覆盖到最新一块就不用再跑了 —— 没有新内容时跑一次
			// 只是白烧 token。
			if a.coversLatest() {
				return
			}
			lastRun = time.Now()
			a.runSummary(false)
		})
	}
}

// coversLatest 说最后一小时是否已经有摘要了。
func (a *app) coversLatest() bool {
	if len(a.sumEntries) == 0 || len(a.blocks) == 0 {
		return false
	}
	lastBlock := a.blocks[len(a.blocks)-1].End
	for _, e := range a.sumEntries {
		if !e.End.Before(lastBlock) {
			return true
		}
	}
	return false
}
