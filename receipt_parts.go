/*
 * [INPUT]: 依赖小票模型、语言目录和 MyGo 绘图组件。
 * [OUTPUT]: 提供纸面条目、结算区、条码和锯齿边。
 * [POS]: 小票内容的纯视图层，由 receipt.go 负责裁剪与送纸。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"konseki/locales"
)

var receiptInk = ui.Hex("#1D1C1A")
var receiptFaded = ui.Hex("#76716A")
var receiptRule = ui.Hex("#BDB7AC")

func receiptDuration(msg locales.Messages, d time.Duration) string {
	minutes := max(0, int(d/time.Minute))
	if minutes < 60 {
		return msg.ReceiptMinutes(strconv.Itoa(minutes))
	}
	return msg.ReceiptHoursMinutes(strconv.Itoa(minutes/60), strconv.Itoa(minutes%60))
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
	msg := locales.In(c.Locale())
	day := a.receipt
	if len(day.Lines) == 0 {
		label := msg.ReceiptEmptyPast()
		if a.dayKey() == a.currentTime().Format("2006-01-02") {
			label = msg.ReceiptEmptyToday()
		}
		ui.Text(c, label).FontSize(13).TextColor(receiptFaded).Margin(12, 0, 12, 0)
		return
	}
	latest := day.Lines[0].End
	receiptLeader(c, msg.ReceiptUntil(latest.Format("15:04")), receiptDuration(msg, day.Totals.Active)+" · "+msg.ReceiptSegments(strconv.Itoa(day.Totals.Sessions)), true)
	receiptDashed(c)
	for i, line := range day.Lines {
		a.renderReceiptLine(c, line, i == 0)
		if i < len(day.Lines)-1 {
			receiptDashed(c)
		}
	}
	receiptDouble(c)
	if day.Totals.HasActive {
		receiptLeader(c, msg.ReceiptTotal(), receiptDuration(msg, day.Totals.Active), true)
	}
	receiptLeader(c, msg.ReceiptSessions(), strconv.Itoa(day.Totals.Sessions), false)
	receiptLeader(c, msg.ReceiptSnapshots(), strconv.Itoa(day.Totals.Snapshots), false)
	if len(day.Totals.TopApps) > 0 {
		receiptDashed(c)
		ui.Text(c, msg.ReceiptTopApps()).FontSize(12).Bold().TextColor(receiptInk)
		for _, app := range day.Totals.TopApps {
			receiptLeader(c, app.App, receiptDuration(msg, app.Duration), false)
		}
	}
	if len(day.Totals.Open) > 0 {
		receiptDashed(c)
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
func (a *app) renderReceiptLine(c *ui.Context, line ReceiptLine, first bool) {
	msg := locales.In(c.Locale())
	title := line.Headline
	if line.Pending {
		title = msg.ReceiptPending()
	}
	row := ui.ButtonBase(c.Key("receipt-line-" + line.ID)).FillWidth().AlignItems(ui.Start)
	if first {
		row.Bind(&a.receiptLineHandle)
	}
	row.Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(9).Children(func() {
				ui.Text(c, line.Start.Format("15:04")).Width(42).Shrink(0).FontSize(12).Font("monospace").FontFeatures("tnum").TextColor(receiptFaded)
				ui.Text(c, title).Grow(1).FontSize(13).Bold().LineHeight(1.35).TextColor(receiptInk)
				ui.Text(c, receiptDuration(msg, line.End.Sub(line.Start))).Shrink(0).FontSize(12).Font("monospace").TextColor(receiptInk)
			})
			ui.Column(c).FillWidth().Padding(0, 0, 0, 51).Gap(4).Children(func() {
				body := line.Summary
				if line.Pending {
					body = strings.Join(line.Titles, " · ")
				}
				if body != "" {
					ui.Text(c, body).FontSize(12).LineHeight(1.45).TextColor(receiptFaded)
				}
				if len(line.Apps) > 0 {
					ui.Text(c, strings.Join(line.Apps, " · ")).FontSize(11).Font("monospace").TextColor(receiptFaded)
				}
				if a.expanded[line.ID] {
					for _, d := range []struct {
						label  string
						values []string
					}{{msg.ReceiptDetailOpen(), line.Open}, {msg.ReceiptDetailDecision(), line.Decisions}, {msg.ReceiptDetailTopic(), line.Topics}, {msg.ReceiptDetailFile(), line.Files}} {
						for _, v := range d.values {
							ui.Text(c, d.label+"  "+v).FontSize(11.5).TextColor(receiptInk)
						}
					}
					ui.Text(c, fmt.Sprintf("%s  %s–%s", msg.ReceiptDetailPeriod(), line.Start.Format("15:04"), line.End.Format("15:04"))).FontSize(11.5).Font("monospace").TextColor(receiptInk)
				}
			})
		})
	})
	if row.Clicked() {
		a.expanded[line.ID] = !a.expanded[line.ID]
	}
}
