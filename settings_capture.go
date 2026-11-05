/*
 * [INPUT]: 依赖采集设置与界面组件。
 * [OUTPUT]: 提供采集设置界面。
 * [POS]: 设置页的采集区块。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"konseki/internal/config"
	"slices"
	"strings"
)

func (a *app) renderCaptureSection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "Capture").
			FontSize(13).
			Bold().
			TextColor(pal.textSecondary).
			Margin(0, 0, 0, 10)

		// Card 1A: Enable Context Awareness + Pause capturing
		a.settingsCard(c, pal, func() {
			// Row 1: Enable Context Awareness
			a.settingsRow(c,
				"Enable Context Awareness",
				"Capture your activity on this device.",
				pal,
				func() {
					enabled := a.cfgNow.Recorder.Enabled
					if renderPillSwitch(c, "sw-enable-context", &enabled, pal) && a.runner != nil {
						a.setRecorderEnabled(enabled)
					}
				},
			)

			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)

			// Row 2: Pause capturing（暂停时间可选）
			pauseSub := "Stop capturing for a while without turning it off."
			if a.pauseDuration != "Select" {
				pauseSub = fmt.Sprintf("Stop capturing for a while without turning it off. (Paused: %s)", a.pauseDuration)
			}
			a.settingsRow(c,
				"Pause capturing",
				pauseSub,
				pal,
				func() {
					pauseMenu := func(m *ui.Menu) {
						durations := []string{
							"15 minutes",
							"30 minutes",
							"1 hour",
							"2 hours",
							"4 hours",
							"Until tomorrow",
							"Indefinitely",
						}
						for _, d := range durations {
							if m.Item(d).Checked(a.pauseDuration == d).Chosen() {
								a.pauseDuration = d
							}
						}
						m.Separator()
						if m.Item("Resume capturing").Checked(a.pauseDuration == "Select").Chosen() {
							a.pauseDuration = "Select"
						}
					}
					labelColor := pal.textSecondary
					if a.pauseDuration != "Select" {
						labelColor = pal.textPrimary
					}
					renderSelectPill(c, "btn-pause-capturing", a.pauseDuration, 94, labelColor, pal, pauseMenu)
				},
			)
		})

		// Card 1B: Capture typed text + Allow Screen Recording + Denylist
		a.settingsCard(c, pal, func() {
			// Row 1: Retention
			// 采集到的原始截屏是最占空间也最敏感的东西，
			// 让用户能自己设一个上限，比事后清理体验好得多。
			retentionSub := "原始截图和屏幕文字保留的天数。摘要不会过期。"
			retentionLabel := fmt.Sprintf("%d days", a.cfgNow.Recorder.RetentionDays)
			if a.cfgNow.Recorder.RetentionDays <= 0 {
				retentionLabel = "Forever"
			}
			a.settingsRow(c, "Retention", retentionSub, pal, func() {
				menu := func(m *ui.Menu) {
					for _, days := range []int{7, 14, 30, 90, 0} {
						label := fmt.Sprintf("%d days", days)
						if days == 0 {
							label = "Forever"
						}
						if m.Item(label).Checked(a.cfgNow.Recorder.RetentionDays == days).Chosen() {
							a.cfg.Update(func(c *config.Config) { c.Recorder.RetentionDays = days })
							a.applyRecorder()
						}
					}
				}
				renderSelectPill(c, "btn-retention", retentionLabel, 100, pal.textPrimary, pal, menu)
			})

			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)

			// Row 3: Denylist（可选 App 和 Website）
			ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Gap(3).Children(func() {
						ui.Text(c, "Denylist").
							FontSize(13.5).
							Bold().
							TextColor(pal.textPrimary)
						ui.Text(c, "Aside never captures what you do in these apps and websites.").
							FontSize(12.2).
							TextColor(pal.textSecondary)
					})

					addBtn := ui.ButtonBase(c.Key("btn-denylist-add")).
						Height(32).
						Padding(0, 13).
						Gap(6).
						Radius(8).
						Background(pal.cardBg).
						Border(1, pal.controlBorder).
						Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: 0.7})
					if addBtn.Hovered() || a.showDenylistPopover {
						addBtn.Background(pal.rowHoverBg)
					}
					addBtn.Children(func() {
						ui.Icon(c, plusSVG).FontSize(12.5).TextColor(pal.textPrimary)
						ui.Text(c, "Add").FontSize(12.5).Bold().TextColor(pal.textPrimary)
					})
					if addBtn.Clicked() {
						a.showDenylistPopover = !a.showDenylistPopover
					}

					// 点击 + Add 弹出双栏选择面板：可选 Apps 或添加 Websites
					ui.PopoverBase(c, addBtn, &a.showDenylistPopover, func(panel ui.Element) {
						panel.Margin(6, 0, 0, 0).
							Width(310).
							Padding(12).
							Gap(10).
							Radius(12).
							Background(pal.cardBg).
							Border(1, pal.cardBorder).
							Shadow(0, 12, 32, -4, ui.RGBA(0, 0, 0, 0.16))

						// 顶部切换：Apps | Websites
						seg := ui.SegmentedBase(c.Key("denylist-seg"), &a.denylistTab, 2)
						seg.Track.FillWidth().Padding(3).Radius(8).Background(pal.rowHoverBg).Children(func() {
							for i, tabName := range []string{"Apps", "Websites"} {
								s := seg.Segment(i).Grow(1).Padding(5, 10).Radius(6).Center()
								if i == a.denylistTab {
									s.Background(pal.cardBg).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.1))
								}
								s.Children(func() {
									ui.Text(c, tabName).FontSize(12).Bold().TextColor(pal.textPrimary)
								})
							}
						})

						if a.denylistTab == 0 {
							// ---- Tab 0: 选择要排除的 App ----
							ui.Text(c, "Exclude applications from capturing:").
								FontSize(11.5).
								TextColor(pal.textSecondary)

							candidateApps := []string{
								"1Password",
								"Keychain Access",
								"Messages",
								"WeChat",
								"Telegram",
								"Dia",
								"T3 Code",
								"Alma",
								"Finder",
							}
							ui.Column(c).FillWidth().Gap(2).Children(func() {
								for _, appName := range candidateApps {
									inList := slices.Contains(a.denylistApps, appName)
									rowBtn := ui.ButtonBase(c.Key("deny-app-"+appName)).
										FillWidth().
										Padding(6, 8).
										Radius(7).
										Gap(8).
										AlignItems(ui.Center)
									if rowBtn.Hovered() {
										rowBtn.Background(pal.rowHoverBg)
									}
									rowBtn.Children(func() {
										renderAppIcon(c, appName, 16)
										ui.Text(c, appName).FontSize(12.2).TextColor(pal.textPrimary).Grow(1)
										if inList {
											ui.Icon(c, checkSVG).FontSize(12.5).TextColor(pal.switchOn)
										}
									})
									if rowBtn.Clicked() {
										if inList {
											a.denylistApps = slices.DeleteFunc(a.denylistApps, func(s string) bool { return s == appName })
										} else {
											a.denylistApps = append(a.denylistApps, appName)
										}
									}
								}
							})

							// 自定义输入 App 名称
							ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
								ui.TextInput(c.Key("custom-app-input"), &a.customAppInput).
									Placeholder("Custom app name...").
									Grow(1)
								submitApp := ui.ButtonBase(c.Key("btn-submit-app")).
									Padding(6, 10).
									Radius(7).
									Background(pal.rowHoverBg).
									Border(1, pal.controlBorder)
								submitApp.Children(func() {
									ui.Text(c, "Add App").FontSize(11.5).Bold().TextColor(pal.textPrimary)
								})
								if submitApp.Clicked() {
									trimmed := strings.TrimSpace(a.customAppInput)
									if trimmed != "" && !slices.Contains(a.denylistApps, trimmed) {
										a.denylistApps = append(a.denylistApps, trimmed)
										a.customAppInput = ""
									}
								}
							})
						} else {
							// ---- Tab 1: 添加或选择要排除的 Website ----
							ui.Text(c, "Exclude websites or domains:").
								FontSize(11.5).
								TextColor(pal.textSecondary)

							ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
								ui.TextInput(c.Key("custom-site-input"), &a.customSiteInput).
									Placeholder("e.g. bank.com, mail.google.com").
									Grow(1)
								submitSite := ui.ButtonBase(c.Key("btn-submit-site")).
									Padding(6, 10).
									Radius(7).
									Background(pal.rowHoverBg).
									Border(1, pal.controlBorder)
								submitSite.Children(func() {
									ui.Text(c, "Add Site").FontSize(11.5).Bold().TextColor(pal.textPrimary)
								})
								if submitSite.Clicked() {
									trimmed := strings.TrimSpace(a.customSiteInput)
									if trimmed != "" && !slices.Contains(a.denylistSites, trimmed) {
										a.denylistSites = append(a.denylistSites, trimmed)
										a.customSiteInput = ""
									}
								}
							})

							candidateSites := []string{
								"1password.com",
								"mail.google.com",
								"chase.com",
								"github.com/settings/tokens",
								"x.com/messages",
							}
							ui.Column(c).FillWidth().Gap(2).Children(func() {
								for _, site := range candidateSites {
									inList := slices.Contains(a.denylistSites, site)
									rowBtn := ui.ButtonBase(c.Key("deny-site-"+site)).
										FillWidth().
										Padding(6, 8).
										Radius(7).
										Gap(8).
										AlignItems(ui.Center)
									if rowBtn.Hovered() {
										rowBtn.Background(pal.rowHoverBg)
									}
									rowBtn.Children(func() {
										ui.Icon(c, globeSVG).FontSize(13.5).TextColor(pal.textSecondary)
										ui.Text(c, site).FontSize(12.2).TextColor(pal.textPrimary).Grow(1)
										if inList {
											ui.Icon(c, checkSVG).FontSize(12.5).TextColor(pal.switchOn)
										}
									})
									if rowBtn.Clicked() {
										if inList {
											a.denylistSites = slices.DeleteFunc(a.denylistSites, func(s string) bool { return s == site })
										} else {
											a.denylistSites = append(a.denylistSites, site)
										}
									}
								}
							})
						}
					})
				})

				// 已添加的 Denylist Apps 与 Websites 标签展示（支持点击移除）
				if len(a.denylistApps) > 0 || len(a.denylistSites) > 0 {
					ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
						var removeApp string
						for _, appName := range a.denylistApps {
							chip := ui.ButtonBase(c.Key("chip-app-"+appName)).
								Gap(5).
								Padding(4, 8).
								Radius(6).
								Background(pal.codeBg).
								AlignItems(ui.Center)
							chip.Children(func() {
								renderAppIcon(c, appName, 14)
								ui.Text(c, appName).FontSize(11.5).TextColor(pal.codeFg)
								ui.Icon(c, closeSmallSVG).FontSize(10).TextColor(pal.textSecondary)
							})
							if chip.Clicked() {
								removeApp = appName
							}
						}
						if removeApp != "" {
							a.denylistApps = slices.DeleteFunc(a.denylistApps, func(s string) bool { return s == removeApp })
						}

						var removeSite string
						for _, site := range a.denylistSites {
							chip := ui.ButtonBase(c.Key("chip-site-"+site)).
								Gap(5).
								Padding(4, 8).
								Radius(6).
								Background(pal.codeBg).
								AlignItems(ui.Center)
							chip.Children(func() {
								ui.Icon(c, globeSVG).FontSize(12).TextColor(pal.textSecondary)
								ui.Text(c, site).FontSize(11.5).Font("monospace").TextColor(pal.codeFg)
								ui.Icon(c, closeSmallSVG).FontSize(10).TextColor(pal.textSecondary)
							})
							if chip.Clicked() {
								removeSite = site
							}
						}
						if removeSite != "" {
							a.denylistSites = slices.DeleteFunc(a.denylistSites, func(s string) bool { return s == removeSite })
						}
					})
				}
			})
		})
	})
}

// renderHistorySection 绘制 History 时间轴卡片
