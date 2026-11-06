/*
 * [INPUT]: 依赖临时应用配置、采集参数字段表、供应商预设和运行应用注入。
 * [OUTPUT]: 验证字段提交、每个字段都有说明、敏感应用快速添加与供应商区域切换。
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
