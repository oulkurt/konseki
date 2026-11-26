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

func providerKey(pc config.ProviderConfig) string {
	if pc.ID != "" {
		return pc.ID
	}
	return pc.Preset + "\x00" + pc.Label
}

func (a *app) importCredential(cred providers.Credential) {
	presetID := "anthropic"
	model := "claude-sonnet-5-5"
	if cred.Source == "codex" {
		presetID = "openai"
		model = "gpt-5.2"
	}
	preset, _ := providers.ByID(presetID)
	auth := cred.Kind
	if auth == "api_key" {
		auth = "apikey"
	}

	pc := config.ProviderConfig{
		Preset:        presetID,
		Label:         cred.Label,
		ChatBase:      preset.Chat,
		AnthropicBase: preset.Anthropic,
		Auth:          auth,
		AccessToken:   cred.AccessToken,
		RefreshToken:  cred.RefreshToken,
		AccountID:     cred.AccountID,
		ExpiresAt:     cred.ExpiresAt,
		DefaultModel:  model,
	}
	if auth == "apikey" {
		pc.APIKey = cred.AccessToken
	}

	var matchedKey string
	if err := a.cfg.Update(func(c *config.Config) {
		for i := range c.Providers {
			if c.Providers[i].Preset == pc.Preset && c.Providers[i].Label == pc.Label {
				pc.ID = c.Providers[i].ID
				c.Providers[i] = pc
				matchedKey = providerKey(c.Providers[i])
				return
			}
		}
		pc.ID = config.NewProviderID()
		c.Providers = append(c.Providers, pc)
	}); err != nil {
		a.err = err
		return
	}
	a.cfgNow = a.cfg.Get()
	if matchedKey != "" {
		a.providerTest.resetForProvider(matchedKey)
		a.fetchState.clear()
	}
	a.rebuildGenerator()
}

func (a *app) addProvider(p providers.Preset) {
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
		ID:            config.NewProviderID(),
		Preset:        p.ID,
		Region:        region,
		ChatBase:      chat,
		AnthropicBase: anthropic,
		DefaultModel:  model,
	}
	if err := a.cfg.Update(func(c *config.Config) { c.Providers = append(c.Providers, pc) }); err != nil {
		a.err = err
		return
	}
	a.cfgNow = a.cfg.Get()
	added := pc
	for _, item := range a.cfgNow.Providers {
		if item.ID == pc.ID {
			added = item
			break
		}
	}
	a.rebuildGenerator()
	a.openProvider = providerKey(added)
	a.apiKeyDraft = ""
	a.baseURLDraft = added.ChatBase
	a.modelDraft = added.DefaultModel
	a.providerTest.clear()
	a.fetchState.clear()
}

func (a *app) removeProvider(idx int) {
	if idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	target := a.cfgNow.Providers[idx]
	targetKey := providerKey(target)
	if err := a.cfg.Update(func(c *config.Config) {
		selectedKey := resolveSummaryProviderKey(c.Summary.ProviderID, c.Providers)
		c.Providers = slices.DeleteFunc(c.Providers, func(p config.ProviderConfig) bool {
			return providerKey(p) == targetKey
		})
		if selectedKey == targetKey {
			c.Summary.ProviderID = ""
			c.Summary.Model = ""
		}
	}); err != nil {
		a.err = err
		return
	}
	a.cfgNow = a.cfg.Get()
	if a.openProvider == targetKey {
		a.openProvider = ""
	}
	a.providerTest.resetForProvider(targetKey)
	if a.fetchState.providerID == targetKey {
		a.fetchState.clear()
	}
	a.rebuildGenerator()
}

func (a *app) saveProviderField(idx int, fn func(*config.ProviderConfig)) {
	if idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	targetKey := providerKey(a.cfgNow.Providers[idx])
	if err := a.cfg.Update(func(c *config.Config) {
		for i := range c.Providers {
			if providerKey(c.Providers[i]) == targetKey {
				fn(&c.Providers[i])
				return
			}
		}
	}); err != nil {
		a.err = err
		return
	}
	a.cfgNow = a.cfg.Get()
	a.providerTest.resetForProvider(targetKey)
	if a.fetchState.providerID == targetKey {
		a.fetchState.clear()
	}
}

func (a *app) setDefaultProvider(pc config.ProviderConfig) {
	a.setSummaryModel(providerKey(pc), pc.DefaultModel)
}
