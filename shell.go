/*
 * [INPUT]: 依赖应用状态与 MyGo 界面组件。
 * [OUTPUT]: 提供窗口主体与导航。
 * [POS]: 连接设置页和时间轴。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"konseki/internal/capture"
)

func (a *app) activeSlots() []TimelineSlot {
	live := buildSlotsFromBlocks(a.blocks)
	if a.includeShowcase {
		demo := showcaseSlots()
		if len(live) == 0 {
			return demo
		}
		return append(live, demo...)
	}
	return live
}

func (a *app) view(c *ui.Context) {
	a.refresh()
	pal := themePalette(c)
	t := c.Theme()

	if c.Vibrancy() {
		c.Root().Background(pal.canvasBg.Alpha(0.76))
	} else {
		c.Root().Background(pal.canvasBg)
	}

	bar := c.TitleBar()
	topInset := max(float32(16), bar.Height+6)

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
								title = "（无标题）"
							}
							ui.Textf(c, "· 现在：%s — %s", a.status.Front.App, title).
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
									ui.Text(c, "● 屏幕休眠").FontSize(11).TextColor(pal.textSecondary)
								case capture.PhasePaused:
									ui.Text(c, "● 已暂停").FontSize(11).TextColor(pal.textSecondary)
								case capture.PhaseActive, capture.PhaseWaiting:
									ui.Text(c, "● 记录中").FontSize(11).TextColor(pal.switchOn)
								default:
									ui.Text(c, "● 已关闭").FontSize(11).TextColor(pal.textSecondary)
								}

								if a.runner != nil {
									toggleLabel := "开始记录"
									if a.status.Phase != capture.PhaseOff {
										toggleLabel = "暂停"
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
						ui.Text(c, "读取数据出错："+a.err).FontSize(12).TextColor(t.Danger)
					}

					// ---- 分页：时间轴 / 设置 ----
					//
					// 为什么从「一页长滚动」改成两页：加了供应商和 skill 之后
					// 设置区比内容还长，进来第一屏看到一堆开关是错的。
					// 时间轴是每天要看的东西，设置是配一次就不动的。
					a.renderTabs(c, pal)

					switch a.tab {
					case 0:
						a.renderHistorySection(c, pal)
					case 1:
						a.renderCaptureSection(c, pal)
						a.renderSummariesSection(c, pal)
						a.renderProvidersSection(c, pal)
						a.renderSkillSection(c, pal)
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

// rebuildGenerator 按当前配置重建摘要生成器。
//
// 每次设置变化都重建而不是就地改：生成器持有 masker（脱敏器），
// 换开关必须换一个新的 —— 旧的里面还留着上一个配置下的反查表，
// 用它去还原会换出不属于当前会话的原文。
