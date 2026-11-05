/*
 * [INPUT]: 依赖供应商预设、配置与界面组件。
 * [OUTPUT]: 提供供应商设置界面和配置操作。
 * [POS]: 设置页供应商区块。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"slices"
	"strings"
)

var providerFilterNames = []string{"全部", "官方", "中转", "本地"}

func kindLabel(k providers.Kind) string {
	switch k {
	case providers.KindVendor:
		return "官方"
	case providers.KindRelay:
		return "中转"
	case providers.KindLocal:
		return "本地"
	}
	return ""
}

func (a *app) renderProvidersSection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Providers").
				FontSize(13).
				Bold().
				TextColor(pal.textSecondary)
			ui.Spacer(c)
			ui.Text(c, fmt.Sprintf("%d 个可用", len(providers.All()))).
				FontSize(11.5).
				TextColor(pal.textSecondary.Alpha(0.7))
		})

		// ---- 已配置的供应商 ----
		if len(a.cfgNow.Providers) == 0 {
			a.settingsCard(c, pal, func() {
				ui.Column(c).FillWidth().Padding(20, 18).Gap(8).Children(func() {
					ui.Text(c, "还没有配置模型").FontSize(13).Bold().TextColor(pal.textPrimary)
					ui.Text(c, "Konseki 本身不含模型。你需要在下面选一家，填上自己的 key —— 或者直接从本机已登录的 CLI 导入。").
						FontSize(12.2).TextColor(pal.textSecondary).LineHeight(1.4)
				})
			})
		} else {
			for i := range a.cfgNow.Providers {
				a.renderConfiguredProvider(c, i, pal)
			}
		}

		// ---- 本机凭据导入 ----
		a.renderLocalImport(c, pal)

		// ---- 添加供应商 ----
		a.renderProviderPicker(c, pal)
	})
}

// renderConfiguredProvider 画一个已经配好的供应商。
func (a *app) renderConfiguredProvider(c *ui.Context, idx int, pal palette) {
	pc := a.cfgNow.Providers[idx]
	preset, _ := providers.ByID(pc.Preset)
	name := pc.Label
	if name == "" {
		name = providers.DisplayName(preset)
	}
	if name == "" {
		name = pc.Preset
	}

	// 解析一次，UI 上要显示最终生效的地址和协议。
	resolved, rerr := llm.Resolve(pc, true)
	isOpen := a.openProvider == pc.Preset+name

	a.settingsCard(c, pal, func() {
		ui.Row(c).FillWidth().Padding(14, 18).AlignItems(ui.Center).Gap(12).Children(func() {
			// 左侧：名字 + 状态
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13.5).Bold().TextColor(pal.textPrimary)
					if preset.Kind != "" {
						ui.Text(c, kindLabel(preset.Kind)).
							FontSize(10.5).
							TextColor(pal.textSecondary).
							Padding(2, 6).
							Radius(999).
							Background(pal.rowHoverBg)
					}
				})
				sub := pc.DefaultModel
				if sub == "" {
					sub = "未选模型"
				}
				if rerr != nil {
					sub = rerr.Error()
				} else {
					sub = fmt.Sprintf("%s · %s", sub, protocolLabel(resolved.Kind))
				}
				ui.Text(c, sub).FontSize(12).TextColor(pal.textSecondary).SingleLine()
			})

			// 设为默认：只有非默认的才显示
			active := a.cfgNow.Summary.ProviderID == pc.Preset && a.cfgNow.Summary.Model == pc.DefaultModel
			if active {
				ui.Text(c, "默认").
					FontSize(10.5).
					Bold().
					TextColor(pal.switchOn).
					Padding(2, 7).
					Radius(999).
					Background(pal.switchOn.Alpha(0.12))
			} else {
				useBtn := ui.ButtonBase(c.Key("prov-use-"+pc.Preset+name)).
					Padding(4, 9).Radius(7).Border(1, pal.controlBorder)
				if useBtn.Hovered() {
					useBtn.Background(pal.rowHoverBg)
				}
				useBtn.Children(func() {
					ui.Text(c, "设为默认").FontSize(11.5).TextColor(pal.textPrimary)
				})
				if useBtn.Clicked() {
					a.setDefaultProvider(pc)
				}
			}

			// 展开配置
			toggle := ui.ButtonBase(c.Key("prov-toggle-"+pc.Preset+name)).
				Size(28, 28).Radius(8).Center().Label("配置")
			if toggle.Hovered() || isOpen {
				toggle.Background(pal.rowHoverBg)
			}
			toggle.Children(func() {
				ui.Icon(c, chevronsUpDownSVG).FontSize(13).TextColor(pal.textSecondary)
			})
			if toggle.Clicked() {
				if isOpen {
					a.openProvider = ""
				} else {
					a.openProvider = pc.Preset + name
					a.apiKeyDraft = pc.APIKey
					a.baseURLDraft = pc.ChatBase
					a.modelDraft = pc.DefaultModel
					a.testResult = ""
				}
			}
		})

		if isOpen {
			ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)
			a.renderProviderEditor(c, idx, pal, preset)
		}
	})
}

func protocolLabel(k llm.Kind) string {
	if k == llm.AnthropicMessages {
		return "Anthropic Messages"
	}
	return "OpenAI 兼容"
}

// renderProviderEditor 是展开后的编辑区。
func (a *app) renderProviderEditor(c *ui.Context, idx int, pal palette, preset providers.Preset) {
	pc := a.cfgNow.Providers[idx]

	ui.Column(c).FillWidth().Padding(16, 18).Gap(12).Children(func() {
		// API key
		ui.Column(c).FillWidth().Gap(5).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "API Key").FontSize(12).Bold().TextColor(pal.textPrimary)
				ui.Spacer(c)
				if preset.KeysURL != "" {
					ui.Text(c, "去申请 →").FontSize(11).TextColor(pal.textSecondary)
				}
			})
			input := ui.TextInput(c.Key("key-"+pc.Preset), &a.apiKeyDraft).
				FillWidth().
				Password().
				Placeholder("sk-...")
			_ = input
			if a.apiKeyDraft != pc.APIKey {
				a.saveProviderField(idx, func(p *config.ProviderConfig) { p.APIKey = a.apiKeyDraft })
			}
		})

		// 档位：区域 / 套餐 / 按量付费
		if regions := providers.Endpoints(preset); len(regions) > 1 {
			label := preset.RegionLabel
			if label == "" {
				label = "端点"
			}
			ui.Column(c).FillWidth().Gap(5).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, label).FontSize(12).Bold().TextColor(pal.textPrimary)
					ui.Spacer(c)
					ui.Text(c, "每档有自己的地址，key 的适用范围也不同").
						FontSize(10.5).TextColor(pal.textSecondary.Alpha(0.8))
				})
				ui.Row(c).FillWidth().Gap(5).Wrap().Children(func() {
					for _, r := range regions {
						r := r
						selected := pc.Region == r.ID || (pc.Region == "" && len(regions) > 0 && r.ID == regions[0].ID)
						chip := ui.ButtonBase(c.Key("region-"+pc.Preset+"-"+r.ID)).
							Padding(3, 9).Radius(7).
							Border(1, pal.controlBorder)
						if selected {
							chip.Background(pal.switchOn.Alpha(0.14)).Border(1, pal.switchOn.Alpha(0.5))
						} else {
							chip.Background(pal.cardBg)
						}
						if chip.Hovered() && !selected {
							chip.Background(pal.rowHoverBg)
						}
						chip.Children(func() {
							color := pal.textPrimary
							if selected {
								color = pal.switchOn
							}
							ui.Text(c, r.Name).FontSize(11).TextColor(color)
						})
						if chip.Clicked() && !selected {
							a.saveProviderField(idx, func(p *config.ProviderConfig) {
								p.Region = r.ID
								// 换档位就把手动覆盖的端点清掉，否则会
								// 出现「选了新区域但还在打旧地址」。
								p.ChatBase = ""
								p.AnthropicBase = ""
								if len(r.Models) > 0 {
									p.DefaultModel = r.Models[0]
								}
							})
							a.modelDraft = a.cfgNow.Providers[idx].DefaultModel
							a.baseURLDraft = ""
						}
					}
				})
			})
		}

		// Base URL（可选）
		ui.Column(c).FillWidth().Gap(5).Children(func() {
			ui.Text(c, "接口地址").FontSize(12).Bold().TextColor(pal.textPrimary)
			ui.Text(c, "留空则用 "+firstURL(preset.Chat, preset.Anthropic)+"；接中转站或自建网关时填这里。").
				FontSize(11).TextColor(pal.textSecondary).LineHeight(1.35)
			ui.TextInput(c.Key("base-"+pc.Preset), &a.baseURLDraft).
				FillWidth().
				Placeholder(preset.Chat)
			if a.baseURLDraft != pc.ChatBase {
				a.saveProviderField(idx, func(p *config.ProviderConfig) { p.ChatBase = a.baseURLDraft })
			}
		})

		// 模型
		ui.Column(c).FillWidth().Gap(5).Children(func() {
			ui.Text(c, "模型").FontSize(12).Bold().TextColor(pal.textPrimary)
			ui.TextInput(c.Key("model-"+pc.Preset), &a.modelDraft).
				FillWidth().
				Placeholder("模型 id，例如 deepseek-v4.1-flash")
			if a.modelDraft != pc.DefaultModel {
				a.saveProviderField(idx, func(p *config.ProviderConfig) { p.DefaultModel = a.modelDraft })
			}
			// 预设里没有模型列表的（大多数官方 API 都没有），
			// 给一个按钮真去问对方要。这比在本地维护一份必然过期的
			// 硬编码列表靠谱得多。
			candidates := preset.Models
			regions := providers.Endpoints(preset)
			activeID := pc.Region
			if activeID == "" && len(regions) > 0 {
				activeID = regions[0].ID
			}
			for _, r := range regions {
				if r.ID == activeID && len(r.Models) > 0 {
					candidates = r.Models
					break
				}
			}
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				fetchBtn := ui.ButtonBase(c.Key("fetch-models-"+pc.Preset)).
					Padding(3, 9).Radius(6).Border(1, pal.controlBorder)
				if fetchBtn.Hovered() {
					fetchBtn.Background(pal.rowHoverBg)
				}
				fetchBtn.Children(func() {
					if a.fetchingModels {
						ui.Spinner(c).Size(10, 10)
						ui.Text(c, " 拉取中…").FontSize(10.5).TextColor(pal.textSecondary)
					} else {
						ui.Text(c, "从接口拉取模型列表").FontSize(10.5).TextColor(pal.textPrimary)
					}
				})
				if fetchBtn.Clicked() && !a.fetchingModels {
					a.fetchModels(idx)
				}
				if a.fetchMsg != "" {
					ui.Text(c, a.fetchMsg).FontSize(10.5).TextColor(pal.textSecondary).SingleLine().Grow(1)
				}
			})

			if len(candidates) > 0 {
				ui.Row(c).FillWidth().Gap(5).Wrap().Children(func() {
					for _, m := range candidates {
						chip := ui.ButtonBase(c.Key("m-"+pc.Preset+"-"+m)).
							Padding(3, 8).Radius(6).Background(pal.codeBg)
						if chip.Hovered() {
							chip.Background(pal.rowHoverBg)
						}
						chip.Children(func() {
							ui.Text(c, m).FontSize(11).TextColor(pal.codeFg)
						})
						if chip.Clicked() {
							a.modelDraft = m
							a.saveProviderField(idx, func(p *config.ProviderConfig) { p.DefaultModel = m })
						}
					}
				})
			}
		})

		// 订阅额度
		if preset.Plan != nil {
			a.renderPlanInfo(c, pal, preset)
		}

		// 测试 + 删除
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			testBtn := ui.ButtonBase(c.Key("prov-test-"+pc.Preset)).
				Padding(5, 12).Radius(7).Background(pal.rowHoverBg).Border(1, pal.controlBorder)
			if testBtn.Hovered() {
				testBtn.Background(pal.cardBorder)
			}
			testBtn.Children(func() {
				if a.testing {
					ui.Spinner(c).Size(12, 12)
					ui.Text(c, " 测试中…").FontSize(11.5).TextColor(pal.textPrimary)
				} else {
					ui.Text(c, "测试连接").FontSize(11.5).Bold().TextColor(pal.textPrimary)
				}
			})
			if testBtn.Clicked() && !a.testing {
				a.testConnection(idx)
			}

			if a.testResult != "" {
				color := pal.textSecondary
				if a.testOK {
					color = pal.switchOn
				} else {
					color = ui.Hex("#DC2626")
				}
				ui.Text(c, a.testResult).FontSize(11).TextColor(color).SingleLine().Grow(1)
			}

			ui.Spacer(c)

			delBtn := ui.ButtonBase(c.Key("prov-del-"+pc.Preset)).
				Padding(5, 10).Radius(7)
			if delBtn.Hovered() {
				delBtn.Background(pal.rowHoverBg)
			}
			delBtn.Children(func() {
				ui.Text(c, "移除").FontSize(11.5).TextColor(ui.Hex("#DC2626"))
			})
			if delBtn.Clicked() {
				a.removeProvider(idx)
			}
		})
	})
}

// renderPlanInfo 显示订阅套餐和能算出来的用量。
func (a *app) renderPlanInfo(c *ui.Context, pal palette, preset providers.Preset) {
	plan := preset.Plan
	quota, hasQuota := providers.LocalUsage(preset.ID)

	ui.Column(c).
		FillWidth().
		Padding(10, 12).
		Gap(5).
		Radius(9).
		Background(pal.codeBg).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, plan.Name).FontSize(11.5).Bold().TextColor(pal.textPrimary)
				if plan.WindowHours > 0 {
					ui.Text(c, fmt.Sprintf("%.0f 小时窗口", plan.WindowHours)).
						FontSize(10.5).TextColor(pal.textSecondary)
				}
				ui.Spacer(c)
				if plan.PriceUSD > 0 {
					ui.Text(c, fmt.Sprintf("$%.0f/月", plan.PriceUSD)).FontSize(11).TextColor(pal.textSecondary)
				}
			})
			if plan.Notes != "" {
				ui.Text(c, plan.Notes).FontSize(11).TextColor(pal.textSecondary).LineHeight(1.35)
			}
			if hasQuota {
				ui.Text(c, fmt.Sprintf("本窗口已用 %s tokens%s",
					humanInt(quota.Used),
					func() string {
						if quota.ResetAt.IsZero() {
							return ""
						}
						return " · 约 " + quota.ResetAt.Format("15:04") + " 重置"
					}())).
					FontSize(11).
					TextColor(pal.textPrimary).
					FontFeatures("tnum")
				ui.Text(c, quota.Notes).FontSize(10.5).TextColor(pal.textSecondary)
			}
		})
}

// humanInt 把大数字写成 1.2M / 345K。
func humanInt(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func firstURL(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return "（未设置）"
}

// renderLocalImport 画「从本机 CLI 导入」。
func (a *app) renderLocalImport(c *ui.Context, pal palette) {
	if !a.detectDone {
		a.detected = providers.DetectLocal()
		a.detectDone = true
	}

	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, "从本机导入").FontSize(13).Bold().TextColor(pal.textPrimary)
				ui.Text(c, "已经登录过 Claude Code 或 Codex 的话，可以直接复用那份凭据，不用再申请 key。").
					FontSize(11.8).TextColor(pal.textSecondary).LineHeight(1.38)
			})

			if len(a.detected) == 0 {
				ui.Text(c, "没有在本机找到已登录的 CLI。").
					FontSize(11.5).TextColor(pal.textSecondary.Alpha(0.75))
				return
			}

			for _, cred := range a.detected {
				cred := cred
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, cred.Label).FontSize(12.2).Bold().TextColor(pal.textPrimary)
						detail := cred.Kind
						if cred.Kind == "oauth" && !cred.ExpiresAt.IsZero() {
							detail = fmt.Sprintf("订阅凭据 · %s 到期", cred.ExpiresAt.Format("01-02 15:04"))
						}
						ui.Text(c, detail).FontSize(11).TextColor(pal.textSecondary)
					})
					btn := ui.ButtonBase(c.Key("cred-import-"+cred.Source)).
						Padding(4, 10).Radius(7).Background(pal.rowHoverBg).Border(1, pal.controlBorder)
					if btn.Hovered() {
						btn.Background(pal.cardBorder)
					}
					btn.Children(func() {
						ui.Text(c, "导入").FontSize(11.5).Bold().TextColor(pal.textPrimary)
					})
					if btn.Clicked() {
						a.importCredential(cred)
					}
				})
			}
		})
	})
}

// importCredential 把扫到的凭据存成一个供应商配置。
func (a *app) importCredential(cred providers.Credential) {
	presetID := "anthropic"
	model := "claude-sonnet-5-5"
	if cred.Source == "codex" {
		if cred.Kind == "api_key" {
			presetID = "openai"
		} else {
			// ChatGPT 订阅走的是 Responses API，Konseki 这边按
			// OpenAI 兼容处理，模型名用 codex 系列。
			presetID = "openai"
		}
		model = "gpt-5.2"
	}
	preset, _ := providers.ByID(presetID)

	pc := config.ProviderConfig{
		Preset:        presetID,
		Label:         cred.Label,
		ChatBase:      preset.Chat,
		AnthropicBase: preset.Anthropic,
		Auth:          cred.Kind,
		AccessToken:   cred.AccessToken,
		RefreshToken:  cred.RefreshToken,
		AccountID:     cred.AccountID,
		ExpiresAt:     cred.ExpiresAt,
		DefaultModel:  model,
	}
	if cred.Kind == "api_key" {
		pc.APIKey = cred.AccessToken
	}

	a.cfg.Update(func(c *config.Config) {
		// 同样来源的重复导入就替换，不要堆一堆。
		for i := range c.Providers {
			if c.Providers[i].Label == pc.Label {
				c.Providers[i] = pc
				return
			}
		}
		c.Providers = append(c.Providers, pc)
	})
	a.cfgNow = a.cfg.Get()
	a.rebuildGenerator()
	a.testResult = "已导入 " + cred.Label
	a.testOK = true
}

// renderProviderPicker 是「添加供应商」的网格。
func (a *app) renderProviderPicker(c *ui.Context, pal palette) {
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "添加供应商").FontSize(13).Bold().TextColor(pal.textPrimary)
				ui.Spacer(c)
				// 筛选
				seg := ui.SegmentedBase(c.Key("prov-filter"), &a.providerFilter, len(providerFilterNames))
				seg.Track.Padding(2).Radius(7).Background(pal.rowHoverBg).Children(func() {
					for i, name := range providerFilterNames {
						sg := seg.Segment(i).Padding(3, 7).Radius(5)
						if i == a.providerFilter {
							sg.Background(pal.cardBg)
						}
						sg.Children(func() {
							ui.Text(c, name).FontSize(10.5).TextColor(pal.textPrimary)
						})
					}
				})
			})

			// 三列网格，和 magpie 的观感一致
			all := providers.All()
			var filtered []providers.Preset
			for _, p := range all {
				if a.providerFilter > 0 && kindLabel(p.Kind) != providerFilterNames[a.providerFilter] {
					continue
				}
				filtered = append(filtered, p)
			}

			cols := 3
			for i := 0; i < len(filtered); i += cols {
				ui.Row(c).FillWidth().Gap(6).Children(func() {
					for j := 0; j < cols; j++ {
						if i+j >= len(filtered) {
							ui.Box(c).Grow(1)
							continue
						}
						a.renderProviderTile(c, filtered[i+j], pal)
					}
				})
			}
		})
	})
}

// renderProviderTile 是网格里的一个方块。
func (a *app) renderProviderTile(c *ui.Context, p providers.Preset, pal palette) {
	added := false
	for _, pc := range a.cfgNow.Providers {
		if pc.Preset == p.ID && pc.Label == "" {
			added = true
			break
		}
	}

	tile := ui.ButtonBase(c.Key("prov-tile-"+p.ID)).
		Grow(1).
		Padding(9, 10).
		Radius(8).
		Gap(0)
	if tile.Hovered() {
		tile.Background(pal.rowHoverBg)
	}
	tile.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, providerGlyph(p)).FontSize(13).TextColor(pal.textPrimary)
			ui.Column(c).Grow(1).Gap(1).Children(func() {
				ui.Text(c, p.Name).FontSize(11.8).TextColor(pal.textPrimary).SingleLine()
				if p.Note != "" {
					ui.Text(c, p.Note).FontSize(9.8).TextColor(pal.textSecondary.Alpha(0.85)).SingleLine()
				}
			})
			if added {
				ui.Icon(c, checkSVG).FontSize(11).TextColor(pal.switchOn)
			}
		})
	})
	if tile.Clicked() && !added {
		a.addProvider(p)
	}
}

// providerGlyph 用首字母当图标。
//
// 不用真图标：33 家厂商的 brand mark 打包进来会让 App 体积翻倍，
// 而一个首字母方块在这个尺寸下反而更清楚。
func providerGlyph(p providers.Preset) string {
	name := p.Short
	if name == "" {
		name = p.Name
	}
	for _, r := range name {
		if r != ' ' {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

// ---------- 供应商 CRUD ----------

func (a *app) addProvider(p providers.Preset) {
	// 有档位的家默认落在第一档，并把该档的地址和模型一起带进来 ——
	// 只存预设的根地址而 region 留空，会让 Resolve 又要回退一次，
	// 用户看到的和实际请求的不一致。
	eps := providers.Endpoints(p)
	region := ""
	model := ""
	chat := p.Chat
	anthropic := p.Anthropic
	if len(eps) > 0 {
		region = eps[0].ID
		chat = eps[0].Chat
		anthropic = eps[0].Anthropic
		if len(eps[0].Models) > 0 {
			model = eps[0].Models[0]
		}
	}
	if model == "" && len(p.Models) > 0 {
		model = p.Models[0]
	}
	pc := config.ProviderConfig{
		Preset:        p.ID,
		Region:        region,
		ChatBase:      chat,
		AnthropicBase: anthropic,
		DefaultModel:  model,
	}
	a.cfg.Update(func(c *config.Config) { c.Providers = append(c.Providers, pc) })
	a.cfgNow = a.cfg.Get()
	a.rebuildGenerator()
	a.openProvider = pc.Preset + pc.Label
	a.apiKeyDraft = ""
	a.baseURLDraft = pc.ChatBase
	a.modelDraft = model
}

func (a *app) removeProvider(idx int) {
	if idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	preset := a.cfgNow.Providers[idx].Preset
	label := a.cfgNow.Providers[idx].Label
	a.cfg.Update(func(c *config.Config) {
		c.Providers = slices.DeleteFunc(c.Providers, func(p config.ProviderConfig) bool {
			return p.Preset == preset && p.Label == label
		})
		// 删掉的正好是默认供应商，就清空默认，让 UI 提示重新选。
		if c.Summary.ProviderID == preset {
			c.Summary.ProviderID = ""
			c.Summary.Model = ""
		}
	})
	a.cfgNow = a.cfg.Get()
	a.openProvider = ""
	a.rebuildGenerator()
}

func (a *app) saveProviderField(idx int, fn func(*config.ProviderConfig)) {
	if idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	preset := a.cfgNow.Providers[idx].Preset
	label := a.cfgNow.Providers[idx].Label
	a.cfg.Update(func(c *config.Config) {
		for i := range c.Providers {
			if c.Providers[i].Preset == preset && c.Providers[i].Label == label {
				fn(&c.Providers[i])
				return
			}
		}
	})
	a.cfgNow = a.cfg.Get()
}

// setDefaultProvider 把某家设成摘要用的模型。
func (a *app) setDefaultProvider(pc config.ProviderConfig) {
	a.cfg.Update(func(c *config.Config) {
		c.Summary.ProviderID = pc.Preset
		c.Summary.Model = pc.DefaultModel
	})
	a.cfgNow = a.cfg.Get()
	a.rebuildGenerator()
	a.testResult = "已设为默认摘要模型"
	a.testOK = true
}

// testConnection 发一个最小请求，确认 key/地址/模型都对得上。
