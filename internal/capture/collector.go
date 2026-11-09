/*
 * [INPUT]: 依赖平台实现提供帧、识别结果和系统观测。
 * [OUTPUT]: 提供采集外壳可替换的依赖接口和累计键鼠计数。
 * [POS]: 采集外壳与平台实现之间的契约。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

// 平台无关的接口定义。
//
// 采集器只依赖这三个能力：抓帧、OCR、看前台窗口。
// 每个平台各自实现，主循环完全不关心底下是 CGDisplayCreateImage
// 还是 BitBlt 还是 PipeWire。

// Frame 包含平台原生帧或测试用 RGBA 像素。
type Frame struct {
	W, H   int
	RGBA   []byte
	Blank  bool
	native uintptr
}

// Screen 是一个平台的屏幕采集实现。
type Screen interface {
	// FrontWindow 返回前台 App 名、窗口标题、进程号。
	// 拿不到时返回空字符串，调用方据此跳过。
	FrontWindow() (app, title string, pid int)

	// Grab 抓一帧全分辨率屏幕。
	// 返回 nil 表示失败（权限缺失、显示器无信号等）。
	Grab() *Frame

	// Permission 查询/请求屏幕录制权限。
	// ask=true 时会尝试弹框（仅 macOS 有真正的弹框机制）。
	Permission(ask bool) PermState
}

// PermState 是权限的三态。
//
// 用三态而不是 bool，是因为 macOS 上「未知」和「被拒绝」要区别对待：
// 未知时应该尝试弹框，被拒绝时弹框不会有效果，只能引导用户去系统设置。
type PermState int

const (
	PermUnknown PermState = iota
	PermGranted
	PermDenied
)

func (p PermState) String() string {
	switch p {
	case PermGranted:
		return "granted"
	case PermDenied:
		return "denied"
	default:
		return "unknown"
	}
}

// OCREngine 是一个平台的文字识别实现。
type OCREngine interface {
	// Recognize 识别一帧里的文字，返回文本和行数。
	// 应当能处理中英日混排。
	Recognize(f *Frame, languages []string) (text string, lines int, err error)
}

// InputSource 返回距最近一次输入的时间。
type InputSource interface {
	Intervals() (key, click, scroll time.Duration)
}

// InputTotals 是系统启动以来的累计按键和点击次数，只有次数，没有内容。
// Secure 为真表示系统安全输入打开，例如焦点在密码框里。
type InputTotals struct {
	Keys, Clicks uint32
	Secure       bool
}

// TotalsSource 返回系统累计键鼠计数。
type TotalsSource interface{ Totals() (InputTotals, bool) }

type FrontSource interface{ Front() Front }
type LockSource interface{ Locked() bool }
type Clock interface {
	Now() time.Time
	Ticks() (<-chan time.Time, func())
}
type PermissionSource interface{ Accessibility() PermState }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) Ticks() (<-chan time.Time, func()) {
	t := time.NewTicker(250 * time.Millisecond)
	return t.C, t.Stop
}

// 可选计时接口保持现有 Clock 实现兼容，并让维护调度可控。
type maintenanceClock interface {
	Timer(time.Duration) (<-chan time.Time, func())
	Ticker(time.Duration) (<-chan time.Time, func())
}

func (systemClock) Timer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}
func (systemClock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

const (
	ScreenPermissionSettingsURL        = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	AccessibilityPermissionSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
)
