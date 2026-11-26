/*
 * [INPUT]: 依赖配置、模型客户端、脱敏选项与摘要服务。
 * [OUTPUT]: 提供 rebuildGenerator、setSummaryModel 和 setThinking。
 * [POS]: 模型设置到摘要服务的唯一装配点。切换供应商、模型或推理档都经这里重建配置。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/summary"
)

func resolveSummaryProviderKey(summaryID string, list []config.ProviderConfig) string {
	if summaryID == "" {
		return ""
	}
	for _, p := range list {
		if p.ID != "" && p.ID == summaryID {
			return providerKey(p)
		}
	}
	for _, p := range list {
		if p.Preset == summaryID {
			return providerKey(p)
		}
	}
	return ""
}

func (a *app) rebuildGenerator() {
	if a.cfg == nil || a.llmc == nil {
		return
	}
	a.cfgNow = a.cfg.Get()

	selectedKey := resolveSummaryProviderKey(a.cfgNow.Summary.ProviderID, a.cfgNow.Providers)
	var pc *config.ProviderConfig
	if selectedKey != "" {
		for i := range a.cfgNow.Providers {
			p := &a.cfgNow.Providers[i]
			if providerKey(*p) == selectedKey {
				pc = p
				break
			}
		}
	}
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
		if a.gen != nil {
			a.gen.SetConfig(summary.LLMConfig{})
		}
		a.gen = nil
		a.summary.cancelActive()
		a.summary.phase = summaryIdle
		a.summary.dayKey = ""
		a.summary.err = nil
		return
	}

	resolved, err := llm.Resolve(*pc, true)
	if err != nil {
		if a.gen != nil {
			a.gen.SetConfig(summary.LLMConfig{})
		}
		a.gen = nil
		a.summary.cancelActive()
		a.summary.phase = summaryFailed
		a.summary.dayKey = ""
		a.summary.err = err
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
		Language:  a.cfgNow.Language,
		MaxTokens: 4096,
		Redact:    a.cfgNow.Redact.Options(),
	}

	if a.gen == nil {
		a.gen = summary.New(a.sums, a.llmc, cfg, nil)
	} else {
		a.gen.SetConfig(cfg)
	}
	if a.summary.phase == summaryFailed {
		a.summary.phase = summaryIdle
		a.summary.dayKey = ""
		a.summary.err = nil
	} else {
		a.summary.err = nil
	}
}

func (a *app) setSummaryModel(providerID, model string) {
	if a.cfg == nil {
		return
	}
	if err := a.cfg.Update(func(c *config.Config) {
		c.Summary.ProviderID = providerID
		c.Summary.Model = model
	}); err != nil {
		a.err = err
		return
	}
	a.summary.cancelActive()
	a.rebuildGenerator()
}

func (a *app) setThinking(level string) {
	if a.cfg == nil {
		return
	}
	if err := a.cfg.Update(func(c *config.Config) { c.Summary.ThinkingEffort = level }); err != nil {
		a.err = err
		return
	}
	a.cfgNow = a.cfg.Get()
	if a.gen != nil {
		a.rebuildGenerator()
	}
}
