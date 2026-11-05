/**
 * [INPUT]: 依赖测试应用、固定的一天采集数据与摘要、固定的本机凭据和 MyGo 离屏渲染器。
 * [OUTPUT]: 提供四种界面状态在中英文、深浅色下的截图，以及逐字节确定性验证。
 * [POS]: 根模块的视觉回归基线。拆文件等不改界面的重构用它逐字节比对，所以夹具要走到尽量多的渲染路径。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/providers"
	"konseki/internal/summary"

	"github.com/egoist/mygo/ui"
)

var shotStates = []string{"timeline-empty", "timeline-day", "timeline-past", "settings"}

// shotCredentials 顶替本机凭据扫描。设置页渲染时会读钥匙串和 ~/.claude、~/.codex，
// 截图不能随开发机的登录状态和令牌到期时间变化，测试也不该把真实令牌读进进程。
var shotCredentials = []providers.Credential{
	{Source: "claude", Label: "Claude Code", Kind: "oauth", ExpiresAt: time.Date(2026, 11, 6, 3, 0, 0, 0, time.Local)},
	{Source: "codex", Label: "Codex", Kind: "oauth"},
}

func shotDay(t *testing.T, a *app) {
	t.Helper()
	at := func(h, m int) time.Time { return time.Date(2026, 11, 4, h, m, 0, 0, time.Local) }
	a.blocks = []capture.Block{
		{Start: at(9, 0), End: at(9, 26), App: "T3 Code", Titles: []string{"P0 地基：类型冻结", "审查 Terra 的提交"}, Samples: 9},
		{Start: at(10, 0), End: at(10, 3), App: "微信", Titles: []string{"文件传输助手"}, Samples: 1},
		{Start: at(10, 4), End: at(10, 8), App: "Alma", Titles: []string{"Activity Recorder"}, Samples: 2},
		{Start: at(11, 15), End: at(12, 2), App: "Dia", Titles: []string{"mygo/docs/i18n.md · GitHub"}, Samples: 12},
		{Start: at(13, 40), End: at(14, 5), App: "Editor", Titles: []string{"receipt.go - 小票时间轴，出纸口与送纸动画的第一版实现，标题够长才能看出换行"}, Samples: 6},
		{Start: at(15, 10), End: at(15, 32), App: "Konseki", Titles: []string{"痕跡 Konseki"}, Samples: 4},
	}
	a.sumEntries = []summary.Entry{
		{ID: "2026-11-04-0900", Day: "2026-11-04", Start: at(9, 0), End: at(10, 0), Headline: "审查 P0 地基的类型契约",
			Summary: "在 T3 Code 里读 Terra 的提交，发现屏幕文字边界会截断脱敏，补了回归测试。", Apps: []string{"T3 Code"},
			Topics: []string{"Konseki", "脱敏"}, Open: []string{"等 P0-A 交付"}, Redactions: 2},
		{ID: "2026-11-04-1000", Day: "2026-11-04", Start: at(10, 0), End: at(11, 0), Headline: "对照 Alma 的活动记录器设置页",
			Summary: "逐项核对 Alma 的采集参数和清除流程。", Apps: []string{"微信", "Alma"}, Topics: []string{"Alma"}},
		{ID: "2026-11-04-1300", Day: "2026-11-04", Start: at(13, 0), End: at(14, 0), Headline: "起草小票时间轴的出纸动画并检查长标题在纸面上的换行方式",
			Summary: "写了出纸口和送纸动画，长标题的换行还没看过。", Apps: []string{"Editor"},
			Topics: []string{"MyGo", "动画"}, Open: []string{"长标题换行观感未验证"}, Decisions: []string{"使用悬挂式小票"}, Files: []string{"receipt.go"}},
	}
	a.dayFilter = "2026-11-04"
	a.receipt = buildReceipt(at(0, 0), a.sumEntries, a.blocks, nil)
	a.expanded["2026-11-04-1300"] = true
}

func shot(t *testing.T, state, lang string, dark bool) []byte {
	t.Helper()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.now = func() time.Time { return time.Date(2026, 11, 5, 16, 0, 0, 0, time.Local) }
	scanTestCredentials(t, a, shotCredentials)
	height := 1000
	switch state {
	case "timeline-day":
		shotDay(t, a)
		height = 2400
	case "timeline-past":
		shotDay(t, a)
		a.blocks = nil
		a.receipt = buildReceipt(time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local), a.sumEntries, nil, nil)
		height = 1800
	case "settings":
		// 设置页带上一天的数据，摘要列表才会出现；再配一家供应商，供应商卡片和编辑区才会出现。
		shotDay(t, a)
		p, ok := providers.ByID("deepseek")
		if !ok {
			t.Fatal("预设 deepseek 不存在")
		}
		a.addProvider(p)
		a.tab = 1
		height = 3600
	}
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, height)
	tt.SetLocale(lang)
	tt.SetDark(dark)
	if state == "settings" {
		if err := tt.Click("配置"); err != nil {
			t.Fatal(err)
		}
		if !tt.HasText("测试连接") {
			t.Fatal("供应商编辑区没有展开，截图覆盖不到它")
		}
	}
	tt.Advance(time.Second)
	tt.Frame()
	var b bytes.Buffer
	if err := png.Encode(&b, tt.Image()); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestShots(t *testing.T) {
	dir := os.Getenv("KONSEKI_SHOTS")
	if dir == "" {
		t.Skip("KONSEKI_SHOTS 未设置")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, state := range shotStates {
		for _, lang := range []string{"zh-Hans", "en"} {
			for _, dark := range []bool{false, true} {
				appearance := "light"
				if dark {
					appearance = "dark"
				}
				name := filepath.Join(dir, state+"-"+lang+"-"+appearance+".png")
				if err := os.WriteFile(name, shot(t, state, lang, dark), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestShotsDeterministic(t *testing.T) {
	for _, state := range shotStates {
		if !bytes.Equal(shot(t, state, "zh-Hans", false), shot(t, state, "zh-Hans", false)) {
			t.Errorf("%s 两次渲染不一致", state)
		}
	}
}
