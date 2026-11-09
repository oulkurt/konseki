/*
 * [INPUT]: 依赖固定日期的摘要、采集块和会话。
 * [OUTPUT]: 验证小票归并、排序和结算规则。
 * [POS]: 小票模型的纯函数回归测试，不访问用户数据。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/summary"
)

func TestBuildReceipt(t *testing.T) {
	day := time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	blocks := []capture.Block{{Start: at(9, 0), End: at(9, 20), App: "A", Titles: []string{"窗口"}, Samples: 3}, {Start: at(9, 25), End: at(9, 35), App: "B", Samples: 2}, {Start: at(10, 0), End: at(10, 30), App: "A", Samples: 4}, {Start: at(11, 0), End: at(11, 10), App: "C", Samples: 1}, {Start: at(12, 0), End: at(12, 5), App: "D", Samples: 1}}
	sums := []summary.Entry{{ID: "nine", Start: at(9, 0), End: at(10, 0), Headline: "摘要", Open: []string{"待办", "待办"}}, {ID: "eleven", Start: at(11, 0), End: at(12, 0), Open: []string{"待办", "另一项"}}}
	cases := []struct {
		name      string
		sums      []summary.Entry
		blocks    []capture.Block
		sessions  []capture.Session
		lines     int
		active    time.Duration
		has       bool
		sessionsN int
	}{
		{"摘要与待摘要", sums, blocks, nil, 4, 75 * time.Minute, true, 0},
		{"会话优先", sums, blocks, []capture.Session{{Start: at(9, 0), End: at(9, 40)}}, 4, 40 * time.Minute, true, 1},
		{"仅摘要", sums, nil, nil, 2, 0, false, 0},
		{"空数据", nil, nil, nil, 0, 0, false, 0},
		{"跨午夜", nil, []capture.Block{{Start: day.Add(-time.Minute), End: at(0, 5), App: "夜间", Samples: 1}}, []capture.Session{{Start: day.Add(-time.Minute), End: at(0, 5)}}, 1, 5 * time.Minute, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := buildReceipt(day, tc.sums, tc.blocks, tc.sessions)
			if len(r.Lines) != tc.lines || r.Totals.Active != tc.active || r.Totals.HasActive != tc.has || r.Totals.Sessions != tc.sessionsN {
				t.Fatalf("结果不符: %+v", r)
			}
			for i := 1; i < len(r.Lines); i++ {
				if r.Lines[i].Start.After(r.Lines[i-1].Start) {
					t.Fatal("顺序错误")
				}
			}
		})
	}
	r := buildReceipt(day, sums, blocks, nil)
	if len(r.Totals.TopApps) != 3 || r.Totals.TopApps[0].App != "A" || r.Totals.Snapshots != 11 || len(r.Totals.Open) != 2 {
		t.Fatalf("结算不符: %+v", r.Totals)
	}
	if !r.Lines[0].Pending || r.Lines[0].Apps[0] != "D" || r.Lines[2].Apps[0] != "A" {
		t.Fatalf("待摘要不符: %+v", r.Lines)
	}
}
