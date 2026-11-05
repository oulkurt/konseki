//go:build darwin

/*
 * [INPUT]: 依赖平台屏幕与文字识别能力。
 * [OUTPUT]: 提供采集接口的平台实现。
 * [POS]: 采集模块的平台适配，与外壳隔离。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

// macOS 实现：CGDisplayCreateImage + Vision OCR。
//
// 这条路已经实测跑通，细节和踩的坑见 docs/findings.md。

import (
	"time"

	"github.com/ebitengine/purego"
)

// macOS 的屏幕录制权限没有「标准请求 API」可用，只有这两个 C 函数：
//
//	CGPreflightScreenCaptureAccess  —— 只查询，永不弹框
//	CGRequestScreenCaptureAccess    —— 会弹框，但仅在用户没明确拒绝过时有效
//
// 关键行为（实测 + 文档确认）：
//   - 用户点过「拒绝」后，Request 不会再次弹框，直接返回 false
//   - 授权后往往需要重启 App 才生效，旧进程的 CGDisplayCreateImage 仍返回黑图
//   - App 名不需要权限，只有窗口标题和截屏需要
var (
	preflightScreen func() bool
	requestScreen   func() bool
)

func initScreenPerm() {
	purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	// 这两个符号在高版本 SDK 才导出，取不到就退化成「未知」
	lib := uintptr(0)
	if l, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW); err == nil {
		lib = l
	}
	if sym, err := purego.Dlsym(lib, "CGPreflightScreenCaptureAccess"); err == nil {
		purego.RegisterFunc(&preflightScreen, sym)
	}
	if sym, err := purego.Dlsym(lib, "CGRequestScreenCaptureAccess"); err == nil {
		purego.RegisterFunc(&requestScreen, sym)
	}
}

// screenPermission 返回权限状态，并在必要时尝试弹框。
//
// 返回值语义：
//
//	granted  —— 有权限，可以采集
//	denied   —— 用户拒绝过，只能引导去系统设置
//	unknown  —— 系统没提供 API，靠「能不能截到非黑图」自行判断
func screenPermission(ask bool) string {
	if preflightScreen == nil {
		return "unknown"
	}
	if preflightScreen() {
		return "granted"
	}
	if !ask {
		return "denied"
	}
	// 只有用户还没明确拒绝时，这个调用才会真的弹框
	if requestScreen != nil && requestScreen() {
		return "granted"
	}
	return "denied"
}

// probeCapture 用「能不能截到非黑图」来判断权限是否真的生效。
//
// 这是必需的兜底：macOS 在「已授权但进程是授权前启动的」情况下，
// preflight 会返回 true，但 CGDisplayCreateImage 拿到的仍是全黑图。
// 只有实测一帧才能确认。
func probeCapture() (ok bool, blank bool) {
	img := grab()
	if img == 0 {
		return false, false
	}
	if isBlank(img) {
		return false, true
	}
	return true, false
}

type darwinScreen struct{}

func newScreen() Screen {
	initPlatform()
	return darwinScreen{}
}

func (darwinScreen) FrontWindow() (string, string, int) {
	return frontWindow()
}

func (darwinScreen) Permission(ask bool) PermState {
	switch screenPermission(ask) {
	case "granted":
		return PermGranted
	case "denied":
		return PermDenied
	default:
		return PermUnknown
	}
}

func (darwinScreen) Grab() *Frame {
	img := grab()
	if img == 0 {
		return nil
	}
	f := &Frame{W: int(cgGetW(img)), H: int(cgGetH(img))}
	f.Blank = isBlank(img)
	if !f.Blank {
		f.RGBA = imageToRGBA(img)
	}
	return f
}

type darwinOCR struct{}

func newOCR() OCREngine { return darwinOCR{} }

func (darwinOCR) Recognize(f *Frame, languages []string) (string, int, error) {
	img := rgbaToImage(f)
	if img == 0 {
		return "", 0, nil
	}
	text, n := ocrImage(img, languages)
	return text, n, nil
}

var _ = time.Now

// platformSaveThumb 把 Frame 缩成小图存盘（磁盘上只留视觉凭证）。
func platformSaveThumb(f *Frame, path string) bool {
	img := rgbaToImage(f)
	if img == 0 {
		return false
	}
	return saveThumb(img, path)
}

func platformSaveJPEG(f *Frame, path string, quality int) bool {
	pool := O(O(cls("NSAutoreleasePool"), "alloc"), "init")
	defer O(pool, "drain")
	img := rgbaToImage(f)
	if img == 0 {
		return false
	}
	img = downscaleTo(img, screenshotMaxDimension)
	if img == 0 {
		return false
	}
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	data := writeJPEGData(img, float64(quality)/100)
	return data != 0 && O(data, "writeToFile:atomically:", nsstr(path), 1) != 0
}
