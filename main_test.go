/*
 * [INPUT]: 依赖临时应用存储和 mygo 测试器。
 * [OUTPUT]: 验证应用视图、录制控制与采集聚合。
 * [POS]: 应用层无窗口测试及共享测试构造器。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"konseki/internal/summary"

	"github.com/egoist/mygo/ui"
)

// 视图能在没有窗口的情况下构建，这是 mygo native 模板的核心好处：
// 界面逻辑可以纯查测试，不用起 GUI。

func newTestApp(t *testing.T) (*app, func()) {
	t.Helper()
	appDir := t.TempDir()
	cs, err := config.Open(appDir)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	r, err := capture.NewWithDependencies(recorderSettings(cs.Get().Recorder, appDir), inertCapture())
	if err != nil {
		t.Fatalf("capture.NewWithDependencies: %v", err)
	}
	sums, err := summary.NewStore(appDir)
	if err != nil {
		t.Fatalf("summary.NewStore: %v", err)
	}
	a := &app{
		runner:   r,
		appDir:   appDir,
		cfg:      cs,
		cfgNow:   cs.Get(),
		sums:     sums,
		llmc:     llm.New(time.Second),
		expanded: map[string]bool{},
	}
	a.post = func(f func()) { f() }
	a.setLocale = func(string) {}
	scanTestCredentials(t, a, nil)
	a.rebuildGenerator()
	return a, func() { r.Stop() }
}

func scanTestCredentials(t *testing.T, a *app, creds []providers.Credential) {
	t.Helper()
	queued := make(chan func(), 1)
	a.post = func(f func()) { queued <- f }
	a.scanCredentials = func() []providers.Credential { return creds }
	a.detectStarted, a.detectDone = false, false
	a.startCredentialScan()
	select {
	case f := <-queued:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("凭据扫描超时")
	}
	a.post = func(f func()) { f() }
}

func TestViewBuilds(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	tt := ui.NewTester(a.view, 640, 560)
	if !tt.HasText("痕跡") {
		t.Errorf("标题没画出来，texts=%q", tt.Texts())
	}
	if !tt.HasText("已关闭") {
		t.Errorf("未启动时应该显示关闭状态，texts=%q", tt.Texts())
	}
}

func TestEmptyState(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	tt := ui.NewTester(a.view, 640, 560)
	if !tt.HasText("今天还没有记录") {
		t.Errorf("空状态提示缺失，texts=%q", tt.Texts())
	}
}

// 聚合逻辑是纯函数，单独测，不依赖平台。
func TestAggregate(t *testing.T) {
	base := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	mk := func(min int, app, title string) capture.Snapshot {
		return capture.Snapshot{
			TS:    base.Add(time.Duration(min) * time.Minute).Format(time.RFC3339),
			App:   app,
			Title: title,
		}
	}

	t.Run("同一 App 连续采样归并成一块", func(t *testing.T) {
		blocks := capture.Aggregate([]capture.Snapshot{
			mk(0, "A", "t1"),
			mk(3, "A", "t1"),
			mk(6, "A", "t2"),
		})
		if len(blocks) != 1 {
			t.Fatalf("期望 1 块，得到 %d", len(blocks))
		}
		if blocks[0].Samples != 3 {
			t.Errorf("Samples = %d，期望 3", blocks[0].Samples)
		}
		if len(blocks[0].Titles) != 2 {
			t.Errorf("标题去重后应为 2 条，得到 %v", blocks[0].Titles)
		}
	})

	t.Run("换 App 切块", func(t *testing.T) {
		blocks := capture.Aggregate([]capture.Snapshot{
			mk(0, "A", ""),
			mk(2, "B", ""),
		})
		if len(blocks) != 2 {
			t.Fatalf("期望 2 块，得到 %d", len(blocks))
		}
	})

	t.Run("间隔超过 10 分钟切块", func(t *testing.T) {
		blocks := capture.Aggregate([]capture.Snapshot{
			mk(0, "A", ""),
			mk(11, "A", ""),
		})
		if len(blocks) != 2 {
			t.Fatalf("期望 2 块，得到 %d", len(blocks))
		}
	})

	t.Run("空输入", func(t *testing.T) {
		if got := capture.Aggregate(nil); len(got) != 0 {
			t.Errorf("期望 0 块，得到 %d", len(got))
		}
	})
}

func TestRecorderControlsPersist(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	tt := ui.NewTester(a.view, 760, 1400)
	if err := tt.Click("已关闭"); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase == capture.PhaseOff {
		t.Fatal("顶栏未启动采集")
	}
	if err := tt.Click("记录中"); err != nil {
		t.Fatal(err)
	}
	if a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase != capture.PhaseOff {
		t.Fatal("顶栏未停止采集")
	}
	a.tab = 1
	tt = ui.NewTester(a.view, 760, 1400)
	rect, ok := tt.Find("Enable Context Awareness")
	if !ok {
		t.Fatal("设置开关缺失")
	}
	tt.ClickAt(700, rect.Y+12)
	if !a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase == capture.PhaseOff {
		t.Fatal("设置开关未启动采集")
	}
	reopened, err := config.Open(a.appDir)
	if err != nil || !reopened.Get().Recorder.Enabled {
		t.Fatalf("开关未落盘: %v", err)
	}
}

// inertFake 是界面测试用的采集依赖：时钟不走、没有输入、抓不到屏幕。
// 界面测试只关心开关和状态，绝不能启动真实采集去抓屏或弹授权框。
type inertFake struct{}

func (inertFake) FrontWindow() (string, string, int)                      { return "", "", 0 }
func (inertFake) Grab() *capture.Frame                                    { return nil }
func (inertFake) Permission(bool) capture.PermState                       { return capture.PermGranted }
func (inertFake) Recognize(*capture.Frame, []string) (string, int, error) { return "", 0, nil }
func (inertFake) Intervals() (time.Duration, time.Duration, time.Duration) {
	return time.Hour, time.Hour, time.Hour
}
func (inertFake) Front() capture.Front              { return capture.Front{} }
func (inertFake) Locked() bool                      { return false }
func (inertFake) Now() time.Time                    { return time.Now() }
func (inertFake) Ticks() (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
func (inertFake) Accessibility() capture.PermState  { return capture.PermGranted }

func inertCapture() capture.Dependencies {
	f := inertFake{}
	return capture.Dependencies{
		Screen: f, OCR: f, Input: f, Front: f, Lock: f, Clock: f, Permissions: f,
		SaveJPEG: func(*capture.Frame, string, int) bool { return false },
	}
}
