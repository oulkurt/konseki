//go:build linux

/*
 * [INPUT]: 依赖 capture_gate.go 在调用前执行许可检查；依赖平台屏幕与文字识别能力。
 * [OUTPUT]: 提供采集接口的平台实现。
 * [POS]: 采集模块的平台适配，与外壳隔离。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

// Linux 实现骨架。
//
// 坏消息：Linux 是三个平台里最难的，原因是显示服务器不统一。
//
//   X11   —— 传统路径，XGetImage / xcb 截屏，不需要授权。
//            但正在被淘汰，新发行版默认 Wayland。
//   Wayland —— 出于安全设计，**任何应用都不能直接读别人屏幕**。
//            唯一合规路径是 xdg-desktop-portal 的 ScreenCast 接口，
//            它会走 PipeWire 输出视频流，并且必须由用户每次确认分享哪块屏幕。
//
// 所以 Linux 上做不到「装完就静默采集」，必须：
//   1. 通过 portal 发起 ScreenCast 会话（用户会看到系统级的选择框）
//   2. 从 PipeWire 流里按需取帧
//   3. 处理会话失效后需要重新授权的逻辑
//
// 想省事的话，只能明说「本产品仅支持 X11」，但那会排除大量用户。
//
// 依赖（都需要 cgo 或额外的 FFI 绑定，是三个平台里成本最高的）：
//
//	X11:      github.com/jezek/xgb 或 libX11 via cgo
//	Wayland:  github.com/rajveermalviya/go-wayland
//	PipeWire: 需自己写 FFI 绑定，Go 生态里没有成熟的
//	OCR:      Tesseract via cgo（系统自带质量一般），或打包模型
//
// OCR 也是问题：Linux 没有 macOS Vision / Windows.Media.Ocr 这种
// 系统级高质量 OCR。只能 Tesseract（质量明显差一些，尤其中英混排）
// 或者塞一个本地模型（体积会从 3MB 涨到几百 MB）。
//
// 尚未实现 —— 这个文件目前只是让 collector 能在 Linux 上编译通过。

import "errors"

var errLinuxNotImplemented = errors.New("linux backend not implemented yet")

type linuxScreen struct{}

func newPlatformScreen() Screen {
	return linuxScreen{}
}

func (linuxScreen) FrontWindow() (string, string, int) {
	// TODO: X11 用 _NET_ACTIVE_WINDOW + _NET_WM_NAME
	//       Wayland 没有通用方案（合成器不暴露），只能降级
	return "", "", 0
}

func (linuxScreen) Permission(ask bool) PermState {
	// Wayland 下必须走 portal，没有「预授权」概念
	return PermUnknown
}

func (linuxScreen) Grab() *Frame {
	// TODO: X11 用 XGetImage；Wayland 走 portal + PipeWire
	return nil
}

type linuxOCR struct{}

func newOCR() OCREngine { return linuxOCR{} }

func (linuxOCR) Recognize(f *Frame, languages []string) (string, int, error) {
	// TODO: Tesseract
	return "", 0, errLinuxNotImplemented
}

func platformSaveJPEG(f *Frame, path string, quality int) bool { return false }
