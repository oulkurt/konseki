/*
 * [INPUT]: 依赖供应商配置、摘要生成器、凭据扫描和语言目录。
 * [OUTPUT]: 提供模型选择、供应商编辑和隐私设置。
 * [POS]: 模型设置页的组合边界，网络操作由 provider_requests.go 执行。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"strings"

	"konseki/internal/config"
	"konseki/internal/providers"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) renderModelsSettings(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		a.settingsRow(c, msg.ModelsSummary(), a.cfgNow.Summary.ProviderID, pal, func() {
			ui.Row(c).Gap(6).Children(func() {
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
							if m.Item(pc.Preset + " · " + model).Checked(a.cfgNow.Summary.ProviderID == pc.Preset && a.cfgNow.Summary.Model == model).Chosen() {
								a.setSummaryModel(pc.Preset, model)
							}
						}
					}
				})
				renderSelectPill(c, "summary-effort", a.cfgNow.Summary.ThinkingEffort, 90, pal.textPrimary, pal, func(m *ui.Menu) {
					for _, level := range []string{"off", "low", "medium", "high", "max"} {
						if m.Item(level).Checked(a.cfgNow.Summary.ThinkingEffort == level).Chosen() {
							a.setThinking(level)
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
			a.settingsRow(c, msg.ModelsAuto(), "", pal, func() {
				on := a.cfgNow.Summary.Auto
				if renderPillSwitch(c, "summary-auto", &on, pal) {
					a.cfg.Update(func(cfg *config.Config) { cfg.Summary.Auto = on })
					a.cfgNow = a.cfg.Get()
				}
			})
			a.settingsRow(c, msg.ModelsRedact(), "", pal, func() {
				on := a.cfgNow.Redact.Enabled
				if renderPillSwitch(c, "summary-redact", &on, pal) {
					a.cfg.Update(func(cfg *config.Config) { cfg.Redact.Enabled = on })
					a.cfgNow = a.cfg.Get()
					a.rebuildGenerator()
				}
			})
		})
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
			for idx := range a.cfgNow.Providers {
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

func (a *app) renderModelsProvider(c *ui.Context, pal palette, idx int) {
	msg := locales.In(c.Locale())
	pc := a.cfgNow.Providers[idx]
	name := pc.Label
	if name == "" {
		if preset, ok := providers.ByID(pc.Preset); ok {
			name = preset.Name
		}
	}
	if name == "" {
		name = pc.Preset
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Grow(1).Bold().TextColor(pal.textPrimary)
			if ui.Button(c.Key("provider-default-"+pc.Preset), msg.ModelsDefault()).Clicked() {
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
		ui.Text(c, msg.ModelsKey()).FontSize(12).TextColor(pal.textPrimary)
		ui.TextInput(c.Key("key-"+pc.Preset), &a.apiKeyDraft).FillWidth().Password().Label(msg.ModelsKey())
		if a.apiKeyDraft != pc.APIKey {
			a.saveProviderField(idx, func(p *config.ProviderConfig) { p.APIKey = a.apiKeyDraft })
		}
		ui.Text(c, msg.ModelsURL()).FontSize(12).TextColor(pal.textPrimary)
		ui.TextInput(c.Key("base-"+pc.Preset), &a.baseURLDraft).FillWidth().Label(msg.ModelsURL())
		if a.baseURLDraft != pc.ChatBase {
			a.saveProviderField(idx, func(p *config.ProviderConfig) { p.ChatBase = a.baseURLDraft })
		}
		ui.Text(c, msg.ModelsModel()).FontSize(12).TextColor(pal.textPrimary)
		ui.TextInput(c.Key("model-"+pc.Preset), &a.modelDraft).FillWidth().Label(msg.ModelsModel())
		if a.modelDraft != pc.DefaultModel {
			a.saveProviderField(idx, func(p *config.ProviderConfig) { p.DefaultModel = a.modelDraft })
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
		if a.testResult != "" {
			ui.Text(c, a.testResult).FontSize(12).TextColor(pal.textSecondary)
		}
	})
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
