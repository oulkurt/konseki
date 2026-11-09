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
	PhaseOff     Phase = iota // 总开关关闭
	PhasePaused               // 暂停到 PausedUntil
	PhaseWaiting              // 开着但没有会话，人不在电脑前
	PhaseActive               // 会话中
	PhaseBlocked              // 开着但被拦截，原因见 Block
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
	OCRDropped  int
}
