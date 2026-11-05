/**
 * [INPUT]: 依赖测试应用、固定采集数据和 MyGo 离屏渲染器。
 * [OUTPUT]: 提供界面截图和确定性验证。
 * [POS]: 根模块的视觉回归基线，复用现有测试应用。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"github.com/egoist/mygo/ui"
	"image/png"
	"konseki/internal/capture"
	"konseki/internal/summary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func shot(t *testing.T, state, lang string, dark bool) []byte {
	t.Helper()
	a, cleanup := newTestApp(t)
	defer cleanup()
	height := 1000
	if state == "settings" {
		a.tab = 1
		height = 3600
	}
	if state == "timeline-day" {
		height = 2400
		base := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
		for i := 0; i < 8; i++ {
			start := base.Add(time.Duration(i*65) * time.Minute)
			a.blocks = append(a.blocks, capture.Block{Start: start, End: start.Add(30 * time.Minute), App: "Editor", Titles: []string{"项目计划 Project plan"}, Samples: 2})
		}
		for i := 0; i < 4; i++ {
			start := base.Add(time.Duration(i*120) * time.Minute)
			a.sumEntries = append(a.sumEntries, summary.Entry{Start: start, End: start.Add(time.Hour), Headline: "项目进展", Summary: "完成设计讨论", Open: []string{"继续审阅"}, Topics: []string{"设计"}})
		}
	}
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, height)
	tt.SetLocale(lang)
	tt.SetDark(dark)
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"timeline-empty", "timeline-day", "settings"} {
		for _, lang := range []string{"zh-Hans", "en"} {
			for _, dark := range []bool{false, true} {
				appearance := "light"
				if dark {
					appearance = "dark"
				}
				if err := os.WriteFile(filepath.Join(dir, state+"-"+lang+"-"+appearance+".png"), shot(t, state, lang, dark), 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
func TestShotsDeterministic(t *testing.T) {
	if !bytes.Equal(shot(t, "timeline-day", "zh-Hans", false), shot(t, "timeline-day", "zh-Hans", false)) {
		t.Fatal("截图不一致")
	}
}
