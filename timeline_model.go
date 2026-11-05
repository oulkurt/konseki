/*
 * [INPUT]: 依赖摘要、采集块、会话与本地日期。
 * [OUTPUT]: 提供小票日期、条目、结算模型和纯聚合函数。
 * [POS]: 时间轴的数据边界。刷新时生成，视图只读取结果。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"konseki/internal/capture"
	"konseki/internal/summary"
)

type ReceiptDay struct {
	Day       time.Time
	Lines     []ReceiptLine
	Totals    ReceiptTotals
	StartedAt time.Time
	Serial    string
}
type ReceiptLine struct {
	ID                                     string
	Start, End                             time.Time
	Headline, Summary                      string
	Apps, Titles                           []string
	Pending                                bool
	Open, Topics, Files, People, Decisions []string
}
type AppTime struct {
	App      string
	Duration time.Duration
}
type ReceiptTotals struct {
	Active              time.Duration
	HasActive           bool
	Sessions, Snapshots int
	TopApps             []AppTime
	Open                []string
}

func buildReceipt(day time.Time, sums []summary.Entry, blocks []capture.Block, sessions []capture.Session) ReceiptDay {
	return buildReceiptWindow(day, sums, blocks, sessions, time.Hour)
}

func buildReceiptWindow(day time.Time, sums []summary.Entry, blocks []capture.Block, sessions []capture.Session, window time.Duration) ReceiptDay {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	end := start.AddDate(0, 0, 1)
	if window <= 0 {
		window = time.Hour
	}
	result := ReceiptDay{Day: start}
	seenOpen := map[string]bool{}
	for _, s := range sums {
		if !s.Start.Before(end) || !s.End.After(start) {
			continue
		}
		line := ReceiptLine{ID: s.ID, Start: s.Start, End: s.End, Headline: s.Headline, Summary: s.Summary, Apps: append([]string(nil), s.Apps...), Open: s.Open, Topics: s.Topics, Files: s.Files, People: s.People, Decisions: s.Decisions}
		result.Lines = append(result.Lines, line)
		for _, v := range s.Open {
			v = strings.TrimSpace(v)
			if v != "" && !seenOpen[v] {
				seenOpen[v] = true
				result.Totals.Open = append(result.Totals.Open, v)
			}
		}
	}
	appDur := map[string]time.Duration{}
	pending := map[int64]*ReceiptLine{}
	for _, b := range blocks {
		if !b.Start.Before(end) || b.End.Before(start) {
			continue
		}
		bs, be := maxTime(b.Start, start), minTime(b.End, end)
		duration := max(be.Sub(bs), 0)
		result.Totals.Snapshots += b.Samples
		result.Totals.Active += duration
		result.Totals.HasActive = true
		if b.App != "" {
			appDur[b.App] += duration
		}
		if result.StartedAt.IsZero() || bs.Before(result.StartedAt) {
			result.StartedAt = bs
		}
		// 摘要覆盖的窗口不再生成待摘要行。
		for cursor := bs; cursor.Before(be) || cursor.Equal(bs); {
			elapsed := cursor.Sub(start)
			bucket := int64(elapsed / window)
			ws := start.Add(time.Duration(bucket) * window)
			we := minTime(ws.Add(window), end)
			covered := false
			for _, s := range sums {
				if s.Start.Before(we) && s.End.After(ws) {
					covered = true
					break
				}
			}
			if !covered {
				line := pending[bucket]
				if line == nil {
					line = &ReceiptLine{ID: fmt.Sprintf("pending-%s-%d", start.Format("2006-01-02"), bucket), Start: cursor, End: minTime(be, we), Pending: true}
					pending[bucket] = line
				}
				line.Start = minTime(line.Start, cursor)
				line.End = maxTime(line.End, minTime(be, we))
				line.Apps = appendUnique(line.Apps, b.App)
				for _, title := range b.Titles {
					line.Titles = appendUnique(line.Titles, title)
				}
			}
			if !we.Before(be) {
				break
			}
			cursor = we
		}
	}
	for _, line := range pending {
		result.Lines = append(result.Lines, *line)
	}
	if len(sessions) > 0 {
		result.Totals.Active = 0
		result.Totals.HasActive = false
		for _, s := range sessions {
			if !s.Start.Before(end) || s.End.Before(start) {
				continue
			}
			result.Totals.Sessions++
			result.Totals.HasActive = true
			result.Totals.Active += max(minTime(s.End, end).Sub(maxTime(s.Start, start)), 0)
		}
		if !result.Totals.HasActive && len(blocks) > 0 {
			for _, b := range blocks {
				if b.Start.Before(end) && !b.End.Before(start) {
					result.Totals.HasActive = true
					result.Totals.Active += max(minTime(b.End, end).Sub(maxTime(b.Start, start)), 0)
				}
			}
		}
	}
	for name, d := range appDur {
		result.Totals.TopApps = append(result.Totals.TopApps, AppTime{name, d})
	}
	sort.Slice(result.Totals.TopApps, func(i, j int) bool {
		a, b := result.Totals.TopApps[i], result.Totals.TopApps[j]
		if a.Duration == b.Duration {
			return a.App < b.App
		}
		return a.Duration > b.Duration
	})
	if len(result.Totals.TopApps) > 3 {
		result.Totals.TopApps = result.Totals.TopApps[:3]
	}
	sort.SliceStable(result.Lines, func(i, j int) bool {
		if result.Lines[i].Start.Equal(result.Lines[j].Start) {
			return result.Lines[i].ID < result.Lines[j].ID
		}
		return result.Lines[i].Start.After(result.Lines[j].Start)
	})
	result.Serial = fmt.Sprintf("%s-%04d", start.Format("20060102"), result.Totals.Snapshots)
	return result
}
func appendUnique(values []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return values
	}
	for _, x := range values {
		if x == v {
			return values
		}
	}
	return append(values, v)
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
