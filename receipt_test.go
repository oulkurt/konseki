/*
 * [INPUT]: 依赖固定小票夹具、MyGo 测试时钟和临时采集存储。
 * [OUTPUT]: 验证展开交互、送纸时序和减少动态效果。
 * [POS]: 小票视图的离屏行为测试，不启动原生窗口。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"github.com/egoist/mygo/ui"
	"konseki/internal/summary"
	"testing"
	"time"
)

func receiptTestApp(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	a, cleanup := newTestApp(t)
	t.Cleanup(cleanup)
	a.now = func() time.Time { return time.Date(2026, 11, 5, 16, 0, 0, 0, time.Local) }
	shotDay(t, a)
	a.expanded = map[string]bool{}
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, 900)
	tt.SetLocale("zh-Hans")
	return a, tt
}
func TestReceiptExpansion(t *testing.T) {
	_, tt := receiptTestApp(t)
	tt.Advance(time.Second)
	if tt.HasText("+ 决定") {
		t.Fatal("条目默认展开")
	}
	if err := tt.Click("起草小票时间轴的出纸动画"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("+ 决定") || !tt.HasText("+ 未完成") || !tt.HasText("+ 主题") {
		t.Fatalf("展开详情缺失：%q", tt.Texts())
	}
	if err := tt.Click("起草小票时间轴的出纸动画"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("+ 决定") {
		t.Fatal("条目没有收起")
	}
}
func TestReceiptFeed(t *testing.T) {
	a, tt := receiptTestApp(t)
	start := tt.Image().RGBAAt(390, 140)
	tt.Advance(time.Second)
	end := tt.Image().RGBAAt(390, 140)
	if start == end {
		t.Fatalf("首帧与停稳帧相同：%v", start)
	}
	if a.feedDistance <= 0 {
		t.Fatalf("纸高未测量: %v", a.feedDistance)
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.dayFilter = "2026-11-03"
	tt.Frame()
	reduced := tt.Image().RGBAAt(390, 140)
	if reduced != end {
		t.Fatalf("减少动态效果没有立即停稳：%v / %v", reduced, end)
	}
}
func TestReceiptNewLineFeed(t *testing.T) {
	a, tt := receiptTestApp(t)
	tt.Advance(time.Second)
	for _, entry := range a.sumEntries {
		if err := a.sums.Upsert(entry); err != nil {
			t.Fatal(err)
		}
	}
	a.lastLoad = time.Time{}
	a.refresh()
	tt.Frame()
	tt.Advance(time.Second)
	before := a.feedRevision
	startTime := time.Date(2026, 11, 4, 16, 0, 0, 0, time.Local)
	if err := a.sums.Upsert(summary.Entry{ID: "new", Day: "2026-11-04", Start: startTime, End: startTime.Add(5 * time.Minute), Headline: "新的条目"}); err != nil {
		t.Fatal(err)
	}
	a.lastLoad = time.Time{}
	a.refresh()
	tt.Frame()
	if a.feedRevision == before || a.feedDistance <= 0 {
		t.Fatalf("新条目没有送纸：%d %f", a.feedRevision, a.feedDistance)
	}
	start := append([]byte(nil), tt.Image().Pix...)
	tt.Advance(time.Second)
	if bytes.Equal(start, tt.Image().Pix) {
		t.Fatal("新条目送纸没有位移")
	}
}
