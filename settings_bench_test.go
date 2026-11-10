/*
 * [INPUT]: 依赖无窗口测试器、临时设置目录与固定的一天夹具。
 * [OUTPUT]: 测量设置页三个子页的单帧构建加绘制耗时。
 * [POS]: 设置页的性能测量入口。夹具兼容旧页面基线。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/providers"

	"github.com/egoist/mygo/ui"
)

// 780 宽时四个页面都放得下；旧设置页的截图基线用的也是这个高度。
const settingsBenchHeight = 3600

// benchmarkSettingsApp 用 newTestApp 建应用，再注入旧页面也认得的一份数据。
func benchmarkSettingsApp(tb testing.TB) (*app, func()) {
	tb.Helper()
	a, cleanup := newTestApp(tb)
	// 开关必须落盘：ensureInit 每帧都会用 cfg.Get() 覆盖 cfgNow。
	if err := a.cfg.Update(func(c *config.Config) { c.Recorder.Enabled = true }); err != nil {
		tb.Fatalf("打开记录开关: %v", err)
	}
	a.cfgNow = a.cfg.Get()
	a.blocks = benchBlocks()
	store, err := capture.NewStore(a.runner.Root())
	if err != nil {
		tb.Fatalf("打开存储: %v", err)
	}
	for _, session := range benchSessions() {
		if err := store.AppendSession(session); err != nil {
			tb.Fatalf("写入会话: %v", err)
		}
	}
	a.loadRecentSessions()
	preset, ok := providers.ByID("deepseek")
	if !ok {
		tb.Fatal("预设 deepseek 不存在")
	}
	a.addProvider(preset)
	// addProvider 会展开新加的供应商；旧页面没有这条规则，统一收起。
	a.openProvider = ""
	// 冻结 refresh 的节流窗口，只测视图构建与绘制。
	a.lastLoad = time.Now().Add(time.Hour)
	return a, cleanup
}

// countingView 记录视图被调用的次数，用来证明每次迭代都真的重建了视图。
type countingView struct {
	inner func(*ui.Context)
	calls int
}

func (v *countingView) render(c *ui.Context) {
	v.calls++
	v.inner(c)
}

func benchmarkSettingsFrame(b *testing.B, settingsTab int) {
	b.Helper()
	a, cleanup := benchmarkSettingsApp(b)
	defer cleanup()
	a.tab, a.settingsTab = 1, settingsTab
	counter := &countingView{inner: a.view}
	tt := ui.NewTester(counter.render, 780, settingsBenchHeight)
	tt.Frame() // 预热字体与布局，不计入样本。
	base := counter.calls
	b.ResetTimer()
	for range b.N {
		tt.Frame()
	}
	b.StopTimer()
	if got := counter.calls - base; got != b.N {
		b.Fatalf("视图重建 %d 次，迭代 %d 次", got, b.N)
	}
}

func BenchmarkP3SettingsRecorder(b *testing.B) { benchmarkSettingsFrame(b, 0) }
func BenchmarkP3SettingsModels(b *testing.B)   { benchmarkSettingsFrame(b, 1) }
func BenchmarkP3SettingsGeneral(b *testing.B)  { benchmarkSettingsFrame(b, 2) }

// TestSettingsBenchPages 证明夹具让三个子页都渲染出各自的标记内容。
func TestSettingsBenchPages(t *testing.T) {
	cases := []struct {
		tab   int
		texts []string
	}{
		{0, []string{"活动记录器", "会话数", "最近会话", "清除全部活动数据"}},
		{1, []string{"摘要模型", "供应商"}},
		{2, []string{"语言", "Agent技能"}},
	}
	for _, tc := range cases {
		a, cleanup := benchmarkSettingsApp(t)
		a.tab, a.settingsTab = 1, tc.tab
		tt := ui.NewTester(a.view, 780, settingsBenchHeight)
		tt.Frame()
		for _, text := range tc.texts {
			if !tt.HasText(text) {
				t.Errorf("子页 %d 缺少 %q", tc.tab, text)
			}
		}
		cleanup()
	}
}

func benchBlocks() []capture.Block {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 4, h, m, 0, 0, time.Local) }
	return []capture.Block{
		{Start: at(9, 0), End: at(9, 26), App: "T3 Code", Titles: []string{"P0 地基：类型冻结", "审查 Terra 的提交"}, Samples: 9},
		{Start: at(10, 0), End: at(10, 3), App: "微信", Titles: []string{"文件传输助手"}, Samples: 1},
		{Start: at(10, 4), End: at(10, 8), App: "Alma", Titles: []string{"Activity Recorder"}, Samples: 2},
		{Start: at(11, 15), End: at(12, 2), App: "Dia", Titles: []string{"mygo/docs/i18n.md · GitHub"}, Samples: 12},
		{Start: at(13, 40), End: at(14, 5), App: "Editor", Titles: []string{"receipt.go - 小票时间轴，出纸口与送纸动画的第一版实现，标题够长才能看出换行"}, Samples: 6},
		{Start: at(15, 10), End: at(15, 32), App: "Konseki", Titles: []string{"痕跡 Konseki"}, Samples: 4},
	}
}

func benchSessions() []capture.Session {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 4, h, m, 0, 0, time.Local) }
	return []capture.Session{
		{ID: "bench-1", Start: at(9, 0), End: at(9, 26), EndReason: capture.EndIdle, Apps: []string{"T3 Code"}, Snapshots: 9},
		{ID: "bench-2", Start: at(11, 15), End: at(12, 2), EndReason: capture.EndIdle, Apps: []string{"Dia"}, Snapshots: 12},
		{ID: "bench-3", Start: at(13, 40), End: at(14, 5), EndReason: capture.EndIdle, Apps: []string{"Editor"}, Snapshots: 6},
	}
}
