package capture

// 平台无关的接口定义。
//
// 采集器只依赖这三个能力：抓帧、OCR、看前台窗口。
// 每个平台各自实现，主循环完全不关心底下是 CGDisplayCreateImage
// 还是 BitBlt 还是 PipeWire。

// Frame 是一帧屏幕画面。
//
// 抽象成 []byte 而不是各平台的原生句柄，是为了让 OCR 和编码这些
// 后续处理能在平台之间共享一套代码。代价是多一次拷贝，
// 但实测一轮 40-60ms 里拷贝只占 1-2ms，可以接受。
type Frame struct {
	W, H  int
	RGBA  []byte // 固定 RGBA 顺序，各平台自己转
	Blank bool   // 黑屏/全白（锁屏、显示器休眠、权限缺失）
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
	Recognize(f *Frame) (text string, lines int, err error)
}
