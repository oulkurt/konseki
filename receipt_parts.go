/*
 * [INPUT]: 依赖小票模型、语言目录和 MyGo 绘图组件。
 * [OUTPUT]: 提供纸面条目、累计行、结算区、展开详情、条码和锯齿边。
 * [POS]: 小票内容的纯视图层。receipt.go 负责整张纸的送纸和新条目的展开高度，这里按展开高度裁出第一块。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

var receiptInk = ui.Hex("#1D1C1A")
var receiptFaded = ui.Hex("#76716A")
var receiptRule = ui.Hex("#BDB7AC")

// receiptDetailLabel 是展开详情左列的宽度。按英文最长的「+ Decision」留余量，两种语言共用一列，值才能对齐。
const receiptDetailLabel = 70

func receiptDuration(msg locales.Messages, d time.Duration) string {
	minutes := int(d / time.Minute)
	switch {
	case minutes < 1:
		return msg.ReceiptUnderMinute()
	case minutes < 60:
		return msg.ReceiptMinutes(strconv.Itoa(minutes))
	case minutes%60 == 0:
		return msg.ReceiptHours(strconv.Itoa(minutes / 60))
	}
	return msg.ReceiptHoursMinutes(strconv.Itoa(minutes/60), strconv.Itoa(minutes%60))
}

// receiptHeadline 只印有数据撑腰的累计。原始采集被保留期清掉、只剩摘要的日子，
// 时长和会话数都无从知道，改印条目数；印「0分 · 0 段」会让人以为那天什么都没做。
func receiptHeadline(msg locales.Messages, day ReceiptDay) string {
	var parts []string
	if day.Totals.HasActive {
		parts = append(parts, receiptDuration(msg, day.Totals.Active))
	}
	if day.Totals.Sessions > 0 {
		parts = append(parts, msg.ReceiptSegments(day.Totals.Sessions))
	}
	if len(parts) == 0 {
		parts = append(parts, msg.ReceiptEntries(len(day.Lines)))
	}
	return strings.Join(parts, " · ")
}

func receiptDashed(c *ui.Context) {
	ui.Box(c).FillWidth().Height(9).Draw(func(p *ui.Painter, r ui.Rect) {
		var path ui.Path
		path.MoveTo(r.X, r.Y+r.H/2).LineTo(r.X+r.W, r.Y+r.H/2).Dash(0, 4, 3)
		p.StrokePath(&path, 1, receiptRule)
	})
}

func receiptDouble(c *ui.Context) {
	ui.Box(c).FillWidth().Height(10).Draw(func(p *ui.Painter, r ui.Rect) {
		y := r.Y + r.H/2
		p.Line(r.X, y-1.5, r.X+r.W, y-1.5, 1, receiptInk)
		p.Line(r.X, y+1.5, r.X+r.W, y+1.5, 1, receiptInk)
	})
}

func receiptLeader(c *ui.Context, label, value string, bold bool) {
	ui.Row(c).FillWidth().AlignItems(ui.End).Gap(6).Children(func() {
		l := ui.Text(c, label).FontSize(12).TextColor(receiptInk)
		if bold {
			l.Bold()
		}
		ui.Box(c).Grow(1).Height(12).Draw(func(p *ui.Painter, r ui.Rect) {
			var path ui.Path
			path.MoveTo(r.X, r.Y+r.H-3).LineTo(r.X+r.W, r.Y+r.H-3).Dash(0, 1.2, 3.2)
			p.StrokePath(&path, 1, receiptFaded)
		})
		v := ui.Text(c, value).FontSize(12).Font("monospace").FontFeatures("tnum").TextColor(receiptInk)
		if bold {
			v.Bold()
		}
	})
}

func receiptZigzag(c *ui.Context, paper ui.Color) {
	ui.Box(c).FillWidth().Height(8).Draw(func(p *ui.Painter, r ui.Rect) {
		n := max(1, int(r.W/9))
		w := r.W / float32(n)
		var path ui.Path
		path.MoveTo(r.X, r.Y)
		for i := 0; i < n; i++ {
			x := r.X + float32(i)*w
			path.LineTo(x+w/2, r.Y+r.H).LineTo(x+w, r.Y)
		}
		path.Close()
		p.FillPath(&path, paper)
	})
}

func receiptBarcode(c *ui.Context, seed string) {
	ui.Box(c).Width(220).Height(40).Draw(func(p *ui.Painter, r ui.Rect) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(seed))
		v := h.Sum64()
		for x := r.X; x < r.X+r.W-4; {
			w := float32(1 + v%3)
			v = v*6364136223846793005 + 1442695040888963407
			gap := float32(1 + v%3)
			v = v*6364136223846793005 + 1442695040888963407
			p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, receiptInk, 0)
			x += w + gap
		}
	})
}

func (a *app) renderReceiptContents(c *ui.Context) {
	a.renderReceiptDayContents(c, a.receipt, true)
}

func (a *app) renderReceiptDayContents(c *ui.Context, day ReceiptDay, interactive bool) {
	msg := locales.In(c.Locale())
	if len(day.Lines) == 0 {
		label := msg.ReceiptEmptyPast()
		if a.dayKey() == a.currentTime().Format("2006-01-02") {
			label = msg.ReceiptEmptyToday()
		}
		ui.Text(c, label).FontSize(13).TextColor(receiptFaded).Margin(12, 0, 12, 0)
		return
	}
	receiptLeader(c, msg.ReceiptUntil(day.Lines[0].End.Format("15:04")), receiptHeadline(msg, day), true)
	receiptDashed(c)
	var reveal float32
	printing := false
	if interactive {
		reveal, printing = a.printReveal(c)
	}
	// 条目列不用父级的 Gap，间距写在每一块里：正在印出的第一块高度为 0 时，下面的旧条目才不会多出一道间距。
	ui.Column(c).FillWidth().Children(func() {
		for i, line := range day.Lines {
			block := func() {
				a.renderReceiptLine(c, line, interactive)
				if i < len(day.Lines)-1 {
					ui.Box(c).Height(9)
					receiptDashed(c)
					ui.Box(c).Height(9)
				}
			}
			if i == 0 && printing {
				ui.Column(c).FillWidth().Height(reveal).Clip().Children(func() {
					ui.Column(c).FillWidth().Shrink(0).Bind(&a.printHandle).Translate(0, min(0, reveal-a.printHeight)).Children(block)
				})
				continue
			}
			block()
		}
	})
	receiptDouble(c)
	// 合计那一节可能整节不印。虚线跟着节走，双线下面才不会紧接一条虚线。
	printed := false
	section := func() {
		if printed {
			receiptDashed(c)
		}
		printed = true
	}
	if day.Totals.HasActive || day.Totals.Sessions > 0 || day.Totals.Snapshots > 0 {
		section()
		if day.Totals.HasActive {
			receiptLeader(c, msg.ReceiptTotal(), receiptDuration(msg, day.Totals.Active), true)
		}
		if day.Totals.Sessions > 0 {
			receiptLeader(c, msg.ReceiptSessions(), strconv.Itoa(day.Totals.Sessions), false)
		}
		if day.Totals.Snapshots > 0 {
			receiptLeader(c, msg.ReceiptSnapshots(), strconv.Itoa(day.Totals.Snapshots), false)
		}
	}
	if len(day.Totals.TopApps) > 0 {
		section()
		ui.Text(c, msg.ReceiptTopApps()).FontSize(12).Bold().TextColor(receiptInk)
		for _, app := range day.Totals.TopApps {
			receiptLeader(c, app.App, receiptDuration(msg, app.Duration), false)
		}
	}
	if len(day.Totals.Open) > 0 {
		section()
		ui.Text(c, msg.ReceiptOpen()).FontSize(12).Bold().TextColor(receiptInk)
		for _, item := range day.Totals.Open {
			ui.Text(c, "□ "+item).FontSize(12).TextColor(receiptInk)
		}
	}
	receiptDashed(c)
	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(5).Padding(6, 0, 2, 0).Children(func() {
		receiptBarcode(c, day.Serial)
		ui.Text(c, msg.ReceiptSerial(day.Serial)).FontSize(10.5).Font("monospace").TextColor(receiptFaded)
		ui.Text(c, msg.ReceiptThanks()).FontSize(11).TextColor(receiptFaded)
	})
}

func (a *app) renderReceiptLine(c *ui.Context, line ReceiptLine, interactive bool) {
	msg := locales.In(c.Locale())
	title, body, meta := line.Headline, line.Summary, line.Apps
	bold := true
	if line.Pending {
		// 待摘要行用窗口标题当标题，几行之间才分得出来；常规字重加「未摘要」标记，
		// 和模型写的粗体结论区分开。
		title, body = msg.ReceiptPending(), ""
		if len(line.Titles) > 0 {
			title, body, bold = line.Titles[0], strings.Join(line.Titles[1:], " · "), false
			meta = append([]string{msg.ReceiptPending()}, line.Apps...)
		}
	}
	row := ui.ButtonBase(c.Key("receipt-line-" + line.ID)).FillWidth().AlignItems(ui.Start).Disabled(!interactive)
	if a.receiptTarget == line.ID && a.receiptTarget != "" {
		row.ScrollIntoView()
		a.receiptTarget = ""
	}
	row.Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(9).Children(func() {
				ui.Text(c, line.Start.Format("15:04")).Width(42).Shrink(0).FontSize(12).Font("monospace").FontFeatures("tnum").TextColor(receiptFaded)
				t := ui.Text(c, title).Grow(1).FontSize(13).LineHeight(1.35).TextColor(receiptInk)
				if bold {
					t.Bold()
				}
				ui.Text(c, receiptDuration(msg, line.End.Sub(line.Start))).Shrink(0).FontSize(12).Font("monospace").TextColor(receiptInk)
			})
			ui.Column(c).FillWidth().Padding(0, 0, 0, 51).Gap(4).Children(func() {
				if body != "" {
					ui.Text(c, body).FontSize(12).LineHeight(1.45).TextColor(receiptFaded)
				}
				if len(meta) > 0 {
					ui.Text(c, strings.Join(meta, " · ")).FontSize(11).Font("monospace").TextColor(receiptFaded)
				}
				if a.expanded[line.ID] {
					renderReceiptDetails(c, msg, line)
				}
			})
		})
	})
	if row.Clicked() {
		a.expanded[line.ID] = !a.expanded[line.ID]
	}
}

// renderReceiptDetails 印两列：标签一列、值一列。同一组有多个值时只在第一行印标签。
func renderReceiptDetails(c *ui.Context, msg locales.Messages, line ReceiptLine) {
	period := line.Start.Format("15:04") + "–" + line.End.Format("15:04")
	for _, g := range []struct {
		label  string
		values []string
		mono   bool
	}{
		{msg.ReceiptDetailOpen(), line.Open, false},
		{msg.ReceiptDetailDecision(), line.Decisions, false},
		{msg.ReceiptDetailTopic(), line.Topics, false},
		{msg.ReceiptDetailFile(), line.Files, false},
		{msg.ReceiptDetailPeriod(), []string{period}, true},
	} {
		for i, v := range g.values {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(6).Children(func() {
				if i == 0 {
					ui.Text(c, g.label).Width(receiptDetailLabel).Shrink(0).FontSize(11.5).TextColor(receiptInk)
				} else {
					ui.Box(c).Width(receiptDetailLabel).Shrink(0)
				}
				value := ui.Text(c, v).Grow(1).FontSize(11.5).LineHeight(1.4).TextColor(receiptInk)
				if g.mono {
					value.Font("monospace").FontFeatures("tnum")
				}
			})
		}
	}
}
