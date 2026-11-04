//go:build windows

package capture

// Windows 实现骨架。
//
// 好消息：Windows 在这件事上比 macOS 简单得多。
//
// 权限模型完全不同：
//   - macOS 需要用户显式授权「屏幕录制」，且授权后常常要重启 App
//   - Windows 的传统 Win32 截屏（GDI BitBlt / PrintWindow / DXGI
//     Desktop Duplication）**不需要任何用户授权**，进程能跑就能截
//
// 这意味着 docs/shipping.md 里那整套 TCC 权限流程在 Windows 上直接不存在，
// 用户装了就能用。这是 Windows 版体验反而更顺的原因。
//
// 需要实现的 Win32 调用（全部通过 syscall 调 user32/gdi32，无需 CGO）：
//
//	GetForegroundWindow()                    -> HWND 前台窗口
//	GetWindowTextW(hwnd, buf, len)           -> 窗口标题（无需权限）
//	GetWindowThreadProcessId(hwnd, &pid)     -> 进程号
//	OpenProcess + QueryFullProcessImageNameW -> 进程名（App 名）
//	GetDC(NULL) / CreateCompatibleDC / BitBlt -> 截全屏
//	GetSystemMetrics(SM_CXSCREEN/SM_CYSCREEN) -> 分辨率
//
// OCR 建议：
//   - Windows.Media.Ocr（系统自带，支持中英日，质量接近 Vision）
//   - 需要走 WinRT COM 接口，比 macOS 的 Vision 麻烦一些
//   - 兜底方案：内置 Tesseract，或调可选的本地模型
//
// 尚未实现 —— 这个文件目前只是让 collector 能在 Windows 上编译通过。

import "errors"

var errNotImplemented = errors.New("windows backend not implemented yet")

type windowsScreen struct{}

func newScreen() Screen {
	return windowsScreen{}
}

func (windowsScreen) FrontWindow() (string, string, int) {
	// TODO: GetForegroundWindow + GetWindowTextW + QueryFullProcessImageNameW
	return "", "", 0
}

func (windowsScreen) Permission(ask bool) PermState {
	// Windows 传统截屏不需要授权，直接当作已授予。
	// 只有走 Windows.Graphics.Capture（UWP）才需要用户确认，
	// 但那条路对桌面 App 没有好处，不用它。
	return PermGranted
}

func (windowsScreen) Grab() *Frame {
	// TODO: BitBlt 全屏到 DIB，转成 Frame.W/H/RGBA
	return nil
}

type windowsOCR struct{}

func newOCR() OCREngine { return windowsOCR{} }

func (windowsOCR) Recognize(f *Frame) (string, int, error) {
	// TODO: Windows.Media.Ocr
	return "", 0, errNotImplemented
}

// platformSaveThumb 未实现。
func platformSaveThumb(f *Frame, path string) bool { return false }
