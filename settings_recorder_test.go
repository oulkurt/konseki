/*
 * [INPUT]: 依赖临时应用配置、采集参数字段表、供应商预设和运行应用注入。
 * [OUTPUT]: 验证字段提交、敏感应用快速添加与供应商区域切换。
 * [POS]: 记录与模型设置交互边界的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"slices"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/providers"

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
