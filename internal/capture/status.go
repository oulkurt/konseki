/*
 * [INPUT]: 依赖会话记录、权限状态与系统时间。
 * [OUTPUT]: 提供采集状态、前台窗口和用量契约。
 * [POS]: 采集循环向界面公开的只读状态边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

type Phase uint8

const (
	PhaseOff Phase = iota
	PhasePaused
	PhaseWaiting
	PhaseActive
	PhaseBlocked
)

type BlockReason uint8

const (
	BlockNone BlockReason = iota
	BlockSensitiveApp
	BlockScreenLocked
	BlockNoScreenPermission
)

type Front struct {
	App, Bundle, Title string
	PID                int
}
type Perms struct{ Screen, Accessibility PermState }
type Usage struct {
	Bytes                      int64
	Shots, Snapshots, Sessions int
}
type Status struct {
	Phase       Phase
	Block       BlockReason
	PausedUntil time.Time
	Session     *Session
	Front       Front
	Usage       Usage
	Perms       Perms
	LastShot    time.Time
	LastErr     error
}
