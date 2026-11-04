package main

import (
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/summary"

	"github.com/egoist/mygo/ui"
)

// 视图能在没有窗口的情况下构建，这是 mygo native 模板的核心好处：
// 界面逻辑可以纯查测试，不用起 GUI。

func newTestApp(t *testing.T) (*app, func()) {
	t.Helper()
	ccfg := capture.DefaultConfig()
	ccfg.Root = t.TempDir()
	r, err := capture.New(ccfg)
	if err != nil {
		t.Fatalf("capture.New: %v", err)
	}
	cs, err := config.Open(ccfg.Root)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	sums, err := summary.NewStore(ccfg.Root)
	if err != nil {
		t.Fatalf("summary.NewStore: %v", err)
	}
	a := &app{
		runner:   r,
		cfg:      cs,
		cfgNow:   cs.Get(),
		sums:     sums,
		llmc:     llm.New(time.Second),
		expanded: map[string]bool{},
	}
	a.rebuildGenerator()
	return a, func() { r.Stop() }
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
