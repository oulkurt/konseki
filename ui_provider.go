/*
 * [INPUT]: 依赖供应商预设、凭据与配置存储。
 * [OUTPUT]: 提供供应商导入、增删和配置操作。
 * [POS]: 模型设置的数据边界，由 settings_models.go 调用。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"slices"

	"konseki/internal/config"
	"konseki/internal/providers"
)

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
	a.testResult = ""
	a.testOK = true
}

func providerKey(pc config.ProviderConfig) string {
	return pc.Preset + "\x00" + pc.Label
}

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
	a.openProvider = providerKey(pc)
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
	a.testResult = ""
	a.testOK = true
}

// testConnection 发一个最小请求，确认 key/地址/模型都对得上。
