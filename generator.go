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

func (a *app) rebuildGenerator() {
	if a.cfg == nil || a.llmc == nil {
		return
	}
	a.cfgNow = a.cfg.Get()

	var pc *config.ProviderConfig
	for i := range a.cfgNow.Providers {
		p := &a.cfgNow.Providers[i]
		if p.Preset == a.cfgNow.Summary.ProviderID {
			pc = p
			break
		}
	}
	// 没指定就用第一个有 key 的。
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
		a.gen = nil
		return
	}

	resolved, err := llm.Resolve(*pc, true)
	if err != nil {
		a.gen = nil
		a.sumErr = err.Error()
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
}

// setSummaryModel 切换摘要用的模型。
func (a *app) setSummaryModel(providerID, model string) {
	a.cfg.Update(func(c *config.Config) {
		c.Summary.ProviderID = providerID
		c.Summary.Model = model
	})
	a.rebuildGenerator()
}

// setThinking 切换推理档位。
func (a *app) setThinking(level string) {
	a.cfg.Update(func(c *config.Config) { c.Summary.ThinkingEffort = level })
	a.cfgNow = a.cfg.Get()
	if a.gen != nil {
		a.rebuildGenerator()
	}
}
