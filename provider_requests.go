/*
 * [INPUT]: 依赖供应商配置与模型客户端。
 * [OUTPUT]: 提供连接测试与模型列表请求。
 * [POS]: 供应商设置的后台请求。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"strings"
	"time"
)

func (a *app) testConnection(idx int) {
	if a.llmc == nil || idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	pc := a.cfgNow.Providers[idx]
	a.testing = true
	a.testResult = ""

	go func() {
		defer func() { a.testing = false }()
		r, err := llm.Resolve(pc, true)
		if err != nil {
			a.testResult = err.Error()
			a.testOK = false
			return
		}
		key := pc.APIKey
		if pc.Auth == "oauth" {
			key = pc.AccessToken
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		resp, err := a.llmc.Complete(ctx, llm.Request{
			Kind:      r.Kind,
			BaseURL:   r.BaseURL,
			APIKey:    key,
			Header:    headersFor(pc, r),
			Model:     r.Model,
			Prompt:    "只回复两个字：收到",
			MaxTokens: 32,
		})
		if err != nil {
			a.testResult = err.Error()
			a.testOK = false
			return
		}
		a.testResult = fmt.Sprintf("连通 · %s · %d tokens",
			strings.TrimSpace(resp.Text), resp.Usage.Input+resp.Usage.Output)
		a.testOK = true
	}()
}

// headersFor 补上少数供应商要的特殊头。
func headersFor(pc config.ProviderConfig, r llm.Resolved) map[string]string {
	h := map[string]string{}
	for k, v := range r.Headers {
		h[k] = v
	}
	// Azure 用 api-key 而不是 Bearer。
	if _, ok := h["api-key"]; ok {
		h["api-key"] = firstNonEmptyStr(pc.APIKey, pc.AccessToken)
	}
	return h
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// withGlass 是个小助手，统一那几个"浮起来"的按钮质感。
func withGlass(el ui.Element, clarity float32) ui.Element {
	return el.Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: clarity})
}

// fetchModels 真的去问供应商要模型列表。
//
// 为什么不在本地维护：模型 id 每周都在变（deepseek-flash 这种名字
// 跟文档里写的 v4.1-flash 完全对不上），硬编码一份必然过期，
// 而过期的表现是「配好了但请求 400」。
//
// 拿到的列表同时写回设置里的 EnabledModels，这样下次打开还是新的。
func (a *app) fetchModels(idx int) {
	if a.llmc == nil || idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	pc := a.cfgNow.Providers[idx]
	a.fetchingModels = true
	a.fetchMsg = ""

	go func() {
		defer func() { a.fetchingModels = false }()

		resolved, err := llm.Resolve(pc, false)
		if err != nil {
			a.fetchMsg = err.Error()
			return
		}
		key := pc.APIKey
		if pc.Auth == "oauth" && pc.AccessToken != "" {
			key = pc.AccessToken
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ids, err := a.llmc.ListModels(ctx, resolved.BaseURL, key, headersFor(pc, resolved))
		if err != nil {
			a.fetchMsg = "拉取失败：" + err.Error()
			return
		}
		if len(ids) == 0 {
			a.fetchMsg = "接口返回了空列表"
			return
		}

		// 只保留看起来像对话模型的。拉回来的列表里常常混着
		// embedding、tts、rerank —— 它们填进摘要模型的选项里只会误导。
		var chat []string
		for _, id := range ids {
			low := strings.ToLower(id)
			if strings.Contains(low, "embed") || strings.Contains(low, "rerank") ||
				strings.Contains(low, "whisper") || strings.Contains(low, "tts") ||
				strings.Contains(low, "moderation") || strings.Contains(low, "dall-e") ||
				strings.Contains(low, "image") {
				continue
			}
			chat = append(chat, id)
		}
		if len(chat) == 0 {
			chat = ids
		}

		preset, _ := providers.ByID(pc.Preset)
		a.cfg.Update(func(c *config.Config) {
			for i := range c.Providers {
				if c.Providers[i].Preset == pc.Preset && c.Providers[i].Label == pc.Label {
					c.Providers[i].EnabledModels = chat
					if c.Providers[i].DefaultModel == "" && len(chat) > 0 {
						c.Providers[i].DefaultModel = chat[0]
					}
					return
				}
			}
		})
		a.cfgNow = a.cfg.Get()
		if idx < len(a.cfgNow.Providers) {
			a.modelDraft = a.cfgNow.Providers[idx].DefaultModel
		}
		_ = preset
		a.fetchMsg = fmt.Sprintf("拉到 %d 个模型", len(chat))
	}()
}
