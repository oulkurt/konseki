/*
 * [INPUT]: 依赖测试应用、固定的一天采集与会话夹具、本机凭据桩和离屏渲染器。
 * [OUTPUT]: 在指定目录输出设置三子页中英文深浅色 12 张全高截图、粉碎动画 GIF 与 6 张三阶段关键帧。
 * [POS]: 设置页与清除粉碎动画的视觉验收工具，仅在显式设置 KONSEKI_SHOTS 时输出文件。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/providers"

	"github.com/egoist/mygo/ui"
)

var settingsShotPages = []struct {
	name   string
	tab    int
	height int
}{
	{"recorder", 0, 2000},
	{"models", 1, 1100},
	{"general", 2, 660},
}

func prepareSettingsShotApp(t *testing.T, lang string) (*app, func()) {
	t.Helper()
	a, cleanup := newTestApp(t)
	a.now = func() time.Time { return time.Date(2026, 11, 5, 16, 0, 0, 0, time.Local) }
	scanTestCredentials(t, a, shotCredentials)
	shotDay(t, a)
	a.runningApps = func() []capture.Front {
		return []capture.Front{
			{App: "Signal", Bundle: "org.whispersystems.signal-desktop"},
			{App: "Telegram", Bundle: "ru.keepcoder.Telegram"},
		}
	}
	a.status = capture.Status{
		Phase: capture.PhaseActive,
		Usage: capture.Usage{Bytes: 344000000, Shots: 5721, Snapshots: 5832, Sessions: 33},
	}
	a.hasSummaries = true
	a.lastSessions = time.Now().Add(time.Hour)
	at := func(h, m int) time.Time { return time.Date(2026, 11, 4, h, m, 0, 0, time.Local) }
	titles := [3]string{
		"审查 P0 地基的类型契约",
		"核对活动记录器采集参数与清除流程",
		"起草小票时间轴与出纸动画",
	}
	if lang == "en" {
		titles = [3]string{
			"Review P0 foundation type contracts",
			"Verify Activity Recorder parameters and clear flow",
			"Draft receipt timeline and paper feed animation",
		}
	}
	a.recentSessions = []recentSession{
		{session: capture.Session{ID: "s3", Start: at(13, 40), End: at(15, 32), Snapshots: 184, Apps: []string{"Editor", "Konseki"}}, title: titles[2]},
		{session: capture.Session{ID: "s2", Start: at(10, 0), End: at(11, 2), Snapshots: 96, Apps: []string{"Alma"}}, title: titles[1]},
		{session: capture.Session{ID: "s1", Start: at(9, 0), End: at(9, 48), Snapshots: 72, Apps: []string{"T3 Code"}}, title: titles[0]},
	}
	preset, ok := providers.ByID("zhipu")
	if !ok {
		cleanup()
		t.Fatal("缺少 zhipu 预设")
	}
	a.addProvider(preset)
	a.setDefaultProvider(a.cfgNow.Providers[0])
	a.lastLoad = time.Now().Add(time.Hour)
	return a, cleanup
}

func renderSettingsShot(t *testing.T, subTab, height int, lang string, dark bool) []byte {
	t.Helper()
	a, cleanup := prepareSettingsShotApp(t, lang)
	defer cleanup()
	a.tab = 1
	a.settingsTab = subTab
	tt := ui.NewTester(a.view, 780, height)
	tt.SetLocale(lang)
	tt.SetDark(dark)
	tt.Advance(200 * time.Millisecond)
	var b bytes.Buffer
	if err := png.Encode(&b, tt.Image()); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func cloneTesterRGBA(tt *ui.Tester) *image.RGBA {
	src := tt.Image()
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, image.Point{}, draw.Src)
	return dst
}

func writePNGImage(t *testing.T, path string, img image.Image) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func renderShredArtifacts(t *testing.T, dir, lang string, dark bool, recordGIF bool) {
	t.Helper()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.now = func() time.Time { return time.Date(2026, 11, 4, 16, 0, 0, 0, time.Local) }
	scanTestCredentials(t, a, shotCredentials)
	shotDay(t, a)
	a.clearReceipt = a.receipt
	a.receipt = ReceiptDay{}
	a.dayFilter = ""
	a.lastLoad = time.Now().Add(time.Hour)

	tt := ui.NewTester(a.view, 780, 920)
	tt.SetLocale(lang)
	tt.SetDark(dark)
	a.shred.pending = true
	a.shred.start = time.Time{}

	appearance := "light"
	if dark {
		appearance = "dark"
	}
	suffix := lang + "-" + appearance

	// Advance 在两次截帧之间按 60 Hz 渲染每一帧，物理照常推进。
	// 截帧间隔就是 GIF 每帧的停留时间，播放速度和真实时间一致。
	end := shredShotKeys[len(shredShotKeys)-1].at
	var frames []*image.RGBA
	for elapsed := shredShotStep; elapsed <= end; elapsed += shredShotStep {
		tt.Advance(shredShotStep)
		frame := cloneTesterRGBA(tt)
		if recordGIF {
			frames = append(frames, frame)
		}
		for _, key := range shredShotKeys {
			if key.at == elapsed {
				writePNGImage(t, filepath.Join(dir, "shred-"+key.name+"-"+suffix+".png"), frame)
			}
		}
	}
	if recordGIF {
		writeReceiptGIF(t, filepath.Join(dir, "shred-"+suffix+".gif"), frames, shredShotStep)
	}
}

// shredShotStep 是粉碎 GIF 的截帧间隔，每秒 20 帧。
const shredShotStep = 50 * time.Millisecond

// shredShotKeys 是三张关键帧的时刻，从清除成功那一帧算起。
// 收纸 0 到 600 毫秒，送纸粉碎到 1800 毫秒，纸条落到 3500 毫秒，空白小票随后印出。
var shredShotKeys = []struct {
	name string
	at   time.Duration
}{
	{"retract", 300 * time.Millisecond},
	{"fall", 2600 * time.Millisecond},
	{"empty", 4600 * time.Millisecond},
}

func TestSettingsShots(t *testing.T) {
	dir := os.Getenv("KONSEKI_SHOTS")
	if dir == "" {
		t.Skip("KONSEKI_SHOTS 未设置")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, page := range settingsShotPages {
		for _, lang := range []string{"zh-Hans", "en"} {
			for _, dark := range []bool{false, true} {
				appearance := "light"
				if dark {
					appearance = "dark"
				}
				name := filepath.Join(dir, "settings-"+page.name+"-"+lang+"-"+appearance+".png")
				if err := os.WriteFile(name, renderSettingsShot(t, page.tab, page.height, lang, dark), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	renderShredArtifacts(t, dir, "zh-Hans", false, true)
	renderShredArtifacts(t, dir, "en", true, false)
}
