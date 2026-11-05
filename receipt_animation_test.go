/*
 * [INPUT]: 依赖固定小票夹具、MyGo 测试时钟与截图输出目录。
 * [OUTPUT]: 在指定目录生成送纸 GIF 和四帧拼图。
 * [POS]: 小票动画的离屏交付工具，只在显式指定截图目录时运行。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"image"
	colorpalette "image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"konseki/internal/summary"

	"github.com/egoist/mygo/ui"
)

func TestReceiptAnimationArtifacts(t *testing.T) {
	dir := os.Getenv("KONSEKI_SHOTS")
	if dir == "" {
		t.Skip("KONSEKI_SHOTS 未设置")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_, feed := receiptTestApp(t)
	frames := receiptFrames(feed)
	writeReceiptGIF(t, filepath.Join(dir, "receipt-feed.gif"), frames)
	w, h := frames[0].Bounds().Dx(), frames[0].Bounds().Dy()
	sheet := image.NewRGBA(image.Rect(0, 0, w*4, h))
	for i, index := range []int{0, 5, 12, len(frames) - 1} {
		draw.Draw(sheet, image.Rect(i*w, 0, (i+1)*w, h), frames[index], image.Point{}, draw.Src)
	}
	file, err := os.Create(filepath.Join(dir, "receipt-feed-sheet.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, sheet); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	a, newline := receiptTestApp(t)
	newline.Advance(time.Second)
	for _, entry := range a.sumEntries {
		if err := a.sums.Upsert(entry); err != nil {
			t.Fatal(err)
		}
	}
	a.lastLoad = time.Time{}
	a.refresh()
	newline.Frame()
	newline.Advance(time.Second)
	start := time.Date(2026, 11, 4, 16, 0, 0, 0, time.Local)
	if err := a.sums.Upsert(summary.Entry{ID: "new", Day: "2026-11-04", Start: start, End: start.Add(20 * time.Minute), Headline: "验证新条目送纸", Summary: "刷新后打印机送出新记录。", Apps: []string{"Konseki"}}); err != nil {
		t.Fatal(err)
	}
	a.lastLoad = time.Time{}
	a.refresh()
	newline.Frame()
	writeReceiptGIF(t, filepath.Join(dir, "receipt-newline.gif"), receiptFrames(newline))
}
func receiptFrames(tt *ui.Tester) []*image.RGBA {
	frames := make([]*image.RGBA, 0, 24)
	for i := 0; i < 24; i++ {
		source := tt.Image()
		copy := image.NewRGBA(source.Bounds())
		draw.Draw(copy, copy.Bounds(), source, image.Point{}, draw.Src)
		frames = append(frames, copy)
		tt.Advance(40 * time.Millisecond)
		tt.Frame()
	}
	return frames
}
func writeReceiptGIF(t *testing.T, path string, frames []*image.RGBA) {
	t.Helper()
	g := &gif.GIF{}
	for i, frame := range frames {
		p := image.NewPaletted(frame.Bounds(), colorpalette.Plan9)
		draw.FloydSteinberg.Draw(p, p.Bounds(), frame, image.Point{})
		g.Image = append(g.Image, p)
		delay := 4
		if i == len(frames)-1 {
			delay = 200
		}
		g.Delay = append(g.Delay, delay)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = gif.EncodeAll(f, g); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
