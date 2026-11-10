/*
 * [INPUT]: 依赖临时应用配置、采集参数字段表、供应商预设和运行应用注入。
 * [OUTPUT]: 验证采集参数提交和说明。验证敏感应用快速添加和去重。
 * 验证供应商区域切换和输入同步。验证删除第一个供应商后界面不越界。
 * 验证权限重启提示、设置行对齐和自动摘要间隔说明。
 * [POS]: 记录与模型设置交互边界的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/providers"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func TestRecorderFieldCommit(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	for i, tc := range []struct{ input, want int }{
		{100, 3000}, {310000, 300000}, {45000, 45000}, {1500, 1500}, {500, 10000}, {58, 60},
	} {
		a.commitRecorderField(i, tc.input)
		if got := recorderFields[i].get(a.cfg.Get().Recorder); got != tc.want {
			t.Errorf("%s: 得到 %d，期望 %d", recorderFields[i].key, got, tc.want)
		}
	}
	s := recorderSettings(a.cfg.Get().Recorder, a.appDir)
	if s.Debounce != 3*time.Second || s.Heartbeat != 300*time.Second || s.Idle != 45*time.Second || s.TypingPause != 1500*time.Millisecond || s.ScreenPoll != 10*time.Second || s.JPEGQuality != 60 {
		t.Fatalf("采集参数映射错误: %+v", s)
	}
}

// 每个采集参数都要说明它管什么，只给范围的话用户看不出「防抖间隔」的用途。
func TestRecorderFieldsExplainThemselves(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		msg := locales.In(lang)
		seen := map[string]bool{}
		for _, f := range recorderFields {
			help := f.help(msg)
			if help == "" || help == f.label(msg) || seen[help] {
				t.Errorf("%s 的 %s 缺少独立的说明：%q", lang, f.key, help)
			}
			seen[help] = true
		}
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab = 1
	a.settingsTab = 0
	tt := ui.NewTester(a.view, 780, 2600)
	tt.SetLocale("zh-Hans")
	want := locales.In("zh-Hans").RecorderDebounceHelp()
	if !slices.ContainsFunc(tt.Texts(), func(s string) bool { return strings.Contains(s, want) }) {
		t.Fatalf("记录页没有显示防抖间隔的说明 %q", want)
	}
}

func TestSensitiveAppsRunningPicker(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.runningApps = func() []capture.Front {
		return []capture.Front{{App: "Signal", Bundle: "org.whispersystems.signal-desktop"}}
	}
	a.tab = 1
	a.settingsTab = 0
	tt := ui.NewTester(a.view, 780, 2600)
	if err := tt.Click("从运行中的应用添加"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Signal · org.whispersystems.signal-desktop"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(a.cfg.Get().Recorder.SensitiveApps, "org.whispersystems.signal-desktop") {
		t.Fatalf("未加入敏感应用: %v", a.cfg.Get().Recorder.SensitiveApps)
	}
}

// 已在列表里的应用不再出现在菜单里，重复添加也不会写两遍。
func TestSensitiveAppsPickerSkipsListed(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	calls := 0
	a.runningApps = func() []capture.Front {
		calls++
		return []capture.Front{
			{App: "1Password", Bundle: "COM.1password.1password"},
			{App: "Signal", Bundle: "org.whispersystems.signal-desktop"},
		}
	}
	for _, f := range a.candidateSensitiveApps() {
		if strings.EqualFold(f.Bundle, "com.1password.1password") {
			t.Fatalf("已在列表里的应用又出现在菜单里：%+v", f)
		}
	}
	a.addSensitiveApp("org.whispersystems.signal-desktop")
	a.addSensitiveApp("org.whispersystems.signal-desktop")
	n := 0
	for _, b := range a.cfg.Get().Recorder.SensitiveApps {
		if b == "org.whispersystems.signal-desktop" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Signal 在敏感应用里出现 %d 次", n)
	}
	if got := a.candidateSensitiveApps(); slices.ContainsFunc(got, func(f capture.Front) bool { return f.App == "Signal" }) {
		t.Fatalf("加入以后菜单里还有 Signal：%v", got)
	}
	// 菜单每帧重建，两秒内只查一次运行中的应用。
	if calls != 1 {
		t.Fatalf("两秒内查了 %d 次运行中的应用", calls)
	}
}

func TestProviderRegionSwitch(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	preset, ok := providers.ByID("zhipu")
	if !ok {
		t.Fatal("缺少 zhipu 预设")
	}
	a.addProvider(preset)
	a.tab = 1
	a.settingsTab = 1
	tt := ui.NewTester(a.view, 780, 1800)
	if err := tt.Click("Coding Plan"); err != nil {
		t.Fatal(err)
	}
	got := a.cfg.Get().Providers[0]
	if got.Region != "coding" || got.ChatBase != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Fatalf("区域未切换: %+v", got)
	}
}

// 编辑框开着时，别处改了配置（重新导入凭据、拉取模型），旧草稿不能写回去；用户输入仍然写进配置。
func TestProviderEditorFollowsOutsideChanges(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	preset, ok := providers.ByID("zhipu")
	if !ok {
		t.Fatal("缺少 zhipu 预设")
	}
	a.addProvider(preset)
	a.tab = 1
	a.settingsTab = 1
	tt := ui.NewTester(a.view, 780, 1800)
	a.saveProviderField(0, func(p *config.ProviderConfig) { p.APIKey = "sk-new"; p.DefaultModel = "glm-4.6" })
	tt.Frame()
	if got := a.cfg.Get().Providers[0]; got.APIKey != "sk-new" || got.DefaultModel != "glm-4.6" {
		t.Fatalf("编辑框把旧草稿写回了配置：key=%q model=%q", got.APIKey, got.DefaultModel)
	}
	// 「模型」两个字先匹配到子页签，所以从模型输入框的说明文字定位输入框：
	// 说明文字只有一行，输入框在它下面。
	model, ok := tt.Find(locales.In("zh-Hans").ModelsModel())
	if !ok {
		t.Fatal("缺少默认模型输入框")
	}
	if model.H < 22 {
		tt.ClickAt(model.X+80, model.Y+model.H+16)
	} else {
		tt.ClickAt(model.X+80, model.Y+model.H/2)
	}
	if !tt.Focused(locales.In("zh-Hans").ModelsModel()) {
		t.Fatal("模型输入框没有拿到焦点")
	}
	tt.Type("-air")
	tt.Frame()
	if got := a.cfg.Get().Providers[0].DefaultModel; !strings.Contains(got, "-air") {
		t.Fatalf("输入没有写进配置：%q", got)
	}
}

// 有两个供应商时移除第一个。移除发生在渲染循环里，同一帧剩下的循环不能越界。
func TestRemoveFirstProviderKeepsRendering(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	for _, id := range []string{"zhipu", "zai"} {
		preset, ok := providers.ByID(id)
		if !ok {
			t.Fatalf("缺少 %s 预设", id)
		}
		a.addProvider(preset)
	}
	a.tab = 1
	a.settingsTab = 1
	tt := ui.NewTester(a.view, 780, 1800)
	if err := tt.Click("配置"); err != nil {
		t.Fatal(err)
	}
	if a.openProvider != providerKey(a.cfgNow.Providers[0]) {
		t.Fatal("没有打开第一个供应商")
	}
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.cfg.Get().Providers; len(got) != 1 || got[0].Preset != "zai" {
		t.Fatalf("移除后剩下 %+v", got)
	}
}

// 屏幕录制生效以后不再提示重启。
func TestPermissionRestartHintOnlyBeforeGrant(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab = 1
	a.lastLoad = time.Now().Add(time.Hour)
	hint := "屏幕录制授权后，请重启Konseki。"
	a.status.Perms.Screen = capture.PermDenied
	tt := ui.NewTester(a.view, 760, 2600)
	if !tt.HasText(hint) {
		t.Fatal("屏幕录制未生效时缺少重启提示")
	}
	a.status.Perms.Screen = capture.PermGranted
	tt.Frame()
	if tt.HasText(hint) {
		t.Fatal("屏幕录制已生效仍提示重启")
	}
}

// 没有说明的设置行，标题和右侧控件的中线对齐。空说明占一行时标题会偏上约 10 像素。
func TestSettingsRowCentersTitleWithoutSubtitle(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	tt := ui.NewTester(func(c *ui.Context) {
		a.settingsRow(c, "行标题", "", themePalette(c), func() {
			ui.Button(c.Key("row-control"), "右侧按钮")
		})
	}, 600, 200)
	title, ok := tt.Find("行标题")
	control, ok2 := tt.Find("右侧按钮")
	if !ok || !ok2 {
		t.Fatal("缺少标题或按钮")
	}
	if d := (title.Y + title.H/2) - (control.Y + control.H/2); d > 2 || d < -2 {
		t.Fatalf("标题中线和按钮中线差 %.1f 像素", d)
	}
}

// 模型页的每一行都说明自己管什么。自动摘要的说明写实际间隔，低于 5 分钟按 5 分钟算，和自动摘要循环一致。
func TestModelsSettingsExplainThemselves(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	msg := locales.In("zh-Hans")
	a.tab = 1
	a.settingsTab = 1
	for _, tc := range []struct{ set, want int }{{45, 45}, {2, 5}} {
		if err := a.cfg.Update(func(c *config.Config) { c.Summary.IntervalMinutes = tc.set }); err != nil {
			t.Fatal(err)
		}
		a.cfgNow = a.cfg.Get()
		if got := summaryInterval(a.cfgNow.Summary); got != time.Duration(tc.want)*time.Minute {
			t.Fatalf("设置 %d 分钟，自动摘要间隔算成 %v", tc.set, got)
		}
		tt := ui.NewTester(a.view, 780, 1800)
		tt.SetLocale("zh-Hans")
		texts := tt.Texts()
		for _, want := range []string{msg.ModelsEffort(), msg.ModelsEffortHelp(), msg.ModelsAutoHelp(tc.want), msg.ModelsRedactHelp(), msg.ModelsRedactSecretsHelp(), msg.ModelsRedactPersonalHelp()} {
			if !slices.Contains(texts, want) {
				t.Errorf("设置 %d 分钟：模型页缺少 %q", tc.set, want)
			}
		}
	}
}
