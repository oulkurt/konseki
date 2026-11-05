/*
 * [INPUT]: 依赖调用方提供采集目录和运行参数。
 * [OUTPUT]: 提供 Settings 和 OCRSettings 采集契约。
 * [POS]: 采集模块的配置边界，不依赖应用设置。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

type Settings struct {
	Root          string
	Debounce      time.Duration
	Heartbeat     time.Duration
	Idle          time.Duration
	TypingPause   time.Duration
	ScreenPoll    time.Duration
	JPEGQuality   int
	OCR           OCRSettings
	InputMonitor  bool
	SensitiveApps []string
	MaxStorage    int64
	RetentionDays int
}
type OCRSettings struct {
	Enabled   bool
	Languages []string
	EveryN    int
}

const (
	dedupWidth               = 160
	dedupHeight              = 90
	histogramThreshold       = 0.05
	pixelDifferenceThreshold = 0.02
	channelTolerance         = 30
	screenshotMaxDimension   = 2560
)
