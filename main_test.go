/*
 * [INPUT]: 依赖临时应用存储和 mygo 测试器。
 * [OUTPUT]: 验证应用视图与时间轴聚合。
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
	r, err := capture.New(recorderSettings(cs.Get().Recorder, appDir))
	if err != nil {
		t.Fatalf("capture.New: %v", err)
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
	if !tt.HasText("开始记录") {
		t.Errorf("未启动时应该有「开始记录」按钮，texts=%q", tt.Texts())
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

func TestFmtDur(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "<1m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h30m"},
		{2 * time.Hour, "2h0m"},
	}
	for _, c := range cases {
		if got := fmtDur(c.in); got != c.want {
			t.Errorf("fmtDur(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestRecorderControlsPersist(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	tt := ui.NewTester(a.view, 760, 1400)
	if err := tt.Click("开始记录"); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase != capture.PhaseActive {
		t.Fatal("顶栏未启动采集")
	}
	if err := tt.Click("暂停"); err != nil {
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
	if !a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase != capture.PhaseActive {
		t.Fatal("设置开关未启动采集")
	}
	reopened, err := config.Open(a.appDir)
	if err != nil || !reopened.Get().Recorder.Enabled {
		t.Fatalf("开关未落盘: %v", err)
	}
}
