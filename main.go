package main

// Konseki（痕跡）—— 本机优先的个人时间轴。
//
// 采集层在 internal/capture，这里负责 mygo 原生界面、Liquid Glass 材质与生命周期。

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"konseki/internal/redact"
	"konseki/internal/summary"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

type app struct {
	runner *capture.Runner

	// ---- 持久层 ----
	cfg    *config.Store
	cfgNow config.Config
	sums   *summary.Store
	gen    *summary.Generator
	llmc   *llm.Client

	// ---- 界面与采集状态 ----
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

	// ---- Capture 设置 ----
	enableContextAwareness bool
	pauseDuration          string
	denylistApps           []string
	denylistSites          []string
	showDenylistPopover    bool
	denylistTab            int
	customAppInput         string
	customSiteInput        string

	// ---- 供应商 ----
	// 当前展开的是哪个供应商的配置面板，空表示都收起。
	openProvider    string
	addProviderOpen bool
	providerFilter  int // 0=全部 1=官方 2=中转 3=本地 4=订阅
	apiKeyDraft     string
	baseURLDraft    string
	modelDraft      string
	testing         bool
	testResult      string
	testOK          bool
	detected        []providers.Credential
	detectDone      bool
	fetchingModels  bool
	fetchMsg        string

	// ---- 摘要 ----
	// 视图切换：0 = 设置（Capture/Summaries/Providers/Skill），1 = 时间轴
	tab        int
	sumEntries []summary.Entry
	sumErr     string
	sumRunning bool
	// 摘要生成时的进度文字，UI 上直接显示。
	sumProgress string
	// 本次生成的脱敏命中数，给用户一个「遮了多少」的反馈。
	sumRedactions int
	showRedactLog bool

	// ---- Skill ----
	skillExportMsg  string
	skillExportedAt time.Time

	// ---- 其它 ----
	settingsInited bool
}

func (a *app) ensureInit() {
	if a.expanded == nil {
		a.expanded = map[string]bool{}
	}
	if a.dayFilter == "" {
		a.dayFilter = "Today"
	}
	if a.cfg != nil {
		a.cfgNow = a.cfg.Get()
	}
	if !a.settingsInited {
		a.settingsInited = true
		a.enableContextAwareness = true
		a.pauseDuration = "Select"
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
	a.loadDay()
	a.loadSummaries()
}

// loadDay 读回当天的时间块。
func (a *app) loadDay() {
	key := a.dayKey()
	blocks, err := a.runner.Timeline(key)
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

// loadSummaries 读回当天的摘要。
func (a *app) loadSummaries() {
	if a.sums == nil {
		return
	}
	ents, err := a.sums.Load(a.dayKey())
	if err != nil {
		a.sumErr = err.Error()
		return
	}
	a.sumErr = ""
	a.sumEntries = ents
}

// dayKey 把 UI 上的筛选词换成日期。
func (a *app) dayKey() string {
	switch a.dayFilter {
	case "Yesterday":
		return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	case "Last 7 Days", "Today":
		return time.Now().Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}

// dayLabel 给摘要生成用：昨天生成昨天，其余都当今天。
func (a *app) dayLabel() string {
	return a.dayKey()
}

// ---------- 摘要生成 ----------

// summarizable 说现在能不能生成摘要。UI 用它决定按钮是否置灰。
func (a *app) summarizable() bool {
	return a.gen != nil && len(a.blocks) > 0 && !a.sumRunning
}

// runSummary 在后台生成当天的摘要。
//
// 单独一个 goroutine：一次生成可能跑好几分钟（逐个小时调模型），
// 卡在 UI 线程里整窗都要冻结。所有回写到 UI 的字段都是简单类型，
// 读取方在下一帧就会看到，不需要额外的锁 —— mygo 的渲染是单线程的，
// 而这里写的是 bool/string/int，最坏情况是晚一帧生效。
func (a *app) runSummary(force bool) {
	if a.gen == nil || a.sumRunning {
		return
	}
	a.sumRunning = true
	a.sumErr = ""
	a.showRedactLog = false

	key := a.dayKey()
	blocks := a.blocks
	root := a.runner.Root()
	gen := a.gen

	go func() {
		defer func() { a.sumRunning = false }()

		ocr, err := a.runner.OCRIndex(key)
		if err != nil {
			ocr = nil // OCR 拿不到也能生成，靠窗口标题
		}
		window := time.Duration(a.cfgNow.Summary.WindowMinutes) * time.Minute
		if window <= 0 {
			window = time.Hour
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		entries, err := gen.GenerateDay(ctx, blocks, ocr, key, window, force)
		// 无论成败都把「这次遮了多少」带回去：失败时用户更需要知道
		// 发出去的内容到底遮没遮住。
		if m := gen.Masker(); m != nil {
			a.sumRedactions = len(m.Hits())
		}
		if err != nil {
			a.sumErr = err.Error()
			return
		}
		a.sumEntries = entries
		a.stampSkill(root)
	}()
}

// stampSkill 在摘要变化后重新导出 skill。
//
// 这是让外部 Agent 真能读到东西的关键：摘要生成完但没导出，
// 别的工具就还是看不到。导出本身很快（纯写文件），不值得再起
// 一个 goroutine。
func (a *app) stampSkill(root string) {
	_ = root // 保留参数是为了以后按数据目录分桶导出；现在 exporter 自己取路径
	if a.cfgNow.Skill.ExportDir == "" {
		return
	}
	if n, err := a.exportSkill(); err == nil {
		a.skillExportMsg = fmt.Sprintf("已导出 %d 个文件", n)
		a.skillExportedAt = time.Now()
	} else {
		a.skillExportMsg = "导出失败：" + err.Error()
	}
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
func (a *app) rebuildGenerator() {
	if a.cfg == nil || a.llmc == nil {
		return
	}
	a.cfgNow = a.cfg.Get()

	var pc *config.ProviderConfig
	for i := range a.cfgNow.Providers {
		p := &a.cfgNow.Providers[i]
		if p.Preset == a.cfgNow.Summary.ProviderID {
			pc = p
			break
		}
	}
	// 没指定就用第一个有 key 的。
	if pc == nil {
		for i := range a.cfgNow.Providers {
			p := &a.cfgNow.Providers[i]
			if p.APIKey != "" || p.AccessToken != "" || p.DefaultModel != "" {
				pc = p
				break
			}
		}
	}
	if pc == nil {
		a.gen = nil
		return
	}

	resolved, err := llm.Resolve(*pc, true)
	if err != nil {
		a.gen = nil
		a.sumErr = err.Error()
		return
	}

	key := pc.APIKey
	if pc.Auth == "oauth" && pc.AccessToken != "" {
		key = pc.AccessToken
	}
	model := a.cfgNow.Summary.Model
	if model == "" {
		model = resolved.Model
	}

	cfg := summary.LLMConfig{
		Kind:      resolved.Kind,
		BaseURL:   resolved.BaseURL,
		APIKey:    key,
		Header:    headersFor(*pc, resolved),
		Model:     model,
		Thinking:  a.cfgNow.Summary.ThinkingEffort,
		Language:  a.cfgNow.Summary.Language,
		MaxTokens: 4096,
		Redact: redact.Options{
			Secrets:  a.cfgNow.Redact.Enabled && a.cfgNow.Redact.Secrets,
			Personal: a.cfgNow.Redact.Enabled && a.cfgNow.Redact.Personal,
			Kinds:    a.cfgNow.Redact.Kinds,
			Allow:    a.cfgNow.Redact.Allow,
			Words:    a.cfgNow.Redact.Words,
		},
	}

	if a.gen == nil {
		a.gen = summary.New(a.sums, a.llmc, cfg, func(e summary.Entry) {
			a.sumEntries = append(a.sumEntries, e)
		})
	} else {
		a.gen.SetConfig(cfg)
	}
}

// setSummaryModel 切换摘要用的模型。
func (a *app) setSummaryModel(providerID, model string) {
	a.cfg.Update(func(c *config.Config) {
		c.Summary.ProviderID = providerID
		c.Summary.Model = model
	})
	a.rebuildGenerator()
}

// setThinking 切换推理档位。
func (a *app) setThinking(level string) {
	a.cfg.Update(func(c *config.Config) { c.Summary.ThinkingEffort = level })
	a.cfgNow = a.cfg.Get()
	if a.gen != nil {
		a.rebuildGenerator()
	}
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
			// Row 1: Retention
			// 采集到的原始截屏是最占空间也最敏感的东西，
			// 让用户能自己设一个上限，比事后清理体验好得多。
			retentionSub := "原始截图和屏幕文字保留的天数。摘要不会过期。"
			retentionLabel := fmt.Sprintf("%d days", a.cfgNow.RetentionDays)
			if a.cfgNow.RetentionDays <= 0 {
				retentionLabel = "Forever"
			}
			a.settingsRow(c, "Retention", retentionSub, pal, func() {
				menu := func(m *ui.Menu) {
					for _, days := range []int{7, 14, 30, 90, 0} {
						label := fmt.Sprintf("%d days", days)
						if days == 0 {
							label = "Forever"
						}
						if m.Item(label).Checked(a.cfgNow.RetentionDays == days).Chosen() {
							a.cfg.SetRetentionDays(days)
							a.cfgNow = a.cfg.Get()
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

	// 配置存在数据目录里，跟采集数据放一起 —— 备份/迁移时一个
	// 目录搬走就全套，不用记两个路径。
	cfg, err := config.Open(runner.Root())
	if err != nil {
		log.Fatal(err)
	}
	// 摘要和原始采样同目录，但分开放。删原始数据不该连带删摘要。
	sums, err := summary.NewStore(runner.Root())
	if err != nil {
		log.Fatal(err)
	}

	a := &app{
		runner:          runner,
		cfg:             cfg,
		cfgNow:          cfg.Get(),
		sums:            sums,
		llmc:            llm.New(5 * time.Minute),
		includeShowcase: false,
		expanded:        map[string]bool{},
	}
	a.rebuildGenerator()

	// 启动时就清一次过期数据。放在 goroutine 里是因为它要扫目录，
	// 而窗口应该在几百毫秒内出现。
	go func() {
		if days := a.cfgNow.RetentionDays; days > 0 {
			runner.Prune(days)
		}
	}()

	mygo.App.WhenReady(func() {
		warmOSAppIcons()
		runner.Start()

		mygo.NewWindow(mygo.WindowOptions{
			Title:           "痕跡 Konseki",
			TitleBarStyle:   mygo.TitleBarHidden,
			Transparent:     true,
			Vibrancy:        mygo.VibrancyGlass,
			BackgroundColor: "light-dark(#f7f8fa, #0f1115)",
			Width:           780,
			Height:          960,
			MinWidth:        560,
			MinHeight:       520,
			StateKey:        "main",
			Content:         ui.View(a.view),
		})
	})

	// 自动摘要。走一个独立的 ticker，和采集循环无关：
	// 采集是 5 秒一次的热路径，摘要要跑几分钟，混在一起会让
	// 采样被拖慢。
	//
	// 无条件启动，让循环自己读开关：用户在设置页打开 Auto 之后
	// 不该要求重启 App 才生效。
	go a.autoSummaryLoop()

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	runner.Stop()
}

// autoSummaryLoop 按设置里的间隔自动补摘要。
//
// 只在「有新的采样、且距上次生成超过间隔」时才跑。空转一次要
// 花一次模型的 token，白花钱还会让用户怀疑它在偷跑。
func (a *app) autoSummaryLoop() {
	// tick 用固定的 1 分钟，真正该跑的间隔在循环里按设置判断。
	//
	// 用 Ticker 而不是 time.Sleep(interval)：用户把间隔从 30 分钟
	// 改成 5 分钟时，睡着的 goroutine 叫不醒，得等那一觉睡完。
	// 1 分钟一次的检查几乎不花代价（只读几个字段）。
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	lastRun := time.Time{}

	for range tick.C {
		a.ensureInit()
		if !a.cfgNow.Summary.Auto || a.sumRunning || a.gen == nil {
			continue
		}
		if a.cfgNow.CapturePaused {
			continue
		}

		interval := time.Duration(a.cfgNow.Summary.IntervalMinutes) * time.Minute
		if interval < 5*time.Minute {
			interval = 5 * time.Minute
		}
		if !lastRun.IsZero() && time.Since(lastRun) < interval {
			continue
		}

		a.loadDay()
		if len(a.blocks) == 0 {
			continue
		}
		// 已经覆盖到最新一块就不用再跑了 —— 没有新内容时跑一次
		// 只是白烧 token。
		if a.coversLatest() {
			continue
		}
		lastRun = time.Now()
		a.runSummary(false)
	}
}

// coversLatest 说最后一小时是否已经有摘要了。
func (a *app) coversLatest() bool {
	if len(a.sumEntries) == 0 || len(a.blocks) == 0 {
		return false
	}
	lastBlock := a.blocks[len(a.blocks)-1].End
	for _, e := range a.sumEntries {
		if !e.End.Before(lastBlock) {
			return true
		}
	}
	return false
}
