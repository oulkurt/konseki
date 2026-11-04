package main

import (
	"image/png"
	"os"
	"slices"
	"testing"
	"time"

	"konseki/internal/capture"

	"github.com/egoist/mygo/ui"
)

// 用真实数据验证时间轴渲染：注入 blocks，检查文本是否画出来。
func TestTimelineRenders(t *testing.T) {
	cfg := capture.DefaultConfig()
	cfg.Root = t.TempDir()
	r, err := capture.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	a := &app{runner: r}
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

// 验证 Capture、Summaries 与 History 全界面渲染以及展开交互
func TestShowcaseTimelineExpandCollapse(t *testing.T) {
	cfg := capture.DefaultConfig()
	cfg.Root = t.TempDir()
	r, err := capture.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	a := &app{
		runner:          r,
		includeShowcase: true,
		lastLoad:        time.Now(),
	}

	tt := ui.NewTester(a.view, 680, 980)
	tt.SetScale(2)

	// 1. 检查 Capture + Summaries + History 全部标题与默认控件文本
	for _, want := range []string{
		"Capture",
		"Enable Context Awareness",
		"Capture your activity on this device.",
		"Pause capturing",
		"Stop capturing for a while without turning it off.",
		"Select",
		"Capture typed text",
		"Save the sentences you type. Aside never stores keystrokes or password fields.",
		"Allow Screen Recording",
		"When a page has no readable text, read it from the screen. Only the text is kept.",
		"Denylist",
		"Aside never captures what you do in these apps and websites.",
		"Add",
		"Summaries",
		"Summary model",
		"Summarizes your activity in the background while you work.",
		"Gemini 3.8 ...",
		"Retention",
		"Aside deletes raw captures after this many days. Summaries stay.",
		"14 days",
		"History",
		"Today",
		"2:40 PM",
		"10:00 AM",
		"9:40 AM",
		"9:30 AM",
		"9:20 AM",
		"9:10 AM",
		"9:00 AM",
	} {
		if !tt.HasText(want) {
			t.Errorf("主界面缺少 %q", want)
		}
	}

	if os.Getenv("KONSEKI_SAVE_PREVIEW") == "1" {
		savePNG(t, "/tmp/konseki-full-ui.png", tt)
	}

	// 2. 点击 10:00 AM 卡片展开详情
	tt.SetSize(680, 1600)
	if err := tt.Click("Checked MyGo subagent CI status in T3 Code and chatted with David Lee"); err != nil {
		t.Fatalf("点击 10:00 AM 条目失败: %v", err)
	}

	for _, want := range []string{
		"Memory summary",
		"MyGo Refactor (T3 Code):",
		"t3/refactor-mygo-plugins-alma-ui",
		"Relevant prior context",
		"Important non-obvious context about the user",
		"xiaotong_work_mygo",
		"Recording summary",
		"09:54–09:55 · Messages and Dia",
		"09:55–09:57 · T3 Code (Nightly)",
		"09:57–09:57 · Alma",
		"09:57–10:00 · Messages, Finder, and WeChat",
	} {
		if !tt.HasText(want) {
			t.Errorf("展开态缺少 %q", want)
		}
	}
}

// 验证模型选择、思考程度选择、暂停时长选择、以及 Denylist 选择 App 和 Website
func TestSettingsControlsInteractive(t *testing.T) {
	cfg := capture.DefaultConfig()
	cfg.Root = t.TempDir()
	r, err := capture.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	a := &app{
		runner:          r,
		includeShowcase: true,
		lastLoad:        time.Now(),
	}

	tt := ui.NewTester(a.view, 680, 980)

	// 1. 选择暂停时间 (Pause capturing -> 30 minutes)
	if err := tt.Click("Select"); err != nil {
		t.Fatalf("点击 Pause capturing Select 失败: %v", err)
	}
	if err := tt.Click("30 minutes"); err != nil {
		t.Fatalf("选择 30 minutes 失败: %v", err)
	}
	if a.pauseDuration != "30 minutes" {
		t.Errorf("pauseDuration = %q, 期望 30 minutes", a.pauseDuration)
	}

	// 2. 选择模型 (Summary model -> Claude 4.5 Sonnet)
	if err := tt.Click("Gemini 3.8 ..."); err != nil {
		t.Fatalf("点击模型选择按钮失败: %v", err)
	}
	if err := tt.Click("Claude 4.5 Sonnet"); err != nil {
		t.Fatalf("选择 Claude 4.5 Sonnet 失败: %v", err)
	}
	if a.summaryModel != "Claude 4.5 Sonnet" {
		t.Errorf("summaryModel = %q, 期望 Claude 4.5 Sonnet", a.summaryModel)
	}

	// 3. 选择思考程度 (Thinking effort -> High)
	if err := tt.Click("Thinking effort"); err != nil {
		t.Fatalf("点击思考程度按钮失败: %v", err)
	}
	if err := tt.Click("Thinking: High"); err != nil {
		t.Fatalf("选择 Thinking: High 失败: %v", err)
	}
	if a.thinkingEffort != "High" {
		t.Errorf("thinkingEffort = %q, 期望 High", a.thinkingEffort)
	}

	// 4. Denylist 添加 App 和 Website
	if err := tt.Click("Add"); err != nil {
		t.Fatalf("点击 Denylist + Add 失败: %v", err)
	}
	if err := tt.Click("1Password"); err != nil {
		t.Fatalf("勾选 1Password App 失败: %v", err)
	}
	if !slices.Contains(a.denylistApps, "1Password") {
		t.Errorf("denylistApps 应包含 1Password, 实际: %v", a.denylistApps)
	}

	// 切换到 Websites 标签页并勾选 1password.com
	if err := tt.Click("Websites"); err != nil {
		t.Fatalf("切换到 Websites 标签失败: %v", err)
	}
	if err := tt.Click("1password.com"); err != nil {
		t.Fatalf("勾选 1password.com 失败: %v", err)
	}
	if !slices.Contains(a.denylistSites, "1password.com") {
		t.Errorf("denylistSites 应包含 1password.com, 实际: %v", a.denylistSites)
	}
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
