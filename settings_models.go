/*
 * [INPUT]: 依赖供应商配置、摘要生成器、凭据扫描和语言目录。
 * [OUTPUT]: 提供模型和推理档位选择、供应商编辑、自动摘要开关行和隐私设置。
 * [POS]: 模型设置页的组合边界，网络操作由 provider_requests.go 执行。供应商输入框只在用户输入时写配置，其他时候跟随配置。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"strings"
	"time"

	"konseki/internal/config"
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
						models := pc.EnabledModels
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
				ui.Text(c, msg.ModelsProviders()).Grow(1).Bold().TextColor(pal.textPrimary)
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
			ui.Text(c, msg.ModelsAdd()).Bold()
			ui.TextInput(c.Key("provider-search"), &a.providerSearch).FillWidth().Placeholder(msg.ModelsSearch()).Label(msg.ModelsSearch())
			ui.Scroll(c).Height(350).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for _, preset := range providers.All() {
						if !strings.Contains(strings.ToLower(preset.Name+" "+preset.ID), strings.ToLower(strings.TrimSpace(a.providerSearch))) {
							continue
						}
						if ui.Button(c.Key("provider-choice-"+preset.ID), preset.Name).FillWidth().Clicked() {
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

func (a *app) renderModelsProvider(c *ui.Context, pal palette, idx int) {
	msg := locales.In(c.Locale())
	pc := a.cfgNow.Providers[idx]
	name := providerDisplayName(pc)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Grow(1).Bold().TextColor(pal.textPrimary)
			if ui.Button(c.Key("provider-default-"+pc.Preset), msg.ModelsDefault()).Disabled(a.cfgNow.Summary.ProviderID == pc.Preset).Clicked() {
				a.setDefaultProvider(pc)
			}
			if ui.Button(c.Key("provider-configure-"+pc.Preset), msg.ModelsConfigure()).Clicked() {
				if a.openProvider == providerKey(pc) {
					a.openProvider = ""
				} else {
					a.openProvider = providerKey(pc)
					a.apiKeyDraft = pc.APIKey
					a.baseURLDraft = pc.ChatBase
					a.modelDraft = pc.DefaultModel
				}
			}
		})
		if a.openProvider != providerKey(pc) {
			return
		}
		if preset, ok := providers.ByID(pc.Preset); ok {
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
		}
		ui.Text(c, msg.ModelsKey()).FontSize(12).TextColor(pal.textPrimary)
		keyIn := ui.TextInput(c.Key("key-"+pc.Preset), &a.apiKeyDraft).FillWidth().Password().Label(msg.ModelsKey())
		a.syncProviderDraft(idx, keyIn.Focused(), &a.apiKeyDraft, pc.APIKey, func(p *config.ProviderConfig, v string) { p.APIKey = v })
		ui.Text(c, msg.ModelsURL()).FontSize(12).TextColor(pal.textPrimary)
		baseIn := ui.TextInput(c.Key("base-"+pc.Preset), &a.baseURLDraft).FillWidth().Label(msg.ModelsURL())
		a.syncProviderDraft(idx, baseIn.Focused(), &a.baseURLDraft, pc.ChatBase, func(p *config.ProviderConfig, v string) { p.ChatBase = v })
		ui.Text(c, msg.ModelsModel()).FontSize(12).TextColor(pal.textPrimary)
		modelIn := ui.TextInput(c.Key("model-"+pc.Preset), &a.modelDraft).FillWidth().Label(msg.ModelsModel())
		if a.syncProviderDraft(idx, modelIn.Focused(), &a.modelDraft, pc.DefaultModel, func(p *config.ProviderConfig, v string) { p.DefaultModel = v }) {
			a.rebuildGenerator()
		}
		ui.Row(c).Gap(8).Children(func() {
			label := msg.ModelsTest()
			if a.testing {
				label = msg.ModelsTesting()
			}
			if ui.Button(c.Key("provider-test-"+pc.Preset), label).Disabled(a.testing).Clicked() {
				a.testConnection(idx)
			}
			if ui.Button(c.Key("provider-remove-"+pc.Preset), msg.ModelsRemove()).Clicked() {
				a.removeProvider(idx)
			}
		})
		if a.testErr != nil {
			ui.Text(c, errorText(c.Locale(), a.testErr)).FontSize(12).TextColor(pal.textSecondary)
		} else if a.testResult != "" {
			ui.Text(c, a.testResult).FontSize(12).TextColor(pal.textSecondary)
		}
	})
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
			ui.Text(c, msg.ModelsImport()).Bold().TextColor(pal.textPrimary)
			if !a.detectDone {
				ui.Text(c, msg.ModelsScanning()).FontSize(12).TextColor(pal.textSecondary)
				return
			}
			if len(a.detected) == 0 {
				ui.Text(c, msg.ModelsImportEmpty()).FontSize(12).TextColor(pal.textSecondary)
			}
			for _, cred := range a.detected {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, cred.Label).Grow(1).TextColor(pal.textPrimary)
					if ui.Button(c.Key("import-"+cred.Source), msg.ModelsImportAction()).Clicked() {
						a.importCredential(cred)
					}
				})
			}
		})
	})
}
