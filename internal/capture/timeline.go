/*
 * [INPUT]: 依赖快照存储和采集时钟。
 * [OUTPUT]: 提供 Block、Timeline 和 Aggregate。
 * [POS]: 将快照聚合为时间轴，不参与采集。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"fmt"
	"sort"
	"time"
)

// ---------- 时间轴聚合 ----------

// Block 是一个时间段：同一 App、间隔小于 10 分钟的连续采样归并而成。
type Block struct {
	Start, End time.Time
	App        string
	Titles     []string
	Samples    int
}

// Duration 返回这个时间块的跨度。
func (b Block) Duration() time.Duration { return b.End.Sub(b.Start) }

// Timeline 读某一天的数据并聚合成时间块。
// dateKey 形如 2026-11-04；传空字符串表示今天。
func (r *Runner) Timeline(dateKey string) ([]Block, error) {
	if dateKey == "" {
		dateKey = r.deps.clock.Now().Format("2006-01-02")
	}
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	snaps, err := st.ReadSnapshots(dateKey)
	if err != nil {
		return nil, err
	}
	return Aggregate(snaps), nil
}

// Aggregates 把原始采样聚合成时间块。
//
// 规则：同一 App 且与上一块的间隔小于 10 分钟，就并入同一块。
// 这样一次连续的工作不会因为中间心跳几次就被切碎。
func Aggregate(snaps []Snapshot) []Block {
	var blocks []Block
	for _, s := range snaps {
		ts, err := parseTS(s.TS)
		if err != nil {
			continue
		}
		if n := len(blocks); n > 0 && blocks[n-1].App == s.App && ts.Sub(blocks[n-1].End) < 10*time.Minute {
			b := &blocks[n-1]
			b.End = ts
			b.Samples++
			if s.Title != "" && (len(b.Titles) == 0 || b.Titles[len(b.Titles)-1] != s.Title) {
				b.Titles = append(b.Titles, s.Title)
			}
			continue
		}
		blocks = append(blocks, Block{Start: ts, End: ts, App: s.App, Titles: []string{s.Title}, Samples: 1})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start.Before(blocks[j].Start) })
	return blocks
}

// parseTS 容忍多种时间格式，避免数据源差异导致整段丢记录。
func parseTS(v string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339, time.RFC3339Nano,
		"2006-01-02T15:04:05", "2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", v)
}
