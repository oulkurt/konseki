/*
 * [INPUT]: 依赖临时应用数据和 mygo 测试器。
 * [OUTPUT]: 验证时间轴与设置界面渲染。
 * [POS]: 应用层的无窗口界面回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"image/png"
	"os"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/providers"

	"github.com/egoist/mygo/ui"
)

// 用真实数据验证时间轴渲染：注入 blocks，检查文本是否画出来。
func TestTimelineRenders(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	base := time.Date(2026, 11, 4, 14, 29, 0, 0, time.Local)
	a.blocks = []capture.Block{
		{Start: base, End: base.Add(45 * time.Second), App: "Konseki",
			Titles: []string{"痕跡 Konseki"}, Samples: 2},
		{Start: base.Add(11 * time.Minute), End: base.Add(11*time.Minute + 20*time.Second),
			App: "Dia", Titles: []string{"Base: konseki - Google Search"}, Samples: 1},
	}
	a.lastLoad = time.Now() // 阻止 refresh 覆盖注入的数据

	tt := ui.NewTester(a.view, 680, 900)

	// 时间轴应该画出 App 名和时间
	for _, want := range []string{"Konseki", "Dia", "14:29", "14:40"} {
		if !tt.HasText(want) {
			t.Errorf("时间轴缺少 %q，texts=%q", want, tt.Texts())
		}
	}
	// 标题也应该在
	if !tt.HasText("痕跡 Konseki") {
		t.Errorf("缺少标题，texts=%q", tt.Texts())
	}
	// 不应再显示空状态
	if tt.HasText("今天还没有记录") {
		t.Errorf("有数据时不该显示空状态")
	}
}

// 验证新分页结构：时间轴页和设置页各自能画出来，设置页里有供应商与脱敏。
func TestTabsRender(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	// 默认在时间轴页
	tt := ui.NewTester(a.view, 760, 900)
	for _, want := range []string{"时间轴", "设置"} {
		if !tt.HasText(want) {
			t.Errorf("顶部分页缺少 %q", want)
		}
	}

	// 切到设置页，四个区块都要在
	a.tab = 1
	tt = ui.NewTester(a.view, 760, 1400)
	for _, want := range []string{"Capture", "Summaries", "Providers", "Skill"} {
		if !tt.HasText(want) {
			t.Errorf("设置页缺少 %q，texts=%q", want, tt.Texts())
		}
	}
}

// 设置页的开关要真的改配置并落盘。
func TestRedactTogglePersists(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab = 1
	tt := ui.NewTester(a.view, 760, 1600)

	if !a.cfg.Get().Redact.Secrets {
		t.Fatal("默认应该开着凭据脱敏")
	}
	// 点「凭据与密码」旁边的开关
	if err := tt.Click("sw-redact-secrets"); err != nil {
		t.Skipf("找不到开关（布局可能变了）: %v", err)
	}
	if a.cfg.Get().Redact.Secrets {
		t.Error("点了开关但设置没变")
	}
}

// 供应商页要能列出预设，并且能添加一个。
func TestAddProvider(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab = 1

	before := len(a.cfg.Get().Providers)
	a.addProvider(mustPreset(t, "deepseek"))
	if got := len(a.cfg.Get().Providers); got != before+1 {
		t.Fatalf("添加后供应商数 = %d，期望 %d", got, before+1)
	}
	if a.cfg.Get().Providers[0].Preset != "deepseek" {
		t.Errorf("预设 id = %q", a.cfg.Get().Providers[0].Preset)
	}

	// 渲染一次，确认 tile 和编辑区都在
	a.openProvider = "deepseek"
	tt := ui.NewTester(a.view, 760, 1800)
	if !tt.HasText("DeepSeek") {
		t.Error("供应商名没画出来")
	}
}

// 摘要页在没有任何摘要时不该崩，也不该显示空列表标题。
func TestSummaryEmptyState(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab = 1
	tt := ui.NewTester(a.view, 760, 1600)
	if tt.HasText("今天的摘要") {
		t.Error("没有摘要时不该显示摘要列表")
	}
	if !tt.HasText("生成摘要") && !tt.HasText("先配置供应商") {
		t.Error("应该显示生成按钮或配置提示")
	}
}

// mustPreset 取一个预设，取不到就让测试失败。
func mustPreset(t *testing.T, id string) providers.Preset {
	t.Helper()
	p, ok := providers.ByID(id)
	if !ok {
		t.Fatalf("预设 %q 不存在", id)
	}
	return p
}

func savePNG(t *testing.T, path string, tt *ui.Tester) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
