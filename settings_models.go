/*
 * [INPUT]: 依赖供应商配置、摘要生成器、凭据扫描和语言目录。
 * [OUTPUT]: 提供模型和推理档位选择、供应商编辑、自动摘要开关行和隐私设置。
 * [POS]: 模型设置页的组合边界。网络操作由 provider_requests.go 执行。
 * 连接结果按当前语言显示。用户输入时才写供应商配置。
 * 未输入时，供应商输入框跟随配置。
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
						models := pc.EnabledModels
						if len(models) == 0 && pc.DefaultModel != "" {
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
				ui.Text(c, msg.ModelsProviders()).Grow(1).Bold().TextColor(pal.textPrimary)
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

func (a *app) renderModelsProvider(c *ui.Context, pal palette, idx int) bool {
	msg := locales.In(c.Locale())
	pc := a.cfgNow.Providers[idx]
	key := providerKey(pc)
	name := providerDisplayName(pc)
	selectedKey := resolveSummaryProviderKey(a.cfgNow.Summary.ProviderID, a.cfgNow.Providers)
	removed := false
	ui.Column(c.Key("provider-row-"+key)).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Grow(1).Bold().TextColor(pal.textPrimary)
			isDefault := selectedKey != "" && key == selectedKey
			if ui.Button(c.Key("provider-default-"+key), msg.ModelsDefault()).Disabled(isDefault).Clicked() {
				a.setDefaultProvider(pc)
			}
			if ui.Button(c.Key("provider-configure-"+key), msg.ModelsConfigure()).Clicked() {
				if a.openProvider == key {
					a.openProvider = ""
				} else {
					a.openProvider = key
					a.apiKeyDraft = pc.APIKey
					a.baseURLDraft = pc.ChatBase
					a.modelDraft = pc.DefaultModel
				}
			}
		})
		if a.openProvider != key {
			return
		}
		if preset, ok := providers.ByID(pc.Preset); ok {
			if eps := providers.Endpoints(preset); len(eps) > 1 {
				ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
					for _, ep := range eps {
						active := pc.Region == ep.ID || (pc.Region == "" && ep.ID == eps[0].ID)
						btn := ui.Button(c.Key("provider-region-"+key+"-"+ep.ID), ep.Name)
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
							pc = a.cfgNow.Providers[idx]
						}
					}
				})
			}
		}
		rebuilt := false
		ui.Text(c, msg.ModelsKey()).FontSize(12).TextColor(pal.textPrimary)
		keyIn := ui.TextInput(c.Key("key-"+key), &a.apiKeyDraft).FillWidth().Password().Label(msg.ModelsKey())
		if a.syncProviderDraft(idx, keyIn.Focused(), &a.apiKeyDraft, pc.APIKey, func(p *config.ProviderConfig, v string) { p.APIKey = v }) {
			rebuilt = true
		}
		ui.Text(c, msg.ModelsURL()).FontSize(12).TextColor(pal.textPrimary)
		baseIn := ui.TextInput(c.Key("base-"+key), &a.baseURLDraft).FillWidth().Label(msg.ModelsURL())
		if a.syncProviderDraft(idx, baseIn.Focused(), &a.baseURLDraft, pc.ChatBase, func(p *config.ProviderConfig, v string) { p.ChatBase = v }) {
			rebuilt = true
		}
		ui.Text(c, msg.ModelsModel()).FontSize(12).TextColor(pal.textPrimary)
		modelIn := ui.TextInput(c.Key("model-"+key), &a.modelDraft).FillWidth().Label(msg.ModelsModel())
		if a.syncProviderDraft(idx, modelIn.Focused(), &a.modelDraft, pc.DefaultModel, func(p *config.ProviderConfig, v string) { p.DefaultModel = v }) {
			rebuilt = true
		}
		if rebuilt {
			a.rebuildGenerator()
		}
		testingThis := a.providerTest.phase == testRunning && a.providerTest.providerID == key
		fetchingThis := a.fetchState.phase == fetchRunning && a.fetchState.providerID == key
		ui.Row(c).Gap(8).Children(func() {
			label := msg.ModelsTest()
			if testingThis {
				label = msg.ModelsTesting()
			}
			if ui.Button(c.Key("provider-test-"+key), label).Disabled(testingThis).Clicked() {
				a.testConnection(idx)
			}
			fetchLabel := msg.ModelsFetch()
			if fetchingThis {
				fetchLabel = msg.ModelsFetching()
			}
			if ui.Button(c.Key("provider-fetch-"+key), fetchLabel).Disabled(fetchingThis).Clicked() {
				a.fetchModels(idx)
			}
			if ui.Button(c.Key("provider-remove-"+key), msg.ModelsRemove()).Clicked() {
				removed = true
			}
		})
		if removed {
			return
		}
		if a.providerTest.providerID == key {
			switch a.providerTest.phase {
			case testFailed:
				if a.providerTest.err != nil {
					ui.Text(c, errorText(c.Locale(), a.providerTest.err)).FontSize(12).TextColor(pal.textSecondary)
				}
			case testSucceeded:
				if a.providerTest.model != "" {
					ui.Text(c, msg.ModelsTestSuccess(a.providerTest.model, a.providerTest.latency)).FontSize(12).TextColor(pal.textSecondary)
				}
			}
		}
		if a.fetchState.providerID == key {
			if fetchMsg := a.fetchState.message(c.Locale()); fetchMsg != "" {
				ui.Text(c, fetchMsg).FontSize(12).TextColor(pal.textSecondary)
			}
		}
	})
	return removed
}

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
			if a.credScan.phase != scanDone {
				ui.Text(c, msg.ModelsScanning()).FontSize(12).TextColor(pal.textSecondary)
				return
			}
			if len(a.credScan.detected) == 0 {
				ui.Text(c, msg.ModelsImportEmpty()).FontSize(12).TextColor(pal.textSecondary)
			}
			for _, cred := range a.credScan.detected {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, cred.Label).Grow(1).TextColor(pal.textPrimary)
					if ui.Button(c.Key("import-"+cred.Source+"-"+cred.Kind), msg.ModelsImportAction()).Clicked() {
						a.importCredential(cred)
					}
				})
			}
		})
	})
}
