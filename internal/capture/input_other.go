//go:build !darwin

/*
 * [INPUT]: 依赖 capture_gate.go 保护公开授权入口；依赖非 macOS 平台的现有采集桩。
 * [OUTPUT]: 提供无输入观测和不可用的累计键鼠计数、空的运行中应用列表、未知权限及授权请求桩。
 * [POS]: 非 macOS 构建的采集观测适配层。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

type nativeObservations struct{}

func (nativeObservations) Intervals() (time.Duration, time.Duration, time.Duration) {
	return 365 * 24 * time.Hour, 365 * 24 * time.Hour, 365 * 24 * time.Hour
}
func (nativeObservations) Totals() (InputTotals, bool) { return InputTotals{}, false }
func (nativeObservations) Front() Front                { return Front{} }
func (nativeObservations) Locked() bool                { return false }
func (nativeObservations) Accessibility() PermState    { return PermUnknown }
func platformRequestScreenPermission() PermState {
	return newPlatformScreen().Permission(true)
}

func platformRequestAccessibility() PermState {
	return PermUnknown
}

// RunningApps 在其他平台返回空列表，设置页只提供当前前台应用。
func RunningApps() []Front { return nil }
