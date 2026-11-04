package main

// Konseki（痕跡）—— 本机优先的个人时间轴。
//
// 采集层在 internal/capture，这里负责 mygo 原生界面、Liquid Glass 材质与生命周期。

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"konseki/internal/capture"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

type app struct {
	runner *capture.Runner

	// 界面与采集状态
	blocks          []capture.Block
	status          capture.Status
	doctor          string
	showDiag        bool
	lastLoad        time.Time
	err             string
	listState       ui.ListState
	scrollState     ui.ScrollState
	expanded        map[string]bool
	includeShowcase bool
	dayFilter       string

	// Capture & Summaries 设置项状态（UI/UX）
	settingsInited         bool
	enableContextAwareness bool
	pauseDuration          string
	captureTypedText       bool
	allowScreenRecording   bool

	// Denylist（支持 App 与 Website 两类）
	showDenylistPopover bool
	denylistTab         int // 0 = Apps, 1 = Websites
	denylistApps        []string
	denylistSites       []string
	customAppInput      string
	customSiteInput     string

	// Summary model & Thinking effort（思考程度）& Retention
	summaryModel   string
	thinkingEffort string
	retention      string
}

func (a *app) ensureInit() {
	if a.expanded == nil {
		a.expanded = map[string]bool{}
	}
	if a.dayFilter == "" {
		a.dayFilter = "Today"
	}
	if !a.settingsInited {
		a.settingsInited = true
		a.enableContextAwareness = true
		a.pauseDuration = "Select"
		a.captureTypedText = true
		a.allowScreenRecording = true
		a.summaryModel = "Gemini 3.8 ..."
		a.thinkingEffort = "Medium"
		a.retention = "14 days"
	}
}

// refresh 重新读一次数据和状态。每帧调用，但内部按 2 秒节流，
// 避免每秒都在扫 JSONL。
func (a *app) refresh() {
	a.ensureInit()
	if time.Since(a.lastLoad) < 2*time.Second {
		return
	}
	a.lastLoad = time.Now()
	if a.runner == nil {
		return
	}
	a.status = a.runner.Status()

	blocks, err := a.runner.Timeline("")
	if err != nil {
		if !os.IsNotExist(err) {
			a.err = err.Error()
		}
		a.blocks = nil
		return
	}
	a.err = ""
	a.blocks = blocks
}

type palette struct {
	canvasBg      ui.Color
	cardBg        ui.Color
	cardBorder    ui.Color
	rowDivider    ui.Color
	controlBorder ui.Color
	textPrimary   ui.Color
	textSecondary ui.Color
	textSection   ui.Color
	textTime      ui.Color
	tickMajor     ui.Color
	tickMinor     ui.Color
	rowHoverBg    ui.Color
	rowExpandedBg ui.Color
	codeBg        ui.Color
	codeFg        ui.Color
	bulletColor   ui.Color
	switchOn      ui.Color
	switchOff     ui.Color
}

func themePalette(c *ui.Context) palette {
	t := c.Theme()
	isDark := t.Background.R < 80 && t.Background.G < 80 && t.Background.B < 80
	if isDark {
		return palette{
			canvasBg:      ui.Hex("#0F1115"),
			cardBg:        ui.Hex("#16181E"),
			cardBorder:    ui.RGBA(255, 255, 255, 0.09),
			rowDivider:    ui.RGBA(255, 255, 255, 0.06),
			controlBorder: ui.RGBA(255, 255, 255, 0.14),
			textPrimary:   ui.Hex("#F3F4F6"),
			textSecondary: ui.Hex("#9CA3AF"),
			textSection:   ui.Hex("#D1D5DB"),
			textTime:      ui.Hex("#9CA3AF"),
			tickMajor:     ui.Hex("#E5E7EB"),
			tickMinor:     ui.RGBA(156, 163, 175, 0.28),
			rowHoverBg:    ui.RGBA(255, 255, 255, 0.055),
			rowExpandedBg: ui.RGBA(255, 255, 255, 0.03),
			codeBg:        ui.RGBA(255, 255, 255, 0.11),
			codeFg:        ui.Hex("#E5E7EB"),
			bulletColor:   ui.Hex("#9CA3AF"),
			switchOn:      ui.Hex("#15B76C"),
			switchOff:     ui.Hex("#3F3F46"),
		}
	}
	return palette{
		canvasBg:      ui.Hex("#F7F8FA"),
		cardBg:        ui.Hex("#FFFFFF"),
		cardBorder:    ui.Hex("#E6E8EC"),
		rowDivider:    ui.Hex("#EFEFEF"),
		controlBorder: ui.Hex("#E2E4E9"),
		textPrimary:   ui.Hex("#111827"),
		textSecondary: ui.Hex("#6B7280"),
		textSection:   ui.Hex("#4B5563"),
		textTime:      ui.Hex("#6B7280"),
		tickMajor:     ui.Hex("#1F2937"),
		tickMinor:     ui.RGBA(156, 163, 175, 0.52),
		rowHoverBg:    ui.RGBA(243, 244, 246, 0.85),
		rowExpandedBg: ui.RGBA(249, 250, 251, 0.65),
		codeBg:        ui.RGBA(15, 23, 42, 0.065),
		codeFg:        ui.Hex("#1E293B"),
		bulletColor:   ui.Hex("#6B7280"),
		switchOn:      ui.Hex("#15B76C"),
		switchOff:     ui.Hex("#E4E4E7"),
	}
}

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

						if a.status.App != "" {
							title := a.status.Title
							if title == "" {
								title = "（无标题）"
							}
							ui.Textf(c, "· 现在：%s — %s", a.status.App, title).
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
								switch {
								case a.status.Blank:
									ui.Text(c, "● 屏幕休眠").FontSize(11).TextColor(pal.textSecondary)
								case a.pauseDuration != "Select":
									ui.Textf(c, "● 已暂停 (%s)", a.pauseDuration).FontSize(11).TextColor(pal.textSecondary)
								case a.status.Running && a.enableContextAwareness:
									ui.Text(c, "● 记录中").FontSize(11).TextColor(pal.switchOn)
								default:
									ui.Text(c, "● 已暂停").FontSize(11).TextColor(pal.textSecondary)
								}

								if a.runner != nil {
									toggleLabel := "开始记录"
									if a.status.Running {
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
										if a.status.Running {
											a.runner.Stop()
											a.enableContextAwareness = false
										} else {
											a.runner.Start()
											a.enableContextAwareness = true
											a.pauseDuration = "Select"
										}
										a.status = a.runner.Status()
									}
								}
							})
					})

					if a.err != "" {
						ui.Text(c, "读取数据出错："+a.err).FontSize(12).TextColor(t.Danger)
					}

					// ================================================================
					// 1. Capture 模块
					// ================================================================
					a.renderCaptureSection(c, pal)

					// ================================================================
					// 2. Summaries 模块
					// ================================================================
					a.renderSummariesSection(c, pal)

					// ================================================================
					// 3. History 模块
					// ================================================================
					a.renderHistorySection(c, pal)
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

// renderCaptureSection 绘制 Capture 设置分组（两张圆角卡片）
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
					changed := renderPillSwitch(c, "sw-enable-context", &a.enableContextAwareness, pal)
					if changed && a.runner != nil {
						if a.enableContextAwareness {
							a.runner.Start()
						} else {
							a.runner.Stop()
						}
						a.status = a.runner.Status()
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
			// Row 1: Capture typed text
			a.settingsRow(c,
				"Capture typed text",
				"Save the sentences you type. Aside never stores keystrokes or password fields.",
				pal,
				func() {
					renderPillSwitch(c, "sw-capture-typed", &a.captureTypedText, pal)
				},
			)

			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)

			// Row 2: Allow Screen Recording
			a.settingsRow(c,
				"Allow Screen Recording",
				"When a page has no readable text, read it from the screen. Only the text is kept.",
				pal,
				func() {
					renderPillSwitch(c, "sw-allow-screen", &a.allowScreenRecording, pal)
				},
			)

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
									rowBtn := ui.ButtonBase(c.Key("deny-app-" + appName)).
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
									rowBtn := ui.ButtonBase(c.Key("deny-site-" + site)).
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
							chip := ui.ButtonBase(c.Key("chip-app-" + appName)).
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
							chip := ui.ButtonBase(c.Key("chip-site-" + site)).
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

// renderSummariesSection 绘制 Summaries 设置分组（模型选择 + 思考程度选择 + 保留天数选择）
func (a *app) renderSummariesSection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "Summaries").
			FontSize(13).
			Bold().
			TextColor(pal.textSecondary).
			Margin(4, 0, 0, 10)

		a.settingsCard(c, pal, func() {
			// Row 1: Summary model（左侧选模型，右侧 ••• 选思考程度 Thinking effort）
			modelSubtitle := "Summarizes your activity in the background while you work."
			if a.thinkingEffort != "Medium" {
				modelSubtitle = fmt.Sprintf("Summarizes your activity in the background while you work. · Thinking: %s", a.thinkingEffort)
			}
			a.settingsRow(c,
				"Summary model",
				modelSubtitle,
				pal,
				func() {
					ui.Row(c).
						Height(32).
						Radius(8).
						Background(pal.cardBg).
						Border(1, pal.controlBorder).
						Clip().
						Children(func() {
							// 左半段：模型选择菜单（同时包含思考程度子菜单）
							modelMenu := func(m *ui.Menu) {
								models := []string{
									"Gemini 3.8 ...",
									"Gemini 3.8 Pro",
									"Gemini 3.8 Flash",
									"Claude 4.5 Sonnet",
									"Claude 4.5 Haiku",
									"GPT-5.2",
									"GPT-5 Mini",
									"Qwen3-30B Local",
								}
								for _, mdl := range models {
									if m.Item(mdl).Checked(a.summaryModel == mdl).Chosen() {
										a.summaryModel = mdl
									}
								}
								m.Separator()
								m.Submenu("Thinking effort (思考程度)", func(sub *ui.Menu) {
									for _, lvl := range []string{"Off", "Low", "Medium", "High", "Max"} {
										if sub.Item(lvl).Checked(a.thinkingEffort == lvl).Chosen() {
											a.thinkingEffort = lvl
										}
									}
								})
							}

							leftBtn := ui.ButtonBase(c.Key("btn-summary-model")).
								FillHeight().
								Padding(0, 10).
								Gap(6).
								PopoverMenuButton(modelMenu)
							if leftBtn.Hovered() {
								leftBtn.Background(pal.rowHoverBg)
							}
							leftBtn.Children(func() {
								ui.Icon(c, modelClusterSVG).FontSize(13).TextColor(pal.textPrimary)
								ui.Text(c, a.summaryModel).FontSize(12.5).Bold().TextColor(pal.textPrimary).SingleLine()
								ui.Icon(c, chevronDownSVG).FontSize(11).TextColor(pal.textSecondary)
							})

							ui.Box(c).Width(1).FillHeight().Background(pal.controlBorder)

							// 右半段 •••：直接选择思考程度（Thinking effort）
							thinkingMenu := func(m *ui.Menu) {
								m.Item("Thinking effort (思考程度)").Disabled(true)
								for _, lvl := range []string{"Off", "Low", "Medium", "High", "Max"} {
									if m.Item("Thinking: " + lvl).Checked(a.thinkingEffort == lvl).Chosen() {
										a.thinkingEffort = lvl
									}
								}
							}

							rightBtn := ui.ButtonBase(c.Key("btn-summary-model-more")).
								Width(28).
								FillHeight().
								Center().
								Label("Thinking effort").
								Tooltip("Thinking effort: " + a.thinkingEffort).
								PopoverMenuButton(thinkingMenu)
							if rightBtn.Hovered() {
								rightBtn.Background(pal.rowHoverBg)
							}
							rightBtn.Children(func() {
								ui.Icon(c, moreHorizontalSVG).FontSize(12.5).TextColor(pal.textPrimary)
							})
						})
				},
			)

			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)

			// Row 2: Retention
			a.settingsRow(c,
				"Retention",
				"Aside deletes raw captures after this many days. Summaries stay.",
				pal,
				func() {
					retentionMenu := func(m *ui.Menu) {
						for _, opt := range []string{"7 days", "14 days", "30 days", "90 days", "Forever"} {
							if m.Item(opt).Checked(a.retention == opt).Chosen() {
								a.retention = opt
							}
						}
					}
					renderSelectPill(c, "btn-retention", a.retention, 118, pal.textPrimary, pal, retentionMenu)
				},
			)
		})
	})
}

// renderHistorySection 绘制 History 时间轴卡片
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

func (a *app) settingsCard(c *ui.Context, pal palette, children func()) {
	ui.Column(c).
		FillWidth().
		Radius(14).
		Background(pal.cardBg).
		Border(1, pal.cardBorder).
		Shadow(0, 2, 10, -2, ui.RGBA(15, 23, 42, 0.03)).
		Clip().
		Children(children)
}

func (a *app) settingsRow(c *ui.Context, title, subtitle string, pal palette, rightControl func()) {
	ui.Row(c).
		FillWidth().
		Padding(14, 18).
		AlignItems(ui.Center).
		Gap(16).
		Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, title).
					FontSize(13.5).
					Bold().
					TextColor(pal.textPrimary)
				ui.Text(c, subtitle).
					FontSize(12.2).
					TextColor(pal.textSecondary).
					LineHeight(1.38)
			})
			rightControl()
		})
}

// renderPillSwitch 绘制与设计稿一致的翡翠绿胶囊开关（带加宽白色胶囊滑块）
func renderPillSwitch(c *ui.Context, key string, val *bool, pal palette) bool {
	sw := ui.SwitchBase(c.Key(key), val)
	sw.Size(44, 24).Radius(12).Cursor(ui.CursorPointer)

	trackColor := pal.switchOff
	targetX := float32(2.5)
	if *val {
		trackColor = pal.switchOn
		targetX = 19.5
	}
	thumbX := sw.Animate("thumb-x", targetX, 150*time.Millisecond)

	sw.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, trackColor, 12)
		thumbRect := ui.Rect{
			X: r.X + thumbX,
			Y: r.Y + 2.5,
			W: 22,
			H: 19,
		}
		p.Shadow(thumbRect, 9.5, 0, 1, 2.5, 0, ui.RGBA(0, 0, 0, 0.15))
		p.Fill(thumbRect, ui.Hex("#FFFFFF"), 9.5)
	})

	return sw.Changed()
}

// renderSelectPill 绘制带上下箭头的下拉选择按钮（如 Select ⌃⌄、14 days ⌃⌄）
func renderSelectPill(c *ui.Context, key, label string, minW float32, textColor ui.Color, pal palette, menu func(*ui.Menu)) {
	btn := ui.ButtonBase(c.Key(key)).
		MinWidth(minW).
		Height(32).
		Padding(0, 12).
		Gap(8).
		Radius(8).
		Background(pal.cardBg).
		Border(1, pal.controlBorder).
		Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: 0.7}).
		PopoverMenuButton(menu)
	if btn.Hovered() {
		btn.Background(pal.rowHoverBg)
	}
	btn.Children(func() {
		ui.Text(c, label).
			FontSize(12.5).
			Bold().
			TextColor(textColor).
			Grow(1).
			SingleLine()
		ui.Icon(c, chevronsUpDownSVG).FontSize(12.5).TextColor(pal.textSecondary)
	})
}

// renderTimelineSlot 绘制单条时间轴节点：左侧时间 + 连续刻度尺，右侧可展开卡片。
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

func main() {
	runner, err := capture.New(capture.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		runner:          runner,
		includeShowcase: true,
		expanded:        map[string]bool{},
	}

	mygo.App.WhenReady(func() {
		warmOSAppIcons()
		runner.Start()

		mygo.NewWindow(mygo.WindowOptions{
			Title:           "痕跡 Konseki",
			TitleBarStyle:   mygo.TitleBarHidden,
			Transparent:     true,
			Vibrancy:        mygo.VibrancyGlass,
			BackgroundColor: "light-dark(#f7f8fa, #0f1115)",
			Width:           720,
			Height:          940,
			MinWidth:        500,
			MinHeight:       460,
			StateKey:        "main",
			Content:         ui.View(a.view),
		})
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
