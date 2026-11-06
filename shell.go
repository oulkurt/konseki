/*
 * [INPUT]: 依赖应用状态、设置页与 MyGo 界面组件。
 * [OUTPUT]: 提供窗口主体、分页与设置页滚动区。
 * [POS]: 连接设置页和小票页。小票页独立固定打印机。
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

func (a *app) view(c *ui.Context) {
	a.refresh()
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
		// 主滚动容器：包含 Capture、Summaries 与 History 三大模块
		ui.Scroll(c).
			TrackScroll(&a.scrollState).
			Fill().
			Padding(topInset, 22, 28, 22).
			ScrollbarInsets(topInset, 0, 0).
			Children(func() {
				ui.Column(c).FillWidth().Gap(16).Children(func() {
					// ---- 顶部轻量品牌状态栏（支持拖拽窗口与实时录制控制） ----
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

						// 快捷录制状态胶囊
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
									toggleLabel := msg.ShellStart()
									if a.status.Phase != capture.PhaseOff {
										toggleLabel = msg.ShellPause()
									}
									btn := ui.ButtonBase(c.Key("btn-record-toggle")).
										Padding(2, 7).
										Radius(999).
										Background(pal.rowHoverBg)
									btn.Children(func() {
										ui.Text(c, toggleLabel).FontSize(11).Bold().TextColor(pal.textPrimary)
									})
									if btn.Clicked() {
										a.setRecorderEnabled(a.status.Phase == capture.PhaseOff)
									}
								}
							})
					})

					if a.err != "" {
						ui.Text(c, msg.ShellReadError()+a.err).FontSize(12).TextColor(t.Danger)
					}

					// ---- 分页：时间轴 / 设置 ----
					//
					// 为什么从「一页长滚动」改成两页：加了供应商和 skill 之后
					// 设置区比内容还长，进来第一屏看到一堆开关是错的。
					// 时间轴是每天要看的东西，设置是配一次就不动的。
					a.renderTabs(c, pal)

					switch a.tab {
					case 1:
						a.renderSettingsTabs(c, pal)
						switch a.settingsTab {
						case 0:
							a.renderCaptureSection(c, pal)
							a.renderDanger(c, pal)
						case 1:
							a.startCredentialScan()
							a.renderSummariesSection(c, pal)
							a.renderProvidersSection(c, pal)
						case 2:
							a.renderLanguageSection(c, pal)
							a.renderSkillSection(c, pal)
						}
					}
				})
			})

		// 窗口顶部边缘 Liquid Glass 渐隐过渡 + 拖拽区
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
