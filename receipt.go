/*
 * [INPUT]: 依赖缓存的小票、采集状态与 MyGo 的日期和动画能力。
 * [OUTPUT]: 提供固定打印机、日期选择、整张纸的送纸动画，以及新条目从累计行下面展开的印出高度。
 * [POS]: 时间轴页面的布局边界，纸面内容由 receipt_parts.go 绘制。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"math"
	"time"

	"konseki/internal/capture"
	"konseki/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

func (a *app) renderTimelinePage(c *ui.Context, pal palette, inset float32) {
	w, _ := c.Size()
	paperWidth := min(float32(520), max(float32(0), w-40))
	ui.Column(c).Fill().Padding(inset, 0, 0, 0).Children(func() {
		ui.Column(c).FillWidth().Padding(0, 22, 12, 22).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Padding(0, 6).DragWindow().Children(func() { ui.Text(c, "痕跡 Konseki").FontSize(11.5).Bold().TextColor(pal.textSecondary) })
			a.renderTabs(c, pal)
			if err := a.bannerError(); err != nil {
				ui.Text(c, locales.In(c.Locale()).ShellReadError()+errorText(c.Locale(), err)).TextColor(c.Theme().Danger)
			}
			if a.summary.phase == summaryFailed && (a.summary.dayKey == "" || a.summary.dayKey == a.dayKey()) {
				if text := a.summary.errorText(c.Locale()); text != "" {
					ui.Text(c, text).FontSize(12).TextColor(c.Theme().Danger)
				}
			} else if a.bannerError() == nil && a.summaryReadErr != nil {
				ui.Text(c, locales.In(c.Locale()).ShellReadError()+errorText(c.Locale(), a.summaryReadErr)).FontSize(12).TextColor(c.Theme().Danger)
			}
		})
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() { a.renderPrinter(c, paperWidth) })
		if a.shred.active() {
			a.renderShred(c, paperWidth)
		} else {
			a.renderPaperScroll(c, paperWidth)
		}
	})
}

func (a *app) renderPaperScroll(c *ui.Context, paperWidth float32) {
	ui.Scroll(c).TrackScroll(&a.scrollState).FillWidth().Grow(1).Clip().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() { a.renderPaper(c, paperWidth) })
	})
}

func (a *app) showReceiptAt(day, at time.Time) {
	if a.clear.running() {
		return
	}
	a.stopShred()
	a.tab = 0
	targetDay := day.In(time.Local).Format("2006-01-02")
	if targetDay == a.todayKey() {
		a.dayFilter = ""
	} else {
		a.dayFilter = targetDay
	}
	a.scrollState.Y = 0
	a.lastLoad = time.Time{}
	a.refresh()
	a.receiptTarget = ""
	for _, line := range a.receipt.Lines {
		if !at.Before(line.Start) && at.Before(line.End) {
			a.receiptTarget = line.ID
			break
		}
	}
}

func (a *app) renderPrinter(c *ui.Context, width float32) {
	msg := locales.In(c.Locale())
	now := a.currentTime()
	selected, err := time.ParseInLocation("2006-01-02", a.dayKey(), time.Local)
	if err != nil {
		selected = now
	}
	todayKey := now.Format("2006-01-02")
	yesterdayKey := now.AddDate(0, 0, -1).Format("2006-01-02")
	dayName := func(d time.Time) string {
		key := d.Format("2006-01-02")
		if key == todayKey {
			return msg.ReceiptToday()
		}
		if key == yesterdayKey {
			return msg.ReceiptYesterday()
		}
		return i18n.FormatDate(c.Locale(), d, i18n.DateOptions{Skeleton: "MMMEd"})
	}
	ui.Column(c).Width(width).Children(func() {
		ui.Row(c).FillWidth().Height(48).Padding(0, 16).Gap(8).AlignItems(ui.Center).Radius(13).Background(ui.Hex("#252529")).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + 6, Y: r.Y + 3, W: r.W - 12, H: 1}, ui.Hex("#3A3A40"), 0)
		}).Children(func() {
			menu := func(m *ui.Menu) {
				if a.clear.running() {
					return
				}
				for i := 0; i < 7; i++ {
					d := now.AddDate(0, 0, -i)
					key := d.Format("2006-01-02")
					if m.Item(dayName(d)).Checked(a.dayKey() == key).Chosen() {
						a.stopShred()
						if key == todayKey {
							a.dayFilter = ""
						} else {
							a.dayFilter = key
						}
						a.scrollState.Y = 0
						a.lastLoad = time.Time{}
						a.refresh()
					}
				}
			}
			ui.ButtonBase(c.Key("receipt-day")).Padding(5, 6).Radius(6).Disabled(a.clear.running()).PopoverMenuButton(menu).Children(func() { ui.Text(c, dayName(selected)+" ⌄").Bold().FontSize(13).TextColor(ui.Hex("#F4F4F5")) })
			ui.Text(c, i18n.FormatDate(c.Locale(), selected, i18n.DateOptions{Skeleton: "MMMEd"})).FontSize(11).TextColor(ui.Hex("#A1A1AA"))
			ui.Spacer(c)
			color, label := ui.Hex("#71717A"), msg.ReceiptOff()
			switch a.status.Phase {
			case capture.PhaseActive:
				color, label = ui.Hex("#22C55E"), msg.ReceiptActive()
			case capture.PhaseWaiting:
				color, label = ui.Hex("#22C55E"), msg.ReceiptWaiting()
			case capture.PhasePaused:
				color, label = ui.Hex("#F59E0B"), msg.ReceiptPaused()
			case capture.PhaseBlocked:
				color, label = ui.Hex("#EF4444"), msg.ReceiptBlocked()
			}
			btn := ui.ButtonBase(c.Key("receipt-recorder")).Gap(6).Padding(5, 6).AlignItems(ui.Center).Label(label).Disabled(a.clear.running())
			btn.Children(func() {
				ui.Box(c).Size(7, 7).Radius(4).Background(color)
				ui.Text(c, label).FontSize(11).TextColor(ui.Hex("#D4D4D8"))
			})
			if btn.Clicked() && a.runner != nil {
				if a.status.Phase == capture.PhasePaused {
					a.pauseRecorder(time.Time{})
				} else {
					a.setRecorderEnabled(a.status.Phase == capture.PhaseOff)
				}
			}
		})
		ui.Box(c).FillWidth().Height(8).Margin(-4, 0, 0, 0).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + 2, Y: r.Y, W: r.W - 4, H: r.H}, ui.Hex("#252529"), 4)
			p.Fill(ui.Rect{X: r.X + 14, Y: r.Y + 2, W: r.W - 28, H: 4}, ui.Hex("#09090B"), 2)
		})
	})
}

const printDuration = 600 * time.Millisecond

func (a *app) printRevealBounds(c *ui.Context) (float32, bool) {
	pr := &a.printReveal
	if pr.revision != a.receiptRevision {
		if c.Preferences().ReduceMotion || a.paperFeed.day != a.dayKey() {
			pr.syncTo(a.receiptRevision)
			return 0, false
		}
		pr.revision = a.receiptRevision
		pr.phase = animMeasuring
		pr.start = c.Now()
		pr.height = 0
		c.AnimationFrame()
		return 0, true
	}
	if c.Preferences().ReduceMotion {
		pr.phase = animIdle
		pr.start = time.Time{}
		return 0, false
	}
	switch pr.phase {
	case animIdle:
		return 0, false
	case animMeasuring:
		if !c.Now().After(pr.start) {
			c.AnimationFrame()
			return 0, true
		}
		h := pr.handle.Bounds(c).H
		if h <= 0 {
			if c.Now().Sub(pr.start) >= printDuration {
				pr.phase = animIdle
				pr.start = time.Time{}
				return 0, false
			}
			c.AnimationFrame()
			return 0, true
		}
		pr.height = h
		pr.phase = animRunning
		pr.start = c.Now()
		c.AnimationFrame()
		return 0, true
	case animRunning:
		progress := float64(c.Now().Sub(pr.start)) / float64(printDuration)
		if progress >= 1 {
			pr.phase = animIdle
			pr.start = time.Time{}
			return 0, false
		}
		c.AnimationFrame()
		return pr.height * float32(1-math.Pow(1-progress, 3)), true
	default:
		pr.phase = animIdle
		return 0, false
	}
}

func (a *app) renderPaper(c *ui.Context, width float32) {
	day := a.dayKey()
	pf := &a.paperFeed
	if pf.day != day {
		pf.day = day
		a.printReveal.syncTo(a.receiptRevision)
		if c.Preferences().ReduceMotion {
			pf.phase = animIdle
			pf.start = time.Time{}
		} else {
			pf.phase = animMeasuring
			pf.start = c.Now()
		}
	}
	if c.Preferences().ReduceMotion {
		pf.phase = animIdle
		pf.start = time.Time{}
	}
	offset := float32(0)
	switch pf.phase {
	case animMeasuring:
		if c.Now().After(pf.start) {
			if h := pf.handle.Bounds(c).H; h > 0 {
				pf.distance = h
				pf.start = c.Now()
				pf.phase = animRunning
				offset = -pf.distance
				c.AnimationFrame()
			} else if c.Now().Sub(pf.start) >= 900*time.Millisecond {
				pf.phase = animIdle
			} else {
				offset = -10000
				c.AnimationFrame()
			}
		} else {
			offset = -10000
			c.AnimationFrame()
		}
	case animRunning:
		elapsed := c.Now().Sub(pf.start)
		if elapsed >= 900*time.Millisecond {
			pf.phase = animIdle
		} else {
			progress := max(float64(0), float64(elapsed)/float64(900*time.Millisecond))
			offset = -pf.distance * float32(math.Pow(1-progress, 3))
			c.AnimationFrame()
		}
	}
	paper := ui.Hex("#FFFDF7")
	if c.Theme().Dark {
		paper = ui.Hex("#ECE8DF")
	}
	ui.Column(c).Width(width).Bind(&pf.handle).Translate(0, offset).Children(func() {
		ui.Column(c).FillWidth().Background(paper).Padding(20, 22, 16, 22).Gap(9).Children(func() { a.renderReceiptContents(c) })
		receiptZigzag(c, paper)
	})
}
