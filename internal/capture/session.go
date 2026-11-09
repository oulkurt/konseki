/*
 * [INPUT]: 依赖会话起止时间与采集统计。
 * [OUTPUT]: 提供 Session 和 EndReason 持久化契约；区分键鼠计数缺失与零值。
 * [POS]: 采集会话的数据模型，供存储和状态共享。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

type EndReason string

const (
	EndIdle    EndReason = "idle"
	EndLock    EndReason = "lock"
	EndSuspend EndReason = "suspend"
	EndStop    EndReason = "stop"
	EndCrash   EndReason = "crash"
)

type Session struct {
	ID        string    `json:"id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	EndReason EndReason `json:"endReason"`
	Apps      []string  `json:"apps"`
	Snapshots int       `json:"snapshots"`
	// Keys、Clicks 为 nil 表示没计，指向 0 表示计了没按。
	Keys   *int `json:"keys,omitempty"`
	Clicks *int `json:"clicks,omitempty"`
}
