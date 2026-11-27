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
	selectedKey := resolveSummaryProviderKey(a.cfgNow.Summary.ProviderID, a.cfgNow.Providers)
	a.settingsCard(c, pal, func() {
		provider := a.cfgNow.Summary.ProviderID
		for _, pc := range a.cfgNow.Providers {
			if providerKey(pc) == selectedKey {
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
						if len(models) == 0 && pc.AllowsModel(pc.DefaultModel) {
							models = []string{pc.DefaultModel}
						}
						key := providerKey(pc)
						for _, model := range models {
							checked := selectedKey != "" && key == selectedKey && a.cfgNow.Summary.Model == model
							if m.Item(providerDisplayName(pc) + " · " + model).Checked(checked).Chosen() {
								a.setSummaryModel(key, model)
							}
						}
					}
				})
			})
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
					if err := a.cfg.Update(func(cfg *config.Config) { cfg.Redact.Enabled = on }); err != nil {
						a.err = err
						return
					}
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
							if err := a.cfg.Update(func(cfg *config.Config) { item.set(cfg, v) }); err != nil {
								a.err = err
								return
							}
							a.cfgNow = a.cfg.Get()
							a.rebuildGenerator()
						}
					})
				}
			}
		})
	})
}

func (a *app) renderAutoSummaryRow(c *ui.Context, pal palette, key string) {
	msg := locales.In(c.Locale())
	minutes := int(summaryInterval(a.cfgNow.Summary) / time.Minute)
	a.settingsRow(c, msg.ModelsAuto(), msg.ModelsAutoHelp(minutes), pal, func() {
		on := a.cfgNow.Summary.Auto
		if renderPillSwitch(c, key, &on, pal) {
			if err := a.cfg.Update(func(cfg *config.Config) { cfg.Summary.Auto = on }); err != nil {
				a.err = err
				return
			}
			a.cfgNow = a.cfg.Get()
		}
	})
}

func (a *app) renderModelsProviders(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	removeIdx := -1
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
			for idx := range a.cfgNow.Providers {
				if a.renderModelsProvider(c, pal, idx) {
					removeIdx = idx
				}
			}
		})
	})
	if removeIdx >= 0 {
		a.removeProvider(removeIdx)
	}
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
				a.providerSearch = ""
			}
		})
	})
	if a.wasAddOpen && !a.addProviderOpen {
		a.providerSearch = ""
	}
	a.wasAddOpen = a.addProviderOpen
}

func providerDisplayName(pc config.ProviderConfig) string {
	if pc.Label != "" {
		return pc.Label
	}
	if preset, ok := providers.ByID(pc.Preset); ok && preset.Name != "" {
		return preset.Name
	}
	return pc.Preset
}

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
	for _, list := range [][]string{pc.DiscoveredModels, resolved, {pc.DefaultModel}} {
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
	return pc.AllowsModel(id)
}

// providerEnabledModels 把勾选状态展开成完整列表，顺序跟随 choices。
// 旧配置没有发现清单时全部勾选：列表本身就是 llm.Resolve 的候选，
// 摘要菜单里能选的正是这些，勾选态必须和菜单一致。
func providerEnabledModels(pc config.ProviderConfig, choices []string) []string {
	if !hasModelSelection(pc) {
		return slices.Clone(choices)
	}
	out := make([]string, 0, len(choices))
	for _, id := range choices {
		if pc.AllowsModel(id) {
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

func (a *app) renderModelsProvider(c *ui.Context, pal palette, idx int) bool {
	msg := locales.In(c.Locale())
	pc := a.cfgNow.Providers[idx]
	preset, _ := providers.ByID(pc.Preset)
	name := providerDisplayName(pc)
	oauth := pc.Auth == "oauth"
	key := providerKey(pc)
	removed := false
	selectedKey := resolveSummaryProviderKey(a.cfgNow.Summary.ProviderID, a.cfgNow.Providers)
	ui.Column(c.Key("provider-row-" + key)).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			headingText(c, name).Grow(1).SingleLine().Tooltip(name).TextColor(pal.textPrimary)
			if ui.Button(c.Key("provider-default-"+providerKey(pc)), msg.ModelsDefault()).Disabled(selectedKey == key).Clicked() {
				a.setDefaultProvider(pc)
			}
			if ui.Button(c.Key("provider-configure-"+providerKey(pc)), msg.ModelsConfigure()).Clicked() {
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
						testing := a.providerTest.phase == testRunning && a.providerTest.providerID == key
						label := msg.ModelsTest()
						if testing {
							label = msg.ModelsTesting()
						}
						if ui.Button(c.Key("provider-test-"+providerKey(pc)), label).Disabled(testing).Clicked() {
							a.testConnection(idx)
						}
					}
					if ui.Button(c.Key("provider-remove-"+providerKey(pc)), msg.ModelsRemove()).Clicked() {
						removed = true
					}
				})
				if !oauth {
					a.renderProviderTestResult(c, pal, key)
				}
			})
		})
	})
	return removed
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
	key := providerKey(pc)
	testing := a.providerTest.phase == testRunning && a.providerTest.providerID == key
	label := msg.ModelsTest()
	if testing {
		label = msg.ModelsTesting()
	}
	ui.Row(c).Children(func() {
		if ui.Button(c.Key("provider-test-"+providerKey(pc)), label).Disabled(testing).Clicked() {
			a.testConnection(idx)
		}
	})
	a.renderProviderTestResult(c, pal, key)
}

// renderProviderKey 绘制按量计费的密钥输入。KeyHint 和取 key 的链接
// 都是预设里的死数据，这里把它们接上。
func (a *app) renderProviderKey(c *ui.Context, pal palette, idx int, pc config.ProviderConfig, preset providers.Preset, msg locales.Messages) {
	ui.Text(c, msg.ModelsKey()).FontSize(12).TextColor(pal.textPrimary)
	keyIn := ui.TextInput(c.Key("key-"+providerKey(pc)), &a.apiKeyDraft).FillWidth().Password().Label(msg.ModelsKey())
	if a.syncProviderDraft(idx, keyIn.Focused(), &a.apiKeyDraft, pc.APIKey, func(p *config.ProviderConfig, v string) { p.APIKey = v }) {
		a.rebuildGenerator()
	}
	if preset.KeyHint != "" {
		ui.Text(c, preset.KeyHint).FontSize(11.5).TextColor(pal.textSecondary).LineHeight(1.35)
	}
	if url := providerKeysURL(pc, preset); url != "" {
		ui.Row(c).Children(func() {
			if ui.Button(c.Key("provider-keys-"+providerKey(pc)), msg.ModelsGetKey()).Clicked() && a.openExternal != nil {
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
					btn := ui.Button(c.Key("provider-region-"+providerKey(pc)+"-"+ep.ID), ep.Name)
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
		baseIn := ui.TextInput(c.Key("base-"+providerKey(pc)), &a.baseURLDraft).FillWidth().
			Placeholder(preset.Endpoint).Label(msg.ModelsURL())
		if a.syncProviderDraft(idx, baseIn.Focused(), &a.baseURLDraft, pc.ChatBase, func(p *config.ProviderConfig, v string) { p.ChatBase = v }) {
			a.rebuildGenerator()
		}
		if preset.Responses != "" {
			ui.Text(c, msg.ModelsResponsesURL()).FontSize(12).TextColor(pal.textPrimary)
			resIn := ui.TextInput(c.Key("responses-"+providerKey(pc)), &a.responsesDraft).FillWidth().
				Placeholder(preset.Responses).Label(msg.ModelsResponsesURL())
			if a.syncProviderDraft(idx, resIn.Focused(), &a.responsesDraft, pc.ResponsesBase, func(p *config.ProviderConfig, v string) { p.ResponsesBase = v }) {
				a.rebuildGenerator()
			}
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
		modelIn := ui.TextInput(c.Key("model-"+providerKey(pc)), &a.modelDraft).FillWidth().Label(msg.ModelsModel())
		if a.syncProviderDraft(idx, modelIn.Focused(), &a.modelDraft, pc.DefaultModel, func(p *config.ProviderConfig, v string) { p.DefaultModel = v }) {
			a.rebuildGenerator()
		}
		key := providerKey(pc)
		showFetch := !preset.NoList && !oauth
		if showFetch {
			label := msg.ModelsFetch()
			fetching := a.fetchState.phase == fetchRunning && a.fetchState.providerID == key
			if fetching {
				label = msg.ModelsFetching()
			}
			ui.Row(c).Children(func() {
				if ui.Button(c.Key("provider-fetch-"+providerKey(pc)), label).Disabled(fetching).Clicked() {
					a.fetchModels(idx)
				}
			})
		}
		if a.fetchState.providerID == key {
			if text := a.fetchState.message(c.Locale()); text != "" {
				ui.Text(c, text).FontSize(11.5).TextColor(pal.textSecondary).LineHeight(1.35)
			}
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
			if ui.Button(c.Key("provider-more-"+providerKey(pc)), msg.ModelsMore(len(choices)-providerModelPreview)).Clicked() {
				a.modelsAll = true
			}
		}
		label := msg.ModelsSelectAll()
		if allChecked {
			label = msg.ModelsDeselectAll()
		}
		if ui.Button(c.Key("provider-select-all-"+providerKey(pc)), label).Clicked() {
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
		cb := ui.CheckboxBase(c.Key("provider-model-"+providerKey(pc)+"-"+id), &checked).Gap(7)
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
		} else if renderDefaultChoice(c, "provider-model-default-"+providerKey(pc)+"-"+id, msg.ModelsDefault()+" "+id, pal) {
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
	previousSummary := a.cfgNow.Summary.Model
	if !a.saveProviderField(idx, func(p *config.ProviderConfig) {
		p.DiscoveredModels = slices.Clone(choices)
		p.EnabledModels = slices.Clone(enabled)
		p.DefaultModel = next
	}) {
		return
	}
	if next != pc.DefaultModel {
		a.modelDraft = a.cfgNow.Providers[idx].DefaultModel
	}
	if next != pc.DefaultModel || previousSummary != a.cfgNow.Summary.Model {
		a.summary.cancelActive()
		a.rebuildGenerator()
	}
}

// renderProviderTestResult 按当前语言显示最近一次连接测试的结果。
func (a *app) renderProviderTestResult(c *ui.Context, pal palette, key string) {
	state := a.providerTest
	if state.providerID != key {
		return
	}
	if state.phase == testFailed && state.err != nil {
		ui.Text(c, errorText(c.Locale(), state.err)).FontSize(12).TextColor(pal.textSecondary)
	} else if state.phase == testSucceeded && state.model != "" {
		numText(c, locales.In(c.Locale()).ModelsTestSuccess(state.model, state.latency)).FontSize(12).TextColor(pal.textSecondary)
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
	return a.saveProviderField(idx, func(p *config.ProviderConfig) { set(p, v) })
}

func (a *app) renderModelsImport(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(8).Children(func() {
			headingText(c, msg.ModelsImport()).SingleLine().TextColor(pal.textPrimary)
			if a.credScan.phase != scanDone {
				ui.Text(c, msg.ModelsScanning()).FontSize(12).TextColor(pal.textSecondary)
				return
			}
			if len(a.credScan.detected) == 0 {
				ui.Text(c, msg.ModelsImportEmpty()).FontSize(12).TextColor(pal.textSecondary)
			}
			for _, cred := range a.credScan.detected {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Children(func() {
						ui.Text(c, cred.Label).FontSize(12.5).TextColor(pal.textPrimary).
							SingleLine().Tooltip(cred.Label).Shrink(1)
						providerTag(c, credentialKindLabel(cred.Kind, msg), pal)
					})
					if ui.Button(c.Key("import-"+cred.Source+"-"+cred.Kind), msg.ModelsImportAction()).Clicked() {
						a.importCredential(cred)
					}
				})
			}
		})
	})
}
