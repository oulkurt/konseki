/*
 * [INPUT]: 依赖采集、会话存储、配置、摘要服务和界面错误边界。
 * [OUTPUT]: 提供应用入口、刷新缓存与后台摘要。
 * [POS]: 应用组合根。入口先分流只读查询命令并维护命令软链接，再启动窗口；刷新后缓存小票供视图读取。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"log"
	"os"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"konseki/internal/skill"
	"konseki/internal/summary"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type app struct {
	tasks                appTasks
	runner               *capture.Runner
	appDir               string
	post                 func(func())
	setLocale            func(string)
	scanCredentials      func() []providers.Credential
	requestScreen        func() capture.PermState
	requestAccessibility func() capture.PermState
	openExternal         func(string) error
	showFolder           func(string)
	legacySkillDir       func() string
	runningApps          func() []capture.Front
	running              runningAppsCache
	linkErr              error

	cfg    *config.Store
	cfgNow config.Config
	sums   *summary.Store
	gen    *summary.Generator
	llmc   *llm.Client

	blocks          []capture.Block
	status          capture.Status
	lastLoad        time.Time
	err             error
	readErr         error
	summaryReadErr  error
	scrollState     ui.ScrollState
	expanded        map[string]bool
	dayFilter       string
	now             func() time.Time
	receipt         ReceiptDay
	receiptStore    *capture.Store
	receiptStoreDir string
	receiptRevision int
	receiptTarget   string
	paperFeed       paperFeedState
	printReveal     printRevealState

	openProvider    string
	addProviderOpen bool
	wasAddOpen      bool
	providerSearch  string
	apiKeyDraft     string
	baseURLDraft    string
	modelDraft      string
	providerTest    providerTestState
	credScan        credScanState
	fetchState      fetchModelsState

	tab             int
	settingsTab     int
	lastTab         int
	lastSettingsTab int
	navInit         bool
	drafts          editDrafts

	sumEntries     []summary.Entry
	summary        summaryJobState
	clear          clearSaga
	shred          shredState
	hasSummaries   bool
	recentSessions []recentSession
	lastSessions   time.Time

	legacy        legacyState
	agentCopiedKey string
	agentCopiedAt  time.Time
}

func (a *app) legacyDir() string {
	if a.cfgNow.Skill.ExportDir != "" {
		return a.cfgNow.Skill.ExportDir
	}
	if a.legacySkillDir != nil {
		return a.legacySkillDir()
	}
	return skill.LegacyDir()
}

func (a *app) resetLegacy() {
	a.legacy = legacyState{}
}

func (a *app) ensureInit() {
	if a.expanded == nil {
		a.expanded = map[string]bool{}
	}
	if a.cfg != nil {
		a.cfgNow = a.cfg.Get()
	}
}

func (a *app) refresh() {
	if a.clear.running() {
		return
	}
	a.ensureInit()
	now := a.currentTime()
	if !a.lastLoad.IsZero() && now.Sub(a.lastLoad) < 2*time.Second {
		return
	}
	a.lastLoad = now
	if a.runner == nil {
		return
	}
	a.status = a.runner.Status()
	a.loadDay()
	a.loadSummaries()
	if a.sums != nil {
		a.hasSummaries = len(a.sums.Days()) > 0
	}
	a.loadRecentSessions()
	root := a.runner.Root()
	if a.receiptStore == nil || a.receiptStoreDir != root {
		store, err := capture.OpenStore(root)
		if err != nil {
			a.readErr = err
		} else {
			a.receiptStore = store
			a.receiptStoreDir = root
		}
	}
	var sessions []capture.Session
	if a.receiptStore != nil {
		var err error
		sessions, err = a.receiptStore.ReadSessions(a.dayKey())
		if err != nil && !os.IsNotExist(err) {
			a.readErr = err
		}
	}
	day, err := time.ParseInLocation("2006-01-02", a.dayKey(), time.Local)
	if err != nil {
		day = now
	}
	next := buildReceiptWindow(day, a.sumEntries, a.blocks, sessions, time.Duration(a.cfgNow.Summary.WindowMinutes)*time.Minute)
	if a.receipt.Day.Equal(next.Day) && len(next.Lines) > 0 && (len(a.receipt.Lines) == 0 || (next.Lines[0].ID != a.receipt.Lines[0].ID && next.Lines[0].Start.After(a.receipt.Lines[0].Start))) {
		a.receiptRevision++
	}
	a.receipt = next
}

func (a *app) loadDay() {
	a.loadDayFor(a.dayKey())
}

func (a *app) loadDayFor(key string) {
	if a.runner == nil {
		return
	}
	blocks, err := a.runner.Timeline(key)
	if err != nil {
		if os.IsNotExist(err) {
			a.readErr = nil
		} else {
			a.readErr = err
		}
		a.blocks = nil
		return
	}
	a.readErr = nil
	a.blocks = blocks
}

func (a *app) loadSummaries() {
	a.loadSummariesFor(a.dayKey())
}

func (a *app) loadSummariesFor(key string) {
	if a.sums == nil {
		return
	}
	ents, err := a.sums.Load(key)
	if err != nil {
		a.summaryReadErr = err
		a.sumEntries = nil
		return
	}
	a.summaryReadErr = nil
	a.sumEntries = ents
}

func (a *app) dayKey() string {
	if a.dayFilter != "" {
		return a.dayFilter
	}
	return a.currentTime().Format("2006-01-02")
}

func (a *app) todayKey() string {
	return a.currentTime().Format("2006-01-02")
}

func (a *app) currentTime() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *app) dayLabel() string {
	return a.dayKey()
}

func (a *app) summarizable() bool {
	return a.gen != nil && len(a.blocks) > 0 && !a.summary.running()
}

func (a *app) runSummary(force bool) {
	a.runSummaryForDay(a.dayKey(), a.blocks, force)
}

func (a *app) runSummaryForDay(key string, blocks []capture.Block, force bool) {
	if a.gen == nil || a.summary.running() || a.clear.running() || a.tasks.isClosed() {
		return
	}
	a.ensureInit()
	epoch, ctx, cancel, done := a.summary.start(a.tasks.ensure(), key, 15*time.Minute)
	gen := a.gen
	window := time.Duration(a.cfgNow.Summary.WindowMinutes) * time.Minute
	if window <= 0 {
		window = time.Hour
	}

	ok := a.tasks.goTask(func(context.Context) {
		defer close(done)
		defer cancel()
		ocr, err := a.runner.OCRIndex(key)
		if err != nil {
			ocr = nil
		}

		entries, genErr := gen.GenerateDay(ctx, blocks, ocr, key, window, force)
		if genErr == nil && !force && a.sums != nil {
			if full, loadErr := a.sums.Load(key); loadErr == nil {
				entries = full
			}
		}
		a.postUI(func() {
			if !a.summary.finish(epoch, genErr) {
				return
			}
			if genErr != nil {
				return
			}
			if a.sums != nil {
				a.hasSummaries = len(a.sums.Days()) > 0
			}
			a.lastSessions = time.Time{}
			if a.dayKey() == key {
				a.sumEntries = entries
				a.summaryReadErr = nil
				a.lastLoad = time.Time{}
			}
		})
	})
	if !ok {
		a.summary.cancelActive()
		close(done)
	}
}

func main() {
	if code, ok := runCLI(os.Args[1:], os.Stdout, os.Stderr); ok {
		os.Exit(code)
	}
	appDir := config.AppDir()
	cfg, err := config.Open(appDir)
	if err != nil {
		log.Fatal(err)
	}
	current := cfg.Get()
	linkErr := linkExecutable(appDir)
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
		runner:               runner,
		appDir:               appDir,
		cfg:                  cfg,
		cfgNow:               cfg.Get(),
		sums:                 sums,
		llmc:                 llm.New(5 * time.Minute),
		expanded:             map[string]bool{},
		scanCredentials:      providers.DetectLocal,
		setLocale:            mygo.App.SetLocale,
		requestScreen:        capture.RequestScreenPermission,
		requestAccessibility: capture.RequestAccessibility,
		openExternal:         mygo.Shell.OpenExternal,
		showFolder:           mygo.Shell.ShowItemInFolder,
		legacySkillDir:       skill.LegacyDir,
		runningApps:          capture.RunningApps,
		linkErr:              linkErr,
	}
	a.rebuildGenerator()

	a.tasks.goTask(func(context.Context) {
		_, _ = runner.Storage().Prune(current.Recorder.RetentionDays, time.Now())
	})

	mygo.App.WhenReady(func() {
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
		a.tasks.goTask(a.autoSummaryLoop)
	})

	if err := mygo.App.Run(); err != nil {
		a.shutdown()
		log.Fatal(err)
	}
	a.shutdown()
}

func summaryInterval(s config.SummaryConfig) time.Duration {
	if interval := time.Duration(s.IntervalMinutes) * time.Minute; interval >= 5*time.Minute {
		return interval
	}
	return 5 * time.Minute
}

func (a *app) autoSummaryLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	lastRun := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		a.postUI(func() {
			if ctx.Err() != nil {
				return
			}
			a.ensureInit()
			if !a.cfgNow.Summary.Auto || a.summary.running() || a.clear.running() || a.gen == nil {
				return
			}
			if !a.cfgNow.Recorder.Enabled {
				return
			}
			now := a.currentTime()
			if !lastRun.IsZero() && now.Sub(lastRun) < summaryInterval(a.cfgNow.Summary) {
				return
			}
			today := a.todayKey()
			blocks, err := a.runner.Timeline(today)
			if err != nil || len(blocks) == 0 {
				return
			}
			var entries []summary.Entry
			if a.sums != nil {
				entries, err = a.sums.Load(today)
				if err != nil {
					return
				}
			}
			window := time.Duration(a.cfgNow.Summary.WindowMinutes) * time.Minute
			if window <= 0 {
				window = time.Hour
			}
			if !summary.HasPendingWindows(today, blocks, entries, window) {
				return
			}
			lastRun = now
			a.runSummaryForDay(today, blocks, false)
		})
	}
}
