/*
 * [INPUT]: 依赖应用状态、设置页与 MyGo 界面组件。
 * [OUTPUT]: 提供窗口主体、分页、设置页滚动区和顶部标签。
 * [POS]: 连接小票页与设置页。小票页独立固定打印机，顶部标签由两页共用。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"time"

	"konseki/internal/capture"
	"konseki/locales"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func (a *app) syncNavState() {
	if a.tab != 0 && a.tab != 1 {
		a.tab = 0
	}
	if a.settingsTab < 0 || a.settingsTab > 2 {
		a.settingsTab = 0
	}
	if !a.navInit {
		a.navInit = true
		a.lastTab = a.tab
		a.lastSettingsTab = a.settingsTab
		return
	}
	if a.tab != a.lastTab || a.settingsTab != a.lastSettingsTab {
		a.scrollState.Y = 0
		a.drafts.clearFocus()
		if a.tab == 1 && a.settingsTab == 2 && a.legacy.phase == legacyFailed {
			a.resetLegacy()
		}
		a.lastTab = a.tab
		a.lastSettingsTab = a.settingsTab
	}
}

func (a *app) view(c *ui.Context) {
	a.refresh()
	a.syncNavState()
	c.After(2 * time.Second)
	pal := themePalette(c)
	msg := locales.In(c.Locale())
	t := c.Theme()

	if c.Vibrancy() {
		c.Root().Background(pal.canvasBg.Alpha(0.76))
	} else {
		c.Root().Background(pal.canvasBg)
	}

	bar := c.TitleBar()
	topInset := max(float32(16), bar.Height+6)
	if a.tab == 0 {
		a.renderTimelinePage(c, pal, topInset)
		return
	}

	ui.Box(c).Fill().Children(func() {
		ui.Scroll(c).
			TrackScroll(&a.scrollState).
			Fill().
			Padding(topInset, 22, 28, 22).
			ScrollbarInsets(topInset, 0, 0).
			Children(func() {
				ui.Column(c).FillWidth().Gap(16).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(0, 6).DragWindow().Children(func() {
						ui.Text(c, "痕跡 Konseki").
							FontSize(11.5).
							Bold().
							TextColor(pal.textSecondary.Alpha(0.7))

						if a.status.Front.App != "" {
							title := a.status.Front.Title
							if title == "" {
								title = msg.ShellUntitled()
							}
							ui.Text(c, msg.ShellNow(a.status.Front.App, title)).
								FontSize(11).
								TextColor(pal.textSecondary).
								SingleLine().
								Margin(0, 0, 0, 8)
						}

						ui.Spacer(c)

						ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(2, 8).Radius(999).
							Material(glass.Glass{Style: glass.Regular, Clarity: 0.4}).
							Border(1, pal.cardBorder).
							Children(func() {
								switch a.status.Phase {
								case capture.PhaseBlocked:
									ui.Text(c, msg.ShellAsleep()).FontSize(11).TextColor(pal.textSecondary)
								case capture.PhasePaused:
									ui.Text(c, msg.ShellPaused()).FontSize(11).TextColor(pal.textSecondary)
								case capture.PhaseActive, capture.PhaseWaiting:
									ui.Text(c, msg.ShellRecording()).FontSize(11).TextColor(pal.switchOn)
								default:
									ui.Text(c, msg.ShellOff()).FontSize(11).TextColor(pal.textSecondary)
								}

								if a.runner != nil {
									toggleLabel := msg.ShellPause()
									switch a.status.Phase {
									case capture.PhaseOff:
										toggleLabel = msg.ShellStart()
									case capture.PhasePaused:
										toggleLabel = msg.RecorderResume()
									}
									btn := ui.ButtonBase(c.Key("btn-record-toggle")).
										Padding(2, 7).
										Radius(999).
										Background(pal.rowHoverBg).
										Disabled(a.clear.running())
									btn.Children(func() {
										ui.Text(c, toggleLabel).FontSize(11).Bold().TextColor(pal.textPrimary)
									})
									if btn.Clicked() {
										if a.status.Phase == capture.PhasePaused {
											a.pauseRecorder(time.Time{})
										} else {
											a.setRecorderEnabled(a.status.Phase == capture.PhaseOff)
										}
									}
								}
							})
					})

					if err := a.bannerError(); err != nil {
						ui.Text(c, msg.ShellReadError()+errorText(c.Locale(), err)).FontSize(12).TextColor(t.Danger)
					}

					a.renderTabs(c, pal)

					switch a.tab {
					case 1:
						a.renderSettingsTabs(c, pal)
						switch a.settingsTab {
						case 0:
							a.renderCaptureSection(c, pal)
							a.renderRecorderPermissions(c, pal)
							a.renderRecorderFields(c, pal)
							a.renderRecorderPrivacy(c, pal)
							a.renderRecorderStorage(c, pal)
							a.renderRecentSessions(c, pal)
							a.renderDanger(c, pal)
							footer := msg.RecorderFooterStopped()
							if a.cfgNow.Recorder.Enabled {
								footer = msg.RecorderFooterRunning()
							}
							ui.Text(c, footer).FontSize(11).TextColor(pal.textSecondary)
						case 1:
							a.startCredentialScan()
							a.renderModelsSettings(c, pal)
						case 2:
							a.renderLanguageSection(c, pal)
							a.renderAgentSection(c, pal)
						}
					}
				})
			})

		if bar.Height > 0 {
			ui.Box(c).
				Absolute().Top(0).Left(0).Right(0).Height(bar.Height + 10).
				PassThrough().
				Material(glass.ScrollEdge{Hard: false, Background: pal.canvasBg})
			ui.Box(c).
				Absolute().Top(0).Left(0).Right(0).Height(bar.Height).
				DragWindow()
		}
	})
}

func (a *app) renderSettingsTabs(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	names := []string{msg.RecorderTab(), msg.ModelsTab(), msg.GeneralTab()}
	seg := ui.SegmentedBase(c.Key("settings-tabs"), &a.settingsTab, len(names))
	seg.Track.Padding(2).Radius(9).Background(pal.rowHoverBg).Children(func() {
		for i, name := range names {
			sg := seg.Segment(i).Padding(6, 16).Radius(7)
			if i == a.settingsTab {
				sg.Background(pal.cardBg)
			}
			sg.Children(func() { ui.Text(c, name).FontSize(12).Bold().TextColor(pal.textPrimary) })
		}
	})
}

func (a *app) renderTabs(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	names := []string{msg.TabsTimeline(), msg.TabsSettings()}

	needSetup := a.gen == nil
	needSummary := !needSetup && len(a.blocks) > 0 && len(a.sumEntries) == 0
	dirty := needSetup || needSummary

	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		seg := ui.SegmentedBase(c.Key("main-tabs"), &a.tab, len(names))
		seg.Track.Padding(2).Radius(9).Background(pal.rowHoverBg).Children(func() {
			for i, name := range names {
				sg := seg.Segment(i).Padding(4, 14).Radius(7)
				if i == a.tab {
					sg.Background(pal.cardBg)
				}
				sg.Children(func() {
					label := name
					if i == 1 && dirty {
						label = name + " •"
					}
					ui.Text(c, label).FontSize(12).Bold().TextColor(pal.textPrimary)
				})
			}
		})

		ui.Spacer(c)

		switch {
		case a.summary.running():
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(11, 11)
				ui.Text(c, msg.TabsGenerating()).FontSize(11).TextColor(pal.textSecondary)
			})
		case needSetup:
			ui.Text(c, msg.TabsSetup()).FontSize(11).TextColor(pal.textSecondary)
		}
	})
}
