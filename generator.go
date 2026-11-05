/*
 * [INPUT]: 依赖配置、模型与摘要服务。
 * [OUTPUT]: 提供摘要生成器配置。
 * [POS]: 连接模型设置与摘要服务。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/redact"
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
		Redact: redact.Options{
			Secrets:  a.cfgNow.Redact.Enabled && a.cfgNow.Redact.Secrets,
			Personal: a.cfgNow.Redact.Enabled && a.cfgNow.Redact.Personal,
			Kinds:    a.cfgNow.Redact.Kinds,
			Allow:    a.cfgNow.Redact.Allow,
			Words:    a.cfgNow.Redact.Words,
		},
	}

	if a.gen == nil {
		a.gen = summary.New(a.sums, a.llmc, cfg, func(e summary.Entry) {
			a.post(func() { a.sumEntries = append(a.sumEntries, e) })
		})
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

// renderCaptureSection 绘制 Capture 设置分组（两张圆角卡片）
