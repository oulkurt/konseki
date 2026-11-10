/*
 * [INPUT]: 依赖供应商配置、预设、模型解析、摘要生成器、凭据扫描和语言目录。
 * [OUTPUT]: 提供模型和推理档位选择、供应商分节配置、可用模型勾选、自动摘要开关行和隐私设置。
 * [POS]: 模型设置页的组合边界。网络操作由 provider_requests.go 执行。
 * 配置面板按鉴权、端点、模型、操作分节。订阅凭据不显示密钥输入框。
 * 候选模型取自 llm.Resolve，菜单里能选的就是请求能用的。
 * 勾选列表决定候选；摘要菜单只列勾选的模型。
 * 连接结果和拉取结果按当前语言显示。用户输入时才写供应商配置。
 * 未输入时，供应商输入框跟随配置。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"slices"
	"strings"
	"time"

	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) renderModelsSettings(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		provider := a.cfgNow.Summary.ProviderID
		for _, pc := range a.cfgNow.Providers {
			if pc.Preset == a.cfgNow.Summary.ProviderID {
				provider = providerDisplayName(pc)
				break
			}
		}
		ui.Column(c).FillWidth().Children(func() {
			a.settingsRow(c, msg.ModelsSummary(), provider, pal, func() {
				label := a.cfgNow.Summary.Model
				if label == "" {
					label = msg.ModelsChoose()
				}
				renderSelectPill(c, "summary-model", label, 130, pal.textPrimary, pal, func(m *ui.Menu) {
					for _, pc := range a.cfgNow.Providers {
						models := providerModelChoices(pc)
						if len(models) == 0 && pc.DefaultModel != "" {
							models = []string{pc.DefaultModel}
						}
						for _, model := range models {
							if m.Item(providerDisplayName(pc) + " · " + model).Checked(a.cfgNow.Summary.ProviderID == pc.Preset && a.cfgNow.Summary.Model == model).Chosen() {
								a.setSummaryModel(pc.Preset, model)
							}
						}
					}
				})
			})
			// 档位单独成行。菜单只显示「中」时，中文界面里看不出它是推理档位。
			a.settingsRow(c, msg.ModelsEffort(), msg.ModelsEffortHelp(), pal, func() {
				efforts := []struct{ key, label string }{{"off", msg.ModelsEffortOff()}, {"low", msg.ModelsEffortLow()}, {"medium", msg.ModelsEffortMedium()}, {"high", msg.ModelsEffortHigh()}, {"max", msg.ModelsEffortMax()}}
				effortLabel := msg.ModelsEffort()
				for _, effort := range efforts {
					if effort.key == a.cfgNow.Summary.ThinkingEffort {
						effortLabel = effort.label
					}
				}
				renderSelectPill(c, "summary-effort", effortLabel, 90, pal.textPrimary, pal, func(m *ui.Menu) {
					for _, effort := range efforts {
						if m.Item(effort.label).Checked(a.cfgNow.Summary.ThinkingEffort == effort.key).Chosen() {
							a.setThinking(effort.key)
						}
					}
				})
			})
		})
	})
	a.renderModelsProviders(c, pal)
	a.renderModelsImport(c, pal)
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			a.renderAutoSummaryRow(c, pal, "summary-auto")
			a.settingsRow(c, msg.ModelsRedact(), msg.ModelsRedactHelp(), pal, func() {
				on := a.cfgNow.Redact.Enabled
				if renderPillSwitch(c, "summary-redact", &on, pal) {
					a.cfg.Update(func(cfg *config.Config) { cfg.Redact.Enabled = on })
					a.cfgNow = a.cfg.Get()
					a.rebuildGenerator()
				}
			})
			if a.cfgNow.Redact.Enabled {
				for _, item := range []struct {
					key, label, help string
					value            bool
					set              func(*config.Config, bool)
				}{
					{"sw-redact-secrets", msg.ModelsRedactSecrets(), msg.ModelsRedactSecretsHelp(), a.cfgNow.Redact.Secrets, func(cfg *config.Config, v bool) { cfg.Redact.Secrets = v }},
					{"sw-redact-personal", msg.ModelsRedactPersonal(), msg.ModelsRedactPersonalHelp(), a.cfgNow.Redact.Personal, func(cfg *config.Config, v bool) { cfg.Redact.Personal = v }},
				} {
					a.settingsRow(c, item.label, item.help, pal, func() {
						v := item.value
						if renderPillSwitch(c, item.key, &v, pal) {
							a.cfg.Update(func(cfg *config.Config) { item.set(cfg, v) })
							a.cfgNow = a.cfg.Get()
							a.rebuildGenerator()
						}
					})
				}
			}
		})
	})
}

// renderAutoSummaryRow 绘制自动摘要开关。记录页和模型页各放一个，key 区分两处。
// 说明写出实际间隔，用户能看出活动多久发给模型一次。
func (a *app) renderAutoSummaryRow(c *ui.Context, pal palette, key string) {
	msg := locales.In(c.Locale())
	minutes := int(summaryInterval(a.cfgNow.Summary) / time.Minute)
	a.settingsRow(c, msg.ModelsAuto(), msg.ModelsAutoHelp(minutes), pal, func() {
		on := a.cfgNow.Summary.Auto
		if renderPillSwitch(c, key, &on, pal) {
			if err := a.cfg.Update(func(cfg *config.Config) { cfg.Summary.Auto = on }); err != nil {
				a.err = err
			}
			a.cfgNow = a.cfg.Get()
		}
	})
}

func (a *app) renderModelsProviders(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				headingText(c, msg.ModelsProviders()).Grow(1).SingleLine().TextColor(pal.textPrimary)
				if ui.Button(c.Key("provider-add"), msg.ModelsAdd()).Clicked() {
					a.addProviderOpen = true
				}
			})
			if len(a.cfgNow.Providers) == 0 {
				ui.Text(c, msg.ModelsEmpty()).FontSize(12).TextColor(pal.textSecondary)
			}
			// 「移除」在循环中途把列表改短，所以每轮重新读长度。
			// range 只在开头取一次长度，移除第一个以后会越界。
			for idx := 0; idx < len(a.cfgNow.Providers); idx++ {
				a.renderModelsProvider(c, pal, idx)
			}
		})
	})
	ui.Modal(c, &a.addProviderOpen, func() {
		ui.Column(c).Width(500).MaxWidth(700).Padding(18).Gap(10).Children(func() {
			headingText(c, msg.ModelsAdd()).SingleLine()
			ui.TextInput(c.Key("provider-search"), &a.providerSearch).FillWidth().Placeholder(msg.ModelsSearch()).Label(msg.ModelsSearch())
			ui.Scroll(c).Height(350).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for _, preset := range sortedPresets() {
						if !strings.Contains(strings.ToLower(preset.Name+" "+preset.ID), strings.ToLower(strings.TrimSpace(a.providerSearch))) {
							continue
						}
						item := ui.ButtonBase(c.Key("provider-choice-"+preset.ID)).FillWidth().Padding(8, 10).Radius(8)
						if item.Hovered() {
							item.Background(pal.rowHoverBg)
						}
						item.Children(func() {
							ui.Column(c).FillWidth().Gap(2).Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
									headingText(c, preset.Name).FontSize(13).SingleLine().Tooltip(preset.Name).TextColor(pal.textPrimary)
									providerTag(c, providerKindLabel(preset.Kind, msg), pal)
								})
								if preset.Note != "" {
									ui.Text(c, preset.Note).FontSize(11.5).TextColor(pal.textSecondary).SingleLine().Tooltip(preset.Note)
								}
							})
						})
						if item.Clicked() {
							a.addProvider(preset)
							a.addProviderOpen = false
							a.providerSearch = ""
						}
					}
				})
			})
			if ui.Button(c, msg.ModelsClose()).Clicked() {
				a.addProviderOpen = false
			}
		})
	})
}

// providerDisplayName 依次取用户起的名字、预设名、预设 ID。
func providerDisplayName(pc config.ProviderConfig) string {
	if pc.Label != "" {
		return pc.Label
	}
	if preset, ok := providers.ByID(pc.Preset); ok && preset.Name != "" {
		return preset.Name
	}
	return pc.Preset
}

// providerModelPreview 是可用模型默认显示的条数。
// 一次拉取可能带回几十个模型，全铺开会把面板撑得比整页还长。
const providerModelPreview = 8

// expiryDateLayout 是订阅到期日期的显示格式。固定 ISO 顺序，两种语言都不歧义。
const expiryDateLayout = "2006-01-02"

// providerKindLabel 把预设种类换成界面文案。未知种类不加标记。
func providerKindLabel(kind providers.Kind, msg locales.Messages) string {
	switch kind {
	case providers.KindVendor:
		return msg.ModelsKindVendor()
	case providers.KindRelay:
		return msg.ModelsKindRelay()
	case providers.KindLocal:
		return msg.ModelsKindLocal()
	case "remote":
		return msg.ModelsKindRemote()
	}
	return ""
}

// credentialKindLabel 把本机凭据的种类换成界面文案。
func credentialKindLabel(kind string, msg locales.Messages) string {
	switch kind {
	case "oauth":
		return msg.ModelsAuthSubscription()
	case "api_key":
		return msg.ModelsCredentialAPIKey()
	}
	return ""
}

// providerTag 绘制一枚小号标记。种类和鉴权方式共用同一视觉。
func providerTag(c *ui.Context, text string, pal palette) {
	if text == "" {
		return
	}
	ui.Row(c).AlignItems(ui.Center).Shrink(0).Padding(1, 7).Radius(999).
		Background(pal.rowHoverBg).Border(1, pal.cardBorder).Children(func() {
		ui.Text(c, text).FontSize(10.5).Bold().TextColor(pal.textSecondary).SingleLine()
	})
}

// providerSectionTitle 是小节标题：比正文小一号，靠颜色而不是字号分层。
func providerSectionTitle(c *ui.Context, title string, pal palette) {
	headingText(c, title).FontSize(11).TextColor(pal.textSecondary).
		LetterSpacing(0.3).SingleLine()
}

// providerDivider 在配置面板的小节之间画一条分隔线。
func providerDivider(c *ui.Context, pal palette) {
	ui.Box(c).FillWidth().Height(1).Background(pal.rowDivider)
}

// providerKindRank 决定添加弹窗里的分组顺序：官方、中转、本地、其余。
func providerKindRank(kind providers.Kind) int {
	switch kind {
	case providers.KindVendor:
		return 0
	case providers.KindRelay:
		return 1
	case providers.KindLocal:
		return 2
	}
	return 3
}

// sortedPresets 按种类分组列出预设，组内保持目录顺序。
// 目录末尾追加的自有预设（gemini-cli）也要回到官方组里。
func sortedPresets() []providers.Preset {
	out := slices.Clone(providers.All())
	slices.SortStableFunc(out, func(a, b providers.Preset) int {
		return providerKindRank(a.Kind) - providerKindRank(b.Kind)
	})
	return out
}

// providerModelChoices 返回摘要菜单可选的模型。直接用 llm.Resolve 的候选列表，
// 保证菜单里能选的模型就是请求实际能用的。
func providerModelChoices(pc config.ProviderConfig) []string {
	resolved, _ := llm.Resolve(pc, false)
	return resolved.Models
}

// providerModelList 返回勾选列表要显示的全部模型。
// 发现清单在前：它的顺序稳定，取消勾选不会让行在列表里跳位。
// 解析候选补上清单之外的预设模型，用户仍能勾选它们。
func providerModelList(pc config.ProviderConfig) []string {
	resolved := providerModelChoices(pc)
	out := make([]string, 0, len(pc.DiscoveredModels)+len(resolved))
	seen := make(map[string]bool, len(pc.DiscoveredModels)+len(resolved))
	for _, list := range [][]string{pc.DiscoveredModels, resolved} {
		for _, id := range list {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// hasModelSelection 报告配置是否已经进入勾选模式（有发现清单）。
// 没有发现清单的旧配置按预设全集解析，勾选态就是全选。
func hasModelSelection(pc config.ProviderConfig) bool {
	return len(pc.DiscoveredModels) > 0
}

// providerModelChecked 报告一枚模型在勾选列表里是否打勾。
func providerModelChecked(pc config.ProviderConfig, id string) bool {
	return !hasModelSelection(pc) || slices.Contains(pc.EnabledModels, id)
}

// providerEnabledModels 把勾选状态展开成完整列表，顺序跟随 choices。
// 旧配置没有发现清单时全部勾选：列表本身就是 llm.Resolve 的候选，
// 摘要菜单里能选的正是这些，勾选态必须和菜单一致。
func providerEnabledModels(pc config.ProviderConfig, choices []string) []string {
	if !hasModelSelection(pc) {
		return slices.Clone(choices)
	}
	enabled := make(map[string]bool, len(pc.EnabledModels))
	for _, id := range pc.EnabledModels {
		enabled[id] = true
	}
	out := make([]string, 0, len(choices))
	for _, id := range choices {
		if enabled[id] {
			out = append(out, id)
		}
	}
	return out
}

// providerKeysURL 优先用当前档位的密钥页面，没填就落回预设。
func providerKeysURL(pc config.ProviderConfig, preset providers.Preset) string {
	for _, ep := range providers.Endpoints(preset) {
		if ep.ID == pc.Region && ep.KeysURL != "" {
			return ep.KeysURL
		}
	}
	return preset.KeysURL
}

func (a *app) renderModelsProvider(c *ui.Context, pal palette, idx int) {
	msg := locales.In(c.Locale())
	pc := a.cfgNow.Providers[idx]
	preset, _ := providers.ByID(pc.Preset)
	name := providerDisplayName(pc)
	oauth := pc.Auth == "oauth"
	key := providerKey(pc)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			headingText(c, name).Grow(1).SingleLine().Tooltip(name).TextColor(pal.textPrimary)
			if ui.Button(c.Key("provider-default-"+pc.Preset), msg.ModelsDefault()).Disabled(a.cfgNow.Summary.ProviderID == pc.Preset).Clicked() {
				a.setDefaultProvider(pc)
			}
			if ui.Button(c.Key("provider-configure-"+pc.Preset), msg.ModelsConfigure()).Clicked() {
				if a.openProvider == key {
					a.openProvider = ""
				} else {
					a.openProvider = key
					a.apiKeyDraft = pc.APIKey
					a.baseURLDraft = pc.ChatBase
					a.responsesDraft = pc.ResponsesBase
					a.modelDraft = pc.DefaultModel
					a.modelsAll = false
				}
			}
		})
		if a.openProvider != key {
			return
		}
		ui.Column(c).FillWidth().Padding(12).Gap(12).Radius(10).
			Background(pal.rowExpandedBg).Border(1, pal.rowDivider).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				providerSectionTitle(c, msg.ModelsSectionAuth(), pal)
				if oauth {
					a.renderProviderSubscription(c, pal, idx, pc, msg, name)
				} else {
					a.renderProviderKey(c, pal, idx, pc, preset, msg)
				}
			})
			// 订阅端点由 Resolve 写死，可编辑的地址会误导用户。
			if !oauth {
				providerDivider(c, pal)
				a.renderProviderEndpoint(c, pal, idx, pc, preset, msg)
			}
			providerDivider(c, pal)
			a.renderProviderModels(c, pal, idx, pc, preset, msg, oauth)
			providerDivider(c, pal)
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				providerSectionTitle(c, msg.ModelsSectionActions(), pal)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if !oauth {
						label := msg.ModelsTest()
						if a.testing {
							label = msg.ModelsTesting()
						}
						if ui.Button(c.Key("provider-test-"+pc.Preset), label).Disabled(a.testing).Clicked() {
							a.testConnection(idx)
						}
					}
					if ui.Button(c.Key("provider-remove-"+pc.Preset), msg.ModelsRemove()).Clicked() {
						a.removeProvider(idx)
					}
				})
				if !oauth {
					a.renderProviderTestResult(c, pal)
				}
			})
		})
	})
}

// renderProviderSubscription 绘制订阅凭据。OAuth 没有 key 可填，
// 测试连接放在凭据行下面，操作节只留移除。
func (a *app) renderProviderSubscription(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, msg locales.Messages, name string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		providerTag(c, msg.ModelsAuthSubscription(), pal)
		ui.Text(c, name).FontSize(12.5).TextColor(pal.textPrimary).Grow(1).SingleLine().Tooltip(name)
		if !pc.ExpiresAt.IsZero() {
			label, color := msg.ModelsAuthExpires(pc.ExpiresAt.Format(expiryDateLayout)), pal.textSecondary
			if a.currentTime().After(pc.ExpiresAt) {
				label, color = msg.ModelsAuthExpired(), c.Theme().Danger
			}
			numText(c, label).FontSize(12).TextColor(color).SingleLine()
		}
	})
	ui.Text(c, msg.ModelsEndpointSubscription()).FontSize(11.5).TextColor(pal.textSecondary).LineHeight(1.35)
	label := msg.ModelsTest()
	if a.testing {
		label = msg.ModelsTesting()
	}
	ui.Row(c).Children(func() {
		if ui.Button(c.Key("provider-test-"+pc.Preset), label).Disabled(a.testing).Clicked() {
			a.testConnection(idx)
		}
	})
	a.renderProviderTestResult(c, pal)
}

// renderProviderKey 绘制按量计费的密钥输入。KeyHint 和取 key 的链接
// 都是预设里的死数据，这里把它们接上。
func (a *app) renderProviderKey(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, preset providers.Preset, msg locales.Messages) {
	ui.Text(c, msg.ModelsKey()).FontSize(12).TextColor(pal.textPrimary)
	keyIn := ui.TextInput(c.Key("key-"+pc.Preset), &a.apiKeyDraft).FillWidth().Password().Label(msg.ModelsKey())
	a.syncProviderDraft(idx, keyIn.Focused(), &a.apiKeyDraft, pc.APIKey, func(p *config.ProviderConfig, v string) { p.APIKey = v })
	if preset.KeyHint != "" {
		ui.Text(c, preset.KeyHint).FontSize(11.5).TextColor(pal.textSecondary).LineHeight(1.35)
	}
	if url := providerKeysURL(pc, preset); url != "" {
		ui.Row(c).Children(func() {
			if ui.Button(c.Key("provider-keys-"+pc.Preset), msg.ModelsGetKey()).Clicked() && a.openExternal != nil {
				if err := a.openExternal(url); err != nil {
					a.err = err
				}
			}
		})
	}
}

// renderProviderEndpoint 绘制档位和地址。端点是预设的模板时用占位符
// 展示，用户填自己的资源地址（Azure）。
func (a *app) renderProviderEndpoint(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, preset providers.Preset, msg locales.Messages) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		providerSectionTitle(c, msg.ModelsSectionEndpoint(), pal)
		if eps := providers.Endpoints(preset); len(eps) > 1 {
			ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
				for _, ep := range eps {
					active := pc.Region == ep.ID || (pc.Region == "" && ep.ID == eps[0].ID)
					btn := ui.Button(c.Key("provider-region-"+pc.Preset+"-"+ep.ID), ep.Name)
					if active {
						btn.Background(pal.switchOn).TextColor(ui.Hex("#FFFFFF"))
					}
					if btn.Clicked() {
						a.saveProviderField(idx, func(p *config.ProviderConfig) {
							p.Region = ep.ID
							p.ChatBase = ep.Chat
							p.AnthropicBase = ep.Anthropic
							if len(ep.Models) > 0 {
								p.DefaultModel = ep.Models[0]
							}
						})
						a.rebuildGenerator()
					}
				}
			})
		}
		ui.Text(c, msg.ModelsURL()).FontSize(12).TextColor(pal.textPrimary)
		baseIn := ui.TextInput(c.Key("base-"+pc.Preset), &a.baseURLDraft).FillWidth().
			Placeholder(preset.Endpoint).Label(msg.ModelsURL())
		a.syncProviderDraft(idx, baseIn.Focused(), &a.baseURLDraft, pc.ChatBase, func(p *config.ProviderConfig, v string) { p.ChatBase = v })
		if preset.Responses != "" {
			ui.Text(c, msg.ModelsResponsesURL()).FontSize(12).TextColor(pal.textPrimary)
			resIn := ui.TextInput(c.Key("responses-"+pc.Preset), &a.responsesDraft).FillWidth().
				Placeholder(preset.Responses).Label(msg.ModelsResponsesURL())
			a.syncProviderDraft(idx, resIn.Focused(), &a.responsesDraft, pc.ResponsesBase, func(p *config.ProviderConfig, v string) { p.ResponsesBase = v })
		}
		if preset.EndpointHint != "" {
			ui.Text(c, preset.EndpointHint).FontSize(11.5).TextColor(pal.textSecondary).LineHeight(1.35)
		}
	})
}

// renderProviderModels 绘制默认模型、拉取按钮和可用模型勾选列表。
// 拉取写回发现清单并默认全选；勾选列表决定摘要菜单的候选。
func (a *app) renderProviderModels(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, preset providers.Preset, msg locales.Messages, oauth bool) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		providerSectionTitle(c, msg.ModelsSectionModel(), pal)
		ui.Text(c, msg.ModelsModel()).FontSize(12).TextColor(pal.textPrimary)
		modelIn := ui.TextInput(c.Key("model-"+pc.Preset), &a.modelDraft).FillWidth().Label(msg.ModelsModel())
		if a.syncProviderDraft(idx, modelIn.Focused(), &a.modelDraft, pc.DefaultModel, func(p *config.ProviderConfig, v string) { p.DefaultModel = v }) {
			a.rebuildGenerator()
		}
		key := providerKey(pc)
		// 订阅端点没有通用的 /models：Codex 导入时已带模型目录；
		// Gemini CLI 的 Code Assist 有列表接口，仍可拉取。
		showFetch := !preset.NoList && (!oauth || pc.Preset == "gemini-cli")
		if showFetch {
			label := msg.ModelsFetch()
			if a.fetchingModels && a.fetchFor == key {
				label = msg.ModelsFetching()
			}
			ui.Row(c).Children(func() {
				if ui.Button(c.Key("provider-fetch-"+pc.Preset), label).Disabled(a.fetchingModels).Clicked() {
					a.fetchModels(idx)
				}
			})
		}
		if a.fetchMsg != "" && a.fetchFor == key {
			ui.Text(c, a.fetchMsg).FontSize(11.5).TextColor(pal.textSecondary).LineHeight(1.35)
		}
		// 没有 /models 可拉的家（NoList）靠预设和档位的列表，
		// 有列表就显示，用户不必手打模型 id。
		if choices := providerModelList(pc); len(choices) > 0 {
			a.renderModelChoices(c, pal, idx, pc, msg, choices)
		}
	})
}

// renderModelChoices 绘制可用模型勾选列表。
// 勾选决定哪些模型进入摘要菜单；行尾圆圈把勾选的某个模型设为默认。
// 旧的「点一枚即默认」做不到只选一部分：勾选和默认是两件事，
// 模型多时用户先圈定范围，再在范围里挑一个当默认。
func (a *app) renderModelChoices(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, msg locales.Messages, choices []string) {
	enabled := providerEnabledModels(pc, choices)
	allChecked := len(choices) > 0 && len(enabled) == len(choices)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
		headingText(c, msg.ModelsAvailable()).FontSize(11.5).TextColor(pal.textSecondary).SingleLine()
		ui.Spacer(c)
		if !a.modelsAll && len(choices) > providerModelPreview {
			if ui.Button(c.Key("provider-more-"+pc.Preset), msg.ModelsMore(len(choices)-providerModelPreview)).Clicked() {
				a.modelsAll = true
			}
		}
		label := msg.ModelsSelectAll()
		if allChecked {
			label = msg.ModelsDeselectAll()
		}
		if ui.Button(c.Key("provider-select-all-"+pc.Preset), label).Clicked() {
			a.selectProviderModels(idx, pc, choices, !allChecked)
		}
	})
	shown := choices
	if !a.modelsAll && len(shown) > providerModelPreview {
		shown = shown[:providerModelPreview]
	}
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		for _, id := range shown {
			a.renderModelChoice(c, pal, idx, pc, msg, choices, id)
		}
	})
	ui.Text(c, msg.ModelsAvailableHint()).FontSize(11).TextColor(pal.textSecondary)
}

// renderModelChoice 绘制一枚模型：复选框管勾选，行尾圆圈管默认。
// mygo 的 Checkbox 整行（含模型名）点击即翻勾选，所以「设为默认」
// 不能挂在文字上，只能另给一个目标。
func (a *app) renderModelChoice(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, msg locales.Messages, choices []string, id string) {
	checked := providerModelChecked(pc, id)
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(3, 6).Radius(6)
	row.Children(func() {
		cb := ui.CheckboxBase(c.Key("provider-model-"+pc.Preset+"-"+id), &checked).Gap(7)
		cb.Label(id)
		cb.Children(func() {
			box := ui.Box(c).Size(15, 15).Radius(4).Shrink(0)
			if checked {
				box.Background(pal.switchOn)
			} else {
				box.Background(pal.cardBg).Border(1.5, pal.controlBorder)
			}
			box.DrawOver(func(p *ui.Painter, r ui.Rect) {
				if checked {
					p.StrokePath(modelCheckPath(r), 1.6, ui.Hex("#FFFFFF"))
				}
			})
			ui.Text(c, id).FontSize(11.5).TextColor(pal.textPrimary).SingleLine().Tooltip(id)
		})
		if cb.Hovered() {
			row.Background(pal.rowHoverBg)
		}
		ui.Spacer(c)
		if !checked {
			// 未勾选的模型不参与摘要，设成默认没有意义。
		} else if id == pc.DefaultModel {
			providerTag(c, msg.ModelsDefaultBadge(), pal)
		} else if renderDefaultChoice(c, "provider-model-default-"+pc.Preset+"-"+id, msg.ModelsDefault()+" "+id, pal) {
			a.saveProviderField(idx, func(p *config.ProviderConfig) { p.DefaultModel = id })
			a.modelDraft = id
			a.rebuildGenerator()
		}
		if cb.Changed() {
			a.toggleProviderModel(idx, pc, choices, id, checked)
		}
	})
}

// modelCheckPath 是勾选框里的对勾，坐标按框内尺寸取比例。
func modelCheckPath(r ui.Rect) *ui.Path {
	var p ui.Path
	return p.MoveTo(r.X+r.W*0.24, r.Y+r.H*0.52).
		LineTo(r.X+r.W*0.43, r.Y+r.H*0.71).
		LineTo(r.X+r.W*0.77, r.Y+r.H*0.32)
}

// renderDefaultChoice 绘制「设为默认」的空心圆圈，返回 true 表示这一帧点了它。
// 用圆圈而不是每行一个文字按钮：列表可能很长，八行「设为默认」会把右侧塞满。
func renderDefaultChoice(c *ui.Context, key, label string, pal palette) bool {
	btn := ui.ButtonBase(c.Key(key)).Size(16, 16).Radius(8).Cursor(ui.CursorPointer)
	btn.Label(label)
	hovered := btn.Hovered()
	btn.Draw(func(p *ui.Painter, r ui.Rect) {
		color := pal.controlBorder
		if hovered {
			color = pal.switchOn
		}
		p.Stroke(r, color, 8, 1.5)
	})
	return btn.Clicked()
}

// toggleProviderModel 翻一枚模型的勾选并写回配置。
func (a *app) toggleProviderModel(idx int, pc config.ProviderConfig, choices []string, id string, checked bool) {
	keep := make(map[string]bool, len(choices))
	for _, m := range providerEnabledModels(pc, choices) {
		keep[m] = true
	}
	if checked {
		keep[id] = true
	} else {
		delete(keep, id)
	}
	enabled := make([]string, 0, len(choices))
	for _, m := range choices {
		if keep[m] {
			enabled = append(enabled, m)
		}
	}
	a.saveProviderModels(idx, choices, enabled, id)
}

// selectProviderModels 全选或全不选。
func (a *app) selectProviderModels(idx int, pc config.ProviderConfig, choices []string, all bool) {
	var enabled []string
	if all {
		enabled = slices.Clone(choices)
	}
	a.saveProviderModels(idx, choices, enabled, "")
}

// saveProviderModels 写回勾选列表。
// 第一次勾选时把当前列表固化成发现清单：「空 = 全用预设」只属于没动过的
// 旧配置；用户一动手，勾选就是唯一依据，否则取消勾选的预设模型会被并集顶回来。
// 默认模型只从勾选的模型里选：取消当前默认时换到仍勾选的第一个，全取消就清空；
// 用户手打的默认模型不在本列表里时不动它。
func (a *app) saveProviderModels(idx int, choices, enabled []string, preferred string) {
	if idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	pc := a.cfgNow.Providers[idx]
	next := pc.DefaultModel
	if next == "" && len(enabled) > 0 {
		next = enabled[0]
		if slices.Contains(enabled, preferred) {
			next = preferred
		}
	} else if next != "" && slices.Contains(choices, next) && !slices.Contains(enabled, next) {
		next = ""
		if len(enabled) > 0 {
			next = enabled[0]
		}
	}
	a.saveProviderField(idx, func(p *config.ProviderConfig) {
		if len(p.DiscoveredModels) == 0 {
			p.DiscoveredModels = slices.Clone(choices)
		}
		p.EnabledModels = slices.Clone(enabled)
		p.DefaultModel = next
	})
	if next != pc.DefaultModel {
		a.modelDraft = next
		a.rebuildGenerator()
	}
}

// renderProviderTestResult 按当前语言显示最近一次连接测试的结果。
func (a *app) renderProviderTestResult(c *ui.Context, pal palette) {
	if a.testErr != nil {
		ui.Text(c, errorText(c.Locale(), a.testErr)).FontSize(12).TextColor(pal.textSecondary)
	} else if a.testModel != "" {
		numText(c, locales.In(c.Locale()).ModelsTestSuccess(a.testModel, a.testLatency)).FontSize(12).TextColor(pal.textSecondary)
	}
}

// syncProviderDraft 在供应商输入框和配置之间同步一个字段。
// 用户正在输入时，草稿写进配置；其他时候，草稿跟随配置。
// 导入凭据、切换档位、拉取模型都会改配置，旧草稿不能把新值写回去。
// 返回值表示这一帧写了配置。
func (a *app) syncProviderDraft(idx int, typing bool, draft *string, saved string, set func(*config.ProviderConfig, string)) bool {
	if !typing {
		*draft = saved
		return false
	}
	if *draft == saved {
		return false
	}
	v := *draft
	a.saveProviderField(idx, func(p *config.ProviderConfig) { set(p, v) })
	return true
}

func (a *app) renderModelsImport(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(8).Children(func() {
			headingText(c, msg.ModelsImport()).SingleLine().TextColor(pal.textPrimary)
			if !a.detectDone {
				ui.Text(c, msg.ModelsScanning()).FontSize(12).TextColor(pal.textSecondary)
				return
			}
			if len(a.detected) == 0 {
				ui.Text(c, msg.ModelsImportEmpty()).FontSize(12).TextColor(pal.textSecondary)
			}
			for _, cred := range a.detected {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Children(func() {
						ui.Text(c, cred.Label).FontSize(12.5).TextColor(pal.textPrimary).
							SingleLine().Tooltip(cred.Label).Shrink(1)
						providerTag(c, credentialKindLabel(cred.Kind, msg), pal)
					})
					if ui.Button(c.Key("import-"+cred.Source), msg.ModelsImportAction()).Clicked() {
						a.importCredential(cred)
					}
				})
			}
		})
	})
}
