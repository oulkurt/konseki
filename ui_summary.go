/*
 * [INPUT]: 依赖摘要、模型设置、导出服务与语言目录。
 * [OUTPUT]: 提供摘要设置、导出界面与分页导航。
 * [POS]: 设置页摘要区块与共享导航。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

// 摘要与 Skill 的设置页。
//
// 摘要页承担两件事：
//   1. 选模型、选推理档、开关自动生成
//   2. 显示"这一天生成了什么"——摘要本身要能被读到，否则整条链路
//      对用户是黑盒
//
// 这一页的时间线视图在 main.go 里，和 History 复用同一套渲染。

import (
	"fmt"
	"konseki/locales"
	"os"
	"time"

	"konseki/internal/config"
	"konseki/internal/providers"
	"konseki/internal/redact"
	"konseki/internal/skill"
	"konseki/internal/summary"

	"github.com/egoist/mygo/ui"
)

// renderSummariesSection 画摘要设置。
func (a *app) renderSummariesSection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "Summaries").
			FontSize(13).
			Bold().
			TextColor(pal.textSecondary).
			Margin(4, 0, 0, 10)

		a.settingsCard(c, pal, func() {
			// ---- 行 1：摘要模型 ----
			modelSub := "把一天的屏幕痕迹压成「在做什么」。走哪个模型都由你决定。"
			if a.cfgNow.Summary.Model != "" {
				modelSub = fmt.Sprintf("当前：%s · %s", a.cfgNow.Summary.ProviderID, a.cfgNow.Summary.Model)
			} else if len(a.cfgNow.Providers) == 0 {
				modelSub = "还没有配置供应商 —— 先在下面 Providers 里选一家。"
			}

			a.settingsRow(c, "Summary model", modelSub, pal, func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					// 模型菜单：按供应商分组
					menu := func(m *ui.Menu) {
						if len(a.cfgNow.Providers) == 0 {
							m.Item("没有可用的供应商").Disabled(true)
							return
						}
						for _, pc := range a.cfgNow.Providers {
							preset, _ := providers.ByID(pc.Preset)
							name := pc.Label
							if name == "" {
								name = preset.Name
							}
							if name == "" {
								name = pc.Preset
							}
							models := preset.Models
							if len(models) == 0 && pc.DefaultModel != "" {
								models = []string{pc.DefaultModel}
							}
							if len(models) == 0 {
								m.Item(name + "（未选模型）").Disabled(true)
								continue
							}
							m.Submenu(name, func(sub *ui.Menu) {
								for _, mdl := range models {
									if sub.Item(mdl).
										Checked(a.cfgNow.Summary.ProviderID == pc.Preset && a.cfgNow.Summary.Model == mdl).
										Chosen() {
										a.setSummaryModel(pc.Preset, mdl)
									}
								}
							})
						}
					}

					label := a.cfgNow.Summary.Model
					if label == "" {
						label = "选择模型"
					}
					renderSelectPill(c, "btn-summary-model", label, 150, pal.textPrimary, pal, menu)

					// 推理档位
					effortMenu := func(m *ui.Menu) {
						m.Item("Thinking effort").Disabled(true)
						for _, lvl := range []string{"off", "low", "medium", "high", "max"} {
							if m.Item(lvl).Checked(a.cfgNow.Summary.ThinkingEffort == lvl).Chosen() {
								a.setThinking(lvl)
							}
						}
					}
					renderSelectPill(c, "btn-thinking", a.cfgNow.Summary.ThinkingEffort, 86, pal.textPrimary, pal, effortMenu)
				})
			})

			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)

			// ---- 行 2：自动生成 ----
			autoSub := "开着的话，每 30 分钟自己补一段摘要。"
			if a.cfgNow.Summary.Auto {
				autoSub = fmt.Sprintf("每 %d 分钟自动生成一次。", a.cfgNow.Summary.IntervalMinutes)
			}
			a.settingsRow(c, "Auto summarize", autoSub, pal, func() {
				auto := a.cfgNow.Summary.Auto
				if renderPillSwitch(c, "sw-auto-summary", &auto, pal) {
					a.cfg.Update(func(cfg *config.Config) { cfg.Summary.Auto = auto })
					a.cfgNow = a.cfg.Get()
				}
			})

			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)

			// ---- 行 3：手动生成 ----
			a.settingsRow(c, "Generate now",
				"把今天还没摘要的部分补上。已有的块会跳过。", pal, func() {
					a.renderGenerateButton(c, pal)
				})
		})

		// ---- 脱敏状态 ----
		a.renderRedactCard(c, pal)

		// ---- 已生成的摘要 ----
		if len(a.sumEntries) > 0 {
			a.renderSummaryList(c, pal)
		}
	})
}

func (a *app) renderGenerateButton(c *ui.Context, pal palette) {
	if a.sumRunning {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, 4).Children(func() {
			ui.Spinner(c).Size(12, 12)
			ui.Text(c, "生成中…").FontSize(12).TextColor(pal.textSecondary)
		})
		return
	}
	if !a.summarizable() && len(a.cfgNow.Providers) == 0 {
		ui.Text(c, "先配置供应商").FontSize(12).TextColor(pal.textSecondary)
		return
	}

	btn := ui.ButtonBase(c.Key("btn-generate")).Padding(5, 12).Radius(8).
		Background(pal.rowHoverBg).Border(1, pal.controlBorder)
	if btn.Hovered() {
		btn.Background(pal.cardBorder)
	}
	btn.Children(func() {
		ui.Text(c, "生成摘要").FontSize(12).Bold().TextColor(pal.textPrimary)
	})
	if btn.Clicked() {
		a.runSummary(false)
	}
}

// renderRedactCard 让用户看到隐私开关，并且能确认它真的在起作用。
func (a *app) renderRedactCard(c *ui.Context, pal palette) {
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					ui.Text(c, "脱敏").FontSize(13).Bold().TextColor(pal.textPrimary)
					ui.Text(c, "发给模型之前，把不该出门的东西换成占位符，回来再换回去。").
						FontSize(11.8).TextColor(pal.textSecondary).LineHeight(1.38)
				})
				on := a.cfgNow.Redact.Enabled
				if renderPillSwitch(c, "sw-redact", &on, pal) {
					a.cfg.Update(func(cfg *config.Config) { cfg.Redact.Enabled = on })
					a.cfgNow = a.cfg.Get()
					a.rebuildGenerator()
				}
			})

			if !a.cfgNow.Redact.Enabled {
				ui.Text(c, "已关闭。你的屏幕内容会原样发到模型供应商。").
					FontSize(11.5).
					TextColor(ui.Hex("#DC2626"))
				return
			}

			// 两类开关
			ui.Row(c).FillWidth().Gap(14).Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, "凭据与密码").FontSize(12).TextColor(pal.textPrimary)
					secrets := a.cfgNow.Redact.Secrets
					if renderPillSwitch(c, "sw-redact-secrets", &secrets, pal) {
						a.cfg.Update(func(cfg *config.Config) { cfg.Redact.Secrets = secrets })
						a.cfgNow = a.cfg.Get()
						a.rebuildGenerator()
					}
				})
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, "个人数据").FontSize(12).TextColor(pal.textPrimary)
					personal := a.cfgNow.Redact.Personal
					if renderPillSwitch(c, "sw-redact-personal", &personal, pal) {
						a.cfg.Update(func(cfg *config.Config) { cfg.Redact.Personal = personal })
						a.cfgNow = a.cfg.Get()
						a.rebuildGenerator()
					}
				})
			})

			// 分类细分
			ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
				for _, cat := range redact.Categories {
					cat := cat
					on, ok := a.cfgNow.Redact.Kinds[cat.ID]
					if !ok {
						on = cat.On
					}
					label := cat.Name
					if on {
						label = "✓ " + label
					}
					chip := ui.ButtonBase(c.Key("redact-cat-"+cat.ID)).
						Padding(3, 8).Radius(6).
						Background(pal.codeBg)
					if chip.Hovered() {
						chip.Background(pal.rowHoverBg)
					}
					chip.Children(func() {
						ui.Text(c, label).FontSize(11).TextColor(pal.codeFg)
					})
					if chip.Clicked() {
						next := !on
						a.cfg.Update(func(cfg *config.Config) {
							if cfg.Redact.Kinds == nil {
								cfg.Redact.Kinds = map[string]bool{}
							}
							cfg.Redact.Kinds[cat.ID] = next
						})
						a.cfgNow = a.cfg.Get()
						a.rebuildGenerator()
					}
				}
			})

			// 本次生成遮了什么。这是最直接的信任建立手段。
			if a.sumRedactions > 0 {
				ui.Text(c, fmt.Sprintf("上一次生成：遮掉了 %d 处内容。", a.sumRedactions)).
					FontSize(11.5).
					TextColor(pal.switchOn)
			}
		})
	})
}

// renderSummaryList 列出已生成的摘要卡片。
func (a *app) renderSummaryList(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("今天的摘要 · %d 段", len(a.sumEntries))).
				FontSize(13).Bold().TextColor(pal.textSecondary).Margin(4, 0, 0, 10)
			ui.Spacer(c)
			if a.sumErr != "" {
				ui.Text(c, a.sumErr).FontSize(11).TextColor(ui.Hex("#DC2626")).SingleLine()
			}
		})

		a.settingsCard(c, pal, func() {
			ui.Column(c).FillWidth().Padding(4, 0).Children(func() {
				for i, e := range a.sumEntries {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider).Margin(0, 16)
					}
					renderSummaryEntry(c, e, pal)
				}
			})
		})
	})
}

func renderSummaryEntry(c *ui.Context, e summary.Entry, pal palette) {
	ui.Column(c).FillWidth().Padding(12, 18).Gap(5).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, fmt.Sprintf("%s–%s", e.Start.Format("15:04"), e.End.Format("15:04"))).
				FontSize(11.5).
				TextColor(pal.textSecondary).
				FontFeatures("tnum")
			ui.Text(c, e.Headline).FontSize(12.8).Bold().TextColor(pal.textPrimary).SingleLine().Grow(1)
			if e.Redactions > 0 {
				ui.Text(c, fmt.Sprintf("遮 %d", e.Redactions)).
					FontSize(10).
					TextColor(pal.textSecondary).
					Padding(1, 5).
					Radius(999).
					Background(pal.codeBg)
			}
		})
		if e.Summary != "" {
			ui.Text(c, e.Summary).FontSize(12).TextColor(pal.textSecondary).LineHeight(1.45)
		}
		if len(e.Open) > 0 {
			ui.Row(c).AlignItems(ui.Start).Gap(6).FillWidth().Children(func() {
				ui.Text(c, "未完成").FontSize(11).Bold().TextColor(ui.Hex("#D97706"))
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					for _, o := range e.Open {
						ui.Text(c, "· "+o).FontSize(11.5).TextColor(pal.textSecondary)
					}
				})
			})
		}
		if len(e.Topics) > 0 {
			ui.Row(c).Gap(4).Wrap().Children(func() {
				for _, t := range e.Topics {
					ui.Text(c, t).FontSize(10.5).TextColor(pal.codeFg).
						Padding(1, 6).Radius(999).Background(pal.codeBg)
				}
			})
		}
	})
}

// ---------- Skill ----------

func (a *app) renderSkillSection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "Skill").
			FontSize(13).
			Bold().
			TextColor(pal.textSecondary).
			Margin(4, 0, 0, 10)

		a.settingsCard(c, pal, func() {
			ui.Column(c).FillWidth().Padding(14, 18).Gap(12).Children(func() {
				ui.Column(c).Gap(3).Children(func() {
					ui.Text(c, "让别的 Agent 认识你").FontSize(13).Bold().TextColor(pal.textPrimary)
					ui.Text(c, "把痕迹导出成一个 skill，Claude Code / Codex / Cursor 都能读。「我昨天在干什么」不用再解释一遍。").
						FontSize(11.8).TextColor(pal.textSecondary).LineHeight(1.4)
				})

				// 输出位置
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Text(c, "输出目录").FontSize(12).Bold().TextColor(pal.textPrimary)
					dir := a.cfgNow.Skill.ExportDir
					if dir == "" {
						dir = skill.DefaultDir()
					}
					ui.TextInput(c.Key("skill-dir"), &dir).FillWidth()
					ui.Text(c, dir).FontSize(10.5).TextColor(pal.textSecondary.Alpha(0.8)).SingleLine()
				})

				// 选项
				ui.Row(c).FillWidth().Gap(18).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "带上原始 OCR").FontSize(12).TextColor(pal.textPrimary)
						ui.Text(c, "默认关。开了之后导出的数据体积和敏感度都会上升。").
							FontSize(10.5).TextColor(pal.textSecondary).LineHeight(1.3)
						on := a.cfgNow.Skill.IncludeOCR
						if renderPillSwitch(c, "sw-skill-ocr", &on, pal) {
							a.cfg.Update(func(cfg *config.Config) { cfg.Skill.IncludeOCR = on })
							a.cfgNow = a.cfg.Get()
						}
					})
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "导出前再脱敏一次").FontSize(12).TextColor(pal.textPrimary)
						ui.Text(c, "导出是第二个外发口，值得多遮一遍。默认开。").
							FontSize(10.5).TextColor(pal.textSecondary).LineHeight(1.3)
						on := a.cfgNow.Skill.RedactOnExport
						if renderPillSwitch(c, "sw-skill-redact", &on, pal) {
							a.cfg.Update(func(cfg *config.Config) { cfg.Skill.RedactOnExport = on })
							a.cfgNow = a.cfg.Get()
						}
					})
				})

				// 导出按钮
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					btn := ui.ButtonBase(c.Key("btn-skill-export")).
						Padding(5, 12).Radius(8).
						Background(pal.rowHoverBg).
						Border(1, pal.controlBorder)
					if btn.Hovered() {
						btn.Background(pal.cardBorder)
					}
					btn.Children(func() {
						ui.Text(c, "导出 Skill").FontSize(12).Bold().TextColor(pal.textPrimary)
					})
					if btn.Clicked() {
						if n, err := a.exportSkill(); err != nil {
							a.skillExportMsg = "导出失败：" + err.Error()
						} else {
							a.skillExportMsg = fmt.Sprintf("已导出 %d 个文件", n)
							a.skillExportedAt = time.Now()
						}
					}

					if a.skillExportMsg != "" {
						ui.Text(c, a.skillExportMsg).FontSize(11.5).TextColor(pal.switchOn).SingleLine()
					}

					ui.Spacer(c)
					if a.skillExportedAt != (time.Time{}) {
						ui.Text(c, "上次："+a.skillExportedAt.Format("15:04")).
							FontSize(11).TextColor(pal.textSecondary)
					}
				})

				// 目录里有什么
				ui.Column(c).FillWidth().Gap(3).Padding(10, 12).Radius(8).
					Background(pal.codeBg).Children(func() {
					ui.Text(c, "导出的内容").FontSize(11).Bold().TextColor(pal.textSecondary)
					for _, line := range []string{
						"SKILL.md — 入口说明，Agent 读它决定要不要用",
						"KONSEKI.md — 跨天画像：时间花在哪、反复出现的主题、悬着没做完的",
						"days/<日期>.md — 一天的人话版，摘要 + 原始时间轴",
						"days/<日期>.json — 同一份数据的机器可读版",
					} {
						ui.Text(c, line).FontSize(10.8).TextColor(pal.codeFg)
					}
				})
			})
		})
	})
}

// exportSkill 收集最近 N 天的数据并写出。
func (a *app) exportSkill() (int, error) {
	if a.runner == nil || a.sums == nil {
		return 0, fmt.Errorf("还没准备好")
	}
	days := a.cfgNow.Skill.Days
	if days <= 0 {
		days = 7
	}

	// 先在 Store 里看哪些天有摘要 —— 没有摘要的日期不导出，
	// 免得给 Agent 一堆空文件。
	have := a.sums.Days()
	if len(have) > days {
		have = have[:days]
	}
	if len(have) == 0 {
		// 还没生成过摘要，就把今天的时间轴导出去。
		have = []string{a.dayKey()}
	}

	var data []skill.DayData
	for _, day := range have {
		blocks, err := a.runner.Timeline(day)
		if err != nil {
			continue
		}
		entries, _ := a.sums.Load(day)
		d := skill.DayData{Day: day, Blocks: blocks, Entries: entries}
		if a.cfgNow.Skill.IncludeOCR {
			if ocr, err := a.runner.OCRIndex(day); err == nil {
				d.OCR = ocr
			}
		}
		data = append(data, d)
	}

	opts := skill.Options{
		Dir:        a.cfgNow.Skill.ExportDir,
		Days:       days,
		IncludeOCR: a.cfgNow.Skill.IncludeOCR,
		Owner:      ownerName(),
	}
	return skill.Export(opts, data)
}

// ownerName 拿系统的用户名当默认称呼。
//
// 不引入配置项：这个值只影响 SKILL.md 里的一句话，让用户专门
// 去填一个名字收益太低。想改就直接改导出的文件。
func ownerName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "the user"
}

// renderTabs 画顶部的时间轴 / 设置切换。
//
// 用分段控件而不是侧边栏：这个 App 只有两页，侧边栏的导航成本
// 比它省下的空间贵。分段控件还能顺便显示「设置里有几项没配好」。
func (a *app) renderTabs(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	names := []string{msg.TabsTimeline(), msg.TabsSettings()}

	// 提示点：没配供应商，或者有内容但还没生成摘要。
	needSetup := len(a.cfgNow.Providers) == 0
	needSummary := len(a.cfgNow.Providers) > 0 && len(a.blocks) > 0 && len(a.sumEntries) == 0
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

		// 右上角一句状态：一句话说清现在到底有没有在记
		switch {
		case a.sumRunning:
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(11, 11)
				ui.Text(c, msg.TabsGenerating()).FontSize(11).TextColor(pal.textSecondary)
			})
		case needSetup:
			ui.Text(c, msg.TabsSetup()).FontSize(11).TextColor(pal.textSecondary)
		}
	})
}
