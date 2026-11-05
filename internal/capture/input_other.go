//go:build !darwin

/*
 * [INPUT]: 依赖非 macOS 平台的现有采集桩。
 * [OUTPUT]: 提供无输入观测与未知权限状态。
 * [POS]: 非 macOS 构建的采集观测适配层。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

type nativeObservations struct{}

func (nativeObservations) Intervals() (time.Duration, time.Duration, time.Duration) {
	return 365 * 24 * time.Hour, 365 * 24 * time.Hour, 365 * 24 * time.Hour
}
func (nativeObservations) Front() Front             { return Front{} }
func (nativeObservations) Locked() bool             { return false }
func (nativeObservations) Accessibility() PermState { return PermUnknown }
func RequestScreenPermission() PermState            { return newScreen().Permission(true) }
