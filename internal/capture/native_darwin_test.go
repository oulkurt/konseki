//go:build darwin

/*
 * [INPUT]: 依赖合成 CGImage、位图缩放、ImageIO 和 Vision。
 * [OUTPUT]: 验证原生空白判断、指纹、编码、OCR 与性能。
 * [POS]: 不抓屏、不请求授权的 macOS 原生管线测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeFrame(t testing.TB, img *image.RGBA) *Frame {
	t.Helper()
	initPlatform()
	f := &Frame{W: img.Rect.Dx(), H: img.Rect.Dy(), RGBA: img.Pix}
	withPool(func() {
		f.native = rgbaToImage(f)

	})
	if f.native == 0 {
		t.Fatal("CGImage 创建失败")
	}
	f.RGBA = nil
	t.Cleanup(func() { releaseFrame(f) })
	return f
}
func pattern(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*img.Stride + x*4
			v := byte((x*17 + y*29 + (x*y)%251) % 256)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, byte(int(v)*3%256), byte(int(v)*7%256), 255
		}
	}
	return img
}
func TestNativeBlank(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
	if !isBlank(nativeFrame(t, img).native) {
		t.Fatal("黑帧未识别")
	}
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{80, 100, 120, 255}}, image.Point{}, draw.Src)
	if isBlank(nativeFrame(t, img).native) {
		t.Fatal("有内容的帧被判空白")
	}
}
func TestNativeFingerprint(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 12; y < 350; y += 18 {
		for x := 30; x < 610; x += 12 {
			draw.Draw(img, image.Rect(x, y, x+8, y+5), image.Black, image.Point{}, draw.Src)
		}
	}
	a := fingerprintFrame(nativeFrame(t, img))
	if !duplicate(a, fingerprintFrame(nativeFrame(t, img))) {
		t.Fatal("同一画面不一致")
	}
	edited := image.NewRGBA(img.Bounds())
	copy(edited.Pix, img.Pix)
	for y := 12; y < 30; y++ {
		for x := 30; x < 130; x++ {
			i := y*edited.Stride + x*4
			edited.Pix[i], edited.Pix[i+1], edited.Pix[i+2] = 255-edited.Pix[i], 255-edited.Pix[i+1], 255-edited.Pix[i+2]
		}
	}
	if !duplicate(a, fingerprintFrame(nativeFrame(t, edited))) {
		t.Fatal("少量变化不应触发")
	}
	shifted := image.NewRGBA(img.Bounds())
	draw.Draw(shifted, shifted.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(shifted, image.Rect(0, 8, 640, 360), img, image.Point{}, draw.Src)
	if duplicate(a, fingerprintFrame(nativeFrame(t, shifted))) {
		t.Fatal("文字滚动应触发")
	}
}
func TestNativeSaveJPEG(t *testing.T) {
	f := nativeFrame(t, pattern(3000, 1700))
	var sizes []int64
	for _, q := range []int{30, 95} {
		path := filepath.Join(t.TempDir(), "shot.jpg")
		if !platformSaveJPEG(f, path, q) {
			t.Fatal("JPEG 编码失败")
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(file)
		file.Close()
		if err != nil || cfg.Width > 2560 || cfg.Height > 2560 {
			t.Fatal(cfg, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, info.Size())
	}
	if sizes[1] <= sizes[0] {
		t.Fatal(sizes)
	}
}
func TestNativeOCR(t *testing.T) {
	file, err := os.Open("testdata/timeline-empty-zh-Hans-light.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	f := nativeFrame(t, rgba)
	text, _, err := (darwinOCR{}).Recognize(f, []string{"zh-Hans", "en-US"})
	if err != nil || !strings.Contains(text, "今天还没有记录") || !strings.Contains(text, "Konseki") {
		t.Fatalf("OCR: %q %v", text, err)
	}
}
func BenchmarkNativePipeline(b *testing.B) {
	f := nativeFrame(b, pattern(5120, 2880))
	b.Run("Blank", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			isBlank(f.native)
		}
	})
	b.Run("Fingerprint", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			fingerprintFrame(f)
		}
	})
	b.Run("SaveJPEG", func(b *testing.B) {
		path := filepath.Join(b.TempDir(), "shot.jpg")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !platformSaveJPEG(f, path, 85) {
				b.Fatal("编码失败")
			}
		}
	})
}
