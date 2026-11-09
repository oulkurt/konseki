/*
 * [INPUT]: 依赖固定小票夹具、MyGo 测试时钟、临时采集存储和真实语言目录。
 * [OUTPUT]: 验证展开交互、对齐和送纸时序。验证新条目印出时旧条目不跳。
 * 验证减少动态效果、时长文案、累计行文案和待摘要标题。
 * [POS]: 小票视图的离屏行为测试，不启动原生窗口。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"testing"
	"time"

	"konseki/internal/summary"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
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

func hasExactText(tt *ui.Tester, s string) bool {
	for _, text := range tt.Texts() {
		if text == s {
			return true
		}
	}
	return false
}

func TestReceiptExpansion(t *testing.T) {
	_, tt := receiptTestApp(t)
	tt.Advance(time.Second)
	if tt.HasText("+ 决定") {
		t.Fatal("条目默认展开")
	}
	if err := tt.Click("起草小票时间轴的出纸动画并检查长标题在纸面上的换行方式"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("+ 决定") || !tt.HasText("+ 未完成") || !tt.HasText("+ 主题") {
		t.Fatalf("展开详情缺失：%q", tt.Texts())
	}
	var column float32 = -1
	for _, value := range []string{"长标题换行观感未验证", "使用悬挂式小票", "MyGo", "动画", "receipt.go", "13:00–14:00"} {
		r, ok := tt.Find(value)
		if !ok {
			t.Fatalf("展开详情缺少 %q：%q", value, tt.Texts())
		}
		if column < 0 {
			column = r.X
		}
		if r.X != column {
			t.Fatalf("展开详情的值没有对齐：%q 在 x=%.1f，其他在 x=%.1f", value, r.X, column)
		}
	}
	if err := tt.Click("起草小票时间轴的出纸动画并检查长标题在纸面上的换行方式"); err != nil {
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

// 换上一张短纸时，送纸距离要量新纸。沿用旧纸的高度，短纸会藏在打印机里大半程，最后猛地掉出来。
// 清除成功后印空白小票走这条路，从长的一天切到短的一天也走这条路。
func TestReceiptFeedMeasuresNewPaper(t *testing.T) {
	a, tt := receiptTestApp(t)
	tt.Advance(time.Second)
	long := a.feedDistance
	a.receipt = ReceiptDay{}
	a.stopShred()
	tt.Frame()
	tt.Advance(time.Second / 60)
	if a.feedDistance <= 0 || a.feedDistance >= long/2 {
		t.Fatalf("送纸距离 = %.1f，应该是空白小票的高度，不是旧纸的 %.1f", a.feedDistance, long)
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
	before, ok := tt.Find("13:00")
	if !ok {
		t.Fatalf("找不到原来的第一行：%q", tt.Texts())
	}
	startTime := time.Date(2026, 11, 4, 16, 0, 0, 0, time.Local)
	if err := a.sums.Upsert(summary.Entry{ID: "new", Day: "2026-11-04", Start: startTime, End: startTime.Add(5 * time.Minute), Headline: "新的条目"}); err != nil {
		t.Fatal(err)
	}
	a.lastLoad = time.Time{}
	tt.Frame()
	ys := []float32{}
	for i := 0; i < 50; i++ {
		r, _ := tt.Find("13:00")
		ys = append(ys, r.Y)
		tt.Advance(16 * time.Millisecond)
	}
	after, _ := tt.Find("13:00")
	if !hasExactText(tt, "新的条目") {
		t.Fatalf("新条目没有印出来：%q", tt.Texts())
	}
	moved := after.Y - before.Y
	if moved <= 0 {
		t.Fatalf("新条目没有把旧条目往下推：%.1f → %.1f", before.Y, after.Y)
	}
	if d := ys[0] - before.Y; d < -0.5 || d > 0.5 {
		t.Fatalf("新条目出现的那一帧旧条目就动了 %.1f", d)
	}
	prev := before.Y
	for i, y := range ys {
		if y < prev-0.5 || y-prev > moved*0.1+0.5 {
			t.Fatalf("第 %d 帧旧条目从 %.1f 跳到 %.1f（全程 %.1f），新条目不是平滑印出来的", i, prev, y, moved)
		}
		prev = y
	}
	settled := append([]byte(nil), tt.Image().Pix...)
	tt.Advance(time.Second)
	if !bytes.Equal(settled, tt.Image().Pix) {
		t.Fatal("印完以后纸还在动")
	}
}

func TestReceiptShowAt(t *testing.T) {
	a, tt := receiptTestApp(t)
	for _, entry := range a.sumEntries {
		if err := a.sums.Upsert(entry); err != nil {
			t.Fatal(err)
		}
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.SetSize(780, 400)
	at := time.Date(2026, 11, 4, 9, 30, 0, 0, time.Local)
	a.showReceiptAt(at, at)
	tt.Frame()
	if a.tab != 0 || a.dayKey() != "2026-11-04" || a.receiptTarget != "" || a.scrollState.Y <= 0 {
		t.Fatalf("定位失败：日期=%s 偏移=%v 目标=%q", a.dayKey(), a.scrollState.Y, a.receiptTarget)
	}
}

func TestReceiptDurationAndHeadline(t *testing.T) {
	zh, en := locales.In("zh-Hans"), locales.In("en")
	for _, c := range []struct {
		d      time.Duration
		zh, en string
	}{
		{30 * time.Second, "<1分", "<1m"},
		{47 * time.Minute, "47分", "47m"},
		{time.Hour, "1小时", "1h"},
		{3*time.Hour + 47*time.Minute, "3小时47分", "3h 47m"},
	} {
		if got := receiptDuration(zh, c.d); got != c.zh {
			t.Errorf("中文时长 %v = %q，期望 %q", c.d, got, c.zh)
		}
		if got := receiptDuration(en, c.d); got != c.en {
			t.Errorf("英文时长 %v = %q，期望 %q", c.d, got, c.en)
		}
	}
	oneSession := ReceiptDay{Lines: make([]ReceiptLine, 2), Totals: ReceiptTotals{HasActive: true, Active: time.Hour, Sessions: 1}}
	if got := receiptHeadline(zh, oneSession); got != "1小时 · 1 段" {
		t.Errorf("中文累计 = %q", got)
	}
	if got := receiptHeadline(en, oneSession); got != "1h · 1 session" {
		t.Errorf("英文累计 = %q", got)
	}
	summariesOnly := ReceiptDay{Lines: make([]ReceiptLine, 3)}
	if got := receiptHeadline(zh, summariesOnly); got != "3 条" {
		t.Errorf("只剩摘要的中文累计 = %q", got)
	}
	if got := receiptHeadline(en, summariesOnly); got != "3 entries" {
		t.Errorf("只剩摘要的英文累计 = %q", got)
	}
}

func TestReceiptSummariesOnlyDay(t *testing.T) {
	a, tt := receiptTestApp(t)
	a.blocks = nil
	a.receipt = buildReceipt(time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local), a.sumEntries, nil, nil)
	tt.Advance(time.Second)
	if !hasExactText(tt, "3 条") {
		t.Fatalf("只剩摘要的日子应印条目数：%q", tt.Texts())
	}
	for _, s := range []string{"会话", "快照", "合计", "0分 · 0 段"} {
		if hasExactText(tt, s) {
			t.Fatalf("没有采集记录的日子印出了 %q：%q", s, tt.Texts())
		}
	}
}

func TestReceiptPendingTitles(t *testing.T) {
	_, tt := receiptTestApp(t)
	tt.Advance(time.Second)
	if !hasExactText(tt, "receipt.go - 小票时间轴，出纸口与送纸动画的第一版实现，标题够长才能看出换行") {
		t.Fatalf("待摘要行应以窗口标题为标题：%q", tt.Texts())
	}
	if !hasExactText(tt, "未摘要 · Editor") {
		t.Fatalf("待摘要行缺少「未摘要」标记：%q", tt.Texts())
	}
	if hasExactText(tt, "未摘要") {
		t.Fatalf("有窗口标题时不该再用「未摘要」当标题：%q", tt.Texts())
	}
	tt.SetLocale("en")
	tt.Frame()
	if !hasExactText(tt, "Not summarized · Editor") {
		t.Fatalf("英文待摘要标记缺失：%q", tt.Texts())
	}
}
