/*
 * [INPUT]: 依赖时间轴数据与界面组件。
 * [OUTPUT]: 提供历史列表与条目视图。
 * [POS]: 时间轴数据的界面呈现。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"konseki/internal/capture"
	"strings"
	"time"
)

func (a *app) renderHistorySection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "History").
			FontSize(13).
			Bold().
			TextColor(pal.textSecondary).
			Margin(4, 0, 0, 10)

		card := ui.Column(c).
			FillWidth().
			Radius(14).
			Background(pal.cardBg).
			Border(1, pal.cardBorder).
			Shadow(0, 4, 18, -4, ui.RGBA(15, 23, 42, 0.04)).
			Clip()

		card.Children(func() {
			// ---- 卡片顶栏：Today ⌃⌄ + ⓘ + ••• ----
			ui.Row(c).
				FillWidth().
				Height(48).
				Padding(0, 16).
				AlignItems(ui.Center).
				Material(glass.Glass{Style: glass.Clear, Clarity: 0.65}).
				Children(func() {
					dayMenu := func(m *ui.Menu) {
						for _, d := range []string{"Today", "Yesterday", "Last 7 Days"} {
							if m.Item(d).Checked(a.dayFilter == d).Chosen() {
								a.dayFilter = d
							}
						}
						m.Separator()
						if m.Item("展示参考时间轴 (Showcase)").Checked(a.includeShowcase).Chosen() {
							a.includeShowcase = !a.includeShowcase
						}
						if m.Item("展开全部时间段").Chosen() {
							for _, s := range a.activeSlots() {
								a.expanded[s.ID] = true
							}
						}
						if m.Item("收起全部时间段").Chosen() {
							a.expanded = map[string]bool{}
						}
					}

					todayBtn := ui.ButtonBase(c.Key("btn-today-selector")).
						Gap(5).
						Padding(5, 8).
						Radius(8).
						Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: 0.6}).
						PopoverMenuButton(dayMenu)
					if todayBtn.Hovered() {
						todayBtn.Background(pal.rowHoverBg)
					}
					todayBtn.Children(func() {
						ui.Text(c, a.dayFilter).FontSize(14).Bold().TextColor(pal.textPrimary)
						ui.Icon(c, chevronsUpDownSVG).FontSize(13).TextColor(pal.textSecondary)
					})

					ui.Spacer(c)

					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						infoBtn := ui.ButtonBase(c.Key("btn-info-diag")).
							Size(28, 28).
							Radius(8).
							Center().
							Label("诊断").
							Tooltip("权限与采集诊断").
							Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: 0.6})
						if infoBtn.Hovered() || a.showDiag {
							infoBtn.Background(pal.rowHoverBg)
						}
						infoBtn.Children(func() {
							ui.Icon(c, infoCircleSVG).FontSize(15).TextColor(pal.textSecondary)
						})
						if infoBtn.Clicked() {
							a.doctor = capture.DoctorReport()
							a.showDiag = !a.showDiag
						}

						moreMenu := func(m *ui.Menu) {
							if m.Item("展开/收起 10:00 AM 详情").Chosen() {
								a.expanded["slot-1000"] = !a.expanded["slot-1000"]
							}
							if m.Item("展开全部").Chosen() {
								for _, s := range a.activeSlots() {
									a.expanded[s.ID] = true
								}
							}
							if m.Item("收起全部").Chosen() {
								a.expanded = map[string]bool{}
							}
							m.Separator()
							if m.Item("显示示例时间轴数据").Checked(a.includeShowcase).Chosen() {
								a.includeShowcase = !a.includeShowcase
							}
							if m.Item("权限与环境诊断").Checked(a.showDiag).Chosen() {
								a.doctor = capture.DoctorReport()
								a.showDiag = !a.showDiag
							}
						}

						moreBtn := ui.ButtonBase(c.Key("btn-more-menu")).
							Size(28, 28).
							Radius(8).
							Center().
							Label("更多选项").
							PopoverMenuButton(moreMenu).
							Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: 0.6})
						if moreBtn.Hovered() {
							moreBtn.Background(pal.rowHoverBg)
						}
						moreBtn.Children(func() {
							ui.Icon(c, moreHorizontalSVG).FontSize(15).TextColor(pal.textSecondary)
						})
					})
				})

			if a.showDiag && a.doctor != "" {
				ui.Column(c).
					FillWidth().
					Padding(12, 18).
					Gap(6).
					Background(pal.rowExpandedBg).
					BorderWidth(1, 0, 1, 0).
					BorderColor(pal.rowDivider).
					Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
							ui.Text(c, "采集与权限诊断").FontSize(12).Bold().TextColor(pal.textPrimary)
							ui.Spacer(c)
							closeBtn := ui.ButtonBase(c.Key("btn-close-diag")).Padding(2, 6).Radius(6)
							closeBtn.Children(func() {
								ui.Text(c, "关闭").FontSize(11).TextColor(pal.textSecondary)
							})
							if closeBtn.Clicked() {
								a.showDiag = false
							}
						})
						ui.Text(c, a.doctor).FontSize(11.5).Font("monospace").TextColor(pal.textSecondary).LineHeight(1.45)
					})
			}

			slots := a.activeSlots()
			if len(slots) == 0 {
				ui.Column(c).FillWidth().Center().Padding(42, 24, 42, 24).Gap(10).Children(func() {
					ui.Text(c, "今天还没有记录").FontSize(14.5).Bold().TextColor(pal.textPrimary)
					ui.Text(c, "点「开始记录」，它会在本机留痕，不上传任何数据").FontSize(12.2).TextColor(pal.textSecondary)

					previewBtn := ui.ButtonBase(c.Key("btn-load-demo")).
						Margin(6, 0, 0, 0).
						Padding(6, 14).
						Radius(999).
						Material(glass.Glass{Style: glass.Regular, Interactive: true, Clarity: 0.25}).
						Border(1, pal.cardBorder)
					previewBtn.Children(func() {
						ui.Text(c, "加载示例时间轴 (Demo)").FontSize(12).Bold().TextColor(pal.textPrimary)
					})
					if previewBtn.Clicked() {
						a.includeShowcase = true
					}
				})
			} else {
				ui.Column(c).FillWidth().Padding(4, 16, 20, 10).Gap(0).Children(func() {
					for _, slot := range slots {
						a.renderTimelineSlot(c, slot, pal)
					}
				})
			}
		})
	})
}

func (a *app) renderTimelineSlot(c *ui.Context, slot TimelineSlot, pal palette) {
	isExpanded := a.expanded[slot.ID]

	ui.Row(c.Key(slot.ID)).AlignItems(ui.Stretch).Gap(4).FillWidth().Children(func() {
		// ---- 左列：时间标签 + 连续刻度尺 ----
		ruler := ui.Box(c).
			Width(78).
			Shrink(0).
			Padding(11, 18, 0, 4).
			AlignItems(ui.End)

		ruler.Draw(func(p *ui.Painter, r ui.Rect) {
			tickRight := r.X + r.W - 2
			majorY := r.Y + 18.0

			p.Fill(ui.Rect{
				X: tickRight - 9.5,
				Y: majorY - 0.85,
				W: 9.5,
				H: 1.7,
			}, pal.tickMajor, 0.8)

			for y := majorY + 8.0; y < r.Y+r.H-2.0; y += 7.5 {
				p.Fill(ui.Rect{
					X: tickRight - 7.5,
					Y: y,
					W: 5.5,
					H: 1.0,
				}, pal.tickMinor, 0.5)
			}
		})

		ruler.Children(func() {
			ui.Text(c, slot.TimeLabel).
				FontSize(11.5).
				FontFeatures("tnum").
				TextColor(pal.textTime).
				SingleLine()
		})

		// ---- 右列：可点击展开的活动卡片 ----
		entryCard := ui.Column(c).
			Grow(1).
			Padding(9, 12, 13, 12).
			Radius(10).
			Gap(6).
			Cursor(ui.CursorPointer)

		if entryCard.Hovered() {
			entryCard.Background(pal.rowHoverBg)
		} else if isExpanded {
			entryCard.Background(pal.rowExpandedBg)
		}
		entryCard.Transition(ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond})

		if entryCard.Clicked() {
			a.expanded[slot.ID] = !isExpanded
			isExpanded = a.expanded[slot.ID]
		}

		entryCard.Children(func() {
			ui.Row(c).AlignItems(ui.Start).Gap(10).FillWidth().Children(func() {
				ui.Text(c, slot.Title).
					FontSize(13.5).
					Bold().
					TextColor(pal.textPrimary).
					LineHeight(1.36).
					Grow(1)

				chev := chevronDownSVG
				if isExpanded {
					chev = chevronUpSVG
				}
				ui.Box(c).Padding(3, 0, 0, 4).Children(func() {
					ui.Icon(c, chev).FontSize(12.5).TextColor(pal.textSecondary)
				})
			})

			ui.Text(c, slot.Summary).
				FontSize(12.5).
				TextColor(pal.textSecondary).
				LineHeight(1.48)

			if len(slot.Apps) > 0 {
				ui.Row(c).Gap(5).Padding(2, 0, 0, 0).AlignItems(ui.Center).Wrap().Children(func() {
					for _, appName := range slot.Apps {
						renderAppIcon(c, appName, 18)
					}
				})
			}

			if isExpanded {
				a.renderExpandedDetails(c, slot, pal)
			}
		})
	})
}

func (a *app) renderExpandedDetails(c *ui.Context, slot TimelineSlot, pal palette) {
	ui.Column(c).Padding(8, 0, 2, 0).Gap(14).FillWidth().Children(func() {
		if len(slot.Memory) > 0 {
			ui.Column(c).Gap(7).FillWidth().Children(func() {
				ui.Text(c, "Memory summary").
					FontSize(12).
					Bold().
					TextColor(pal.textSection)

				for _, grp := range slot.Memory {
					ui.Column(c).Gap(5).FillWidth().Children(func() {
						ui.Row(c).AlignItems(ui.Start).Gap(7).FillWidth().Children(func() {
							ui.Text(c, "•").FontSize(12).Bold().TextColor(pal.bulletColor)
							ui.Text(c, grp.Category).
								FontSize(12.2).
								Bold().
								TextColor(pal.textSection).
								LineHeight(1.45).
								Grow(1)
						})

						ui.Column(c).Padding(0, 0, 0, 14).Gap(5).FillWidth().Children(func() {
							for _, item := range grp.Items {
								ui.Row(c).AlignItems(ui.Start).Gap(7).FillWidth().Children(func() {
									ui.Text(c, "•").FontSize(12).TextColor(pal.bulletColor)
									ui.Box(c).Grow(1).Children(func() {
										renderRichTextWithCodePills(c, item, 12.2, pal.textSecondary, pal.codeFg, pal.codeBg)
									})
								})
							}
						})
					})
				}
			})
		}

		if slot.PriorContext != "" {
			ui.Column(c).Gap(5).FillWidth().Children(func() {
				ui.Text(c, "Relevant prior context").
					FontSize(12).
					Bold().
					TextColor(pal.textSection)
				renderRichTextWithCodePills(c, slot.PriorContext, 12.2, pal.textSecondary, pal.codeFg, pal.codeBg)
			})
		}

		if len(slot.UserContext) > 0 {
			ui.Column(c).Gap(5).FillWidth().Children(func() {
				ui.Text(c, "Important non-obvious context about the user").
					FontSize(12).
					Bold().
					TextColor(pal.textSection)

				for _, tok := range slot.UserContext {
					clean := strings.Trim(tok, "`")
					ui.Row(c).AlignItems(ui.Center).Gap(7).FillWidth().Children(func() {
						ui.Text(c, "•").FontSize(12).TextColor(pal.bulletColor)
						renderRichTextWithCodePills(c, "`"+clean+"`", 12.0, pal.textSecondary, pal.codeFg, pal.codeBg)
					})
				}
			})
		}

		if len(slot.Recordings) > 0 {
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				ui.Text(c, "Recording summary").
					FontSize(12).
					Bold().
					TextColor(pal.textSection)

				for _, rec := range slot.Recordings {
					ui.Column(c).Gap(3).FillWidth().Children(func() {
						header := fmt.Sprintf("%s · %s", rec.TimeRange, rec.AppsLabel)
						ui.Text(c, header).
							FontSize(12.2).
							Bold().
							TextColor(pal.textSection).
							FontFeatures("tnum")
						renderRichTextWithCodePills(c, rec.Detail, 12.2, pal.textSecondary, pal.codeFg, pal.codeBg)
					})
				}
			})
		}
	})
}

func fmtDur(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return itoa(h) + "h" + itoa(m) + "m"
	}
	return itoa(m) + "m"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
