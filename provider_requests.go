/*
 * [INPUT]: 依赖供应商配置、模型客户端与双语目录。
 * [OUTPUT]: 提供凭据扫描、连接测试与模型列表请求。
 * [POS]: 供应商设置的后台请求边界。结果经界面队列发布。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"strings"
	"time"

	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/locales"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func (a *app) startCredentialScan() {
	if a.detectStarted || a.scanCredentials == nil {
		return
	}
	a.detectStarted = true
	scan := a.scanCredentials
	go func() {
		credentials := scan()
		a.post(func() { a.detected = credentials; a.detectDone = true })
	}()
}

func (a *app) testConnection(idx int) {
	if a.llmc == nil || idx < 0 || idx >= len(a.cfgNow.Providers) {
		return
	}
	pc, client := a.cfgNow.Providers[idx], a.llmc
	lang := a.cfgNow.Language
	a.testing, a.testResult, a.testErr = true, "", nil
	go func() {
		started := time.Now()
		result, ok := "", false
		r, err := llm.Resolve(pc, true)
		if err == nil {
			key := pc.APIKey
			if pc.Auth == "oauth" {
				key = pc.AccessToken
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			resp, e := client.Complete(ctx, llm.Request{Kind: r.Kind, BaseURL: r.BaseURL, APIKey: key, Header: headersFor(pc, r), Model: r.Model, Prompt: "Reply with OK.", MaxTokens: 32})
			err = e
			if err == nil {
				model := resp.Model
				if model == "" {
					model = r.Model
				}
				result = locales.In(lang).ModelsTestSuccess(model, int(time.Since(started).Milliseconds()))
				ok = true
			}
		}
		a.post(func() { a.testing, a.testResult, a.testErr, a.testOK = false, result, err, ok })
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
	pc, client := a.cfgNow.Providers[idx], a.llmc
	a.fetchingModels, a.fetchMsg = true, ""
	go func() {
		resolved, err := llm.Resolve(pc, false)
		var ids []string
		if err == nil {
			key := pc.APIKey
			if pc.Auth == "oauth" && pc.AccessToken != "" {
				key = pc.AccessToken
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ids, err = client.ListModels(ctx, resolved.BaseURL, key, headersFor(pc, resolved))
		}
		var chat []string
		for _, id := range ids {
			low := strings.ToLower(id)
			if strings.Contains(low, "embed") || strings.Contains(low, "rerank") || strings.Contains(low, "whisper") || strings.Contains(low, "tts") || strings.Contains(low, "moderation") || strings.Contains(low, "dall-e") || strings.Contains(low, "image") {
				continue
			}
			chat = append(chat, id)
		}
		if len(chat) == 0 {
			chat = ids
		}
		a.post(func() {
			a.fetchingModels = false
			if err != nil {
				a.fetchMsg = locales.In(a.cfgNow.Language).ModelsFetchFailed() + errorText(a.cfgNow.Language, err)
				return
			}
			if len(chat) == 0 {
				a.fetchMsg = locales.In(a.cfgNow.Language).ModelsFetchEmpty()
				return
			}
			a.cfg.Update(func(c *config.Config) {
				for i := range c.Providers {
					p := &c.Providers[i]
					if p.Preset == pc.Preset && p.Label == pc.Label {
						p.EnabledModels = chat
						if p.DefaultModel == "" {
							p.DefaultModel = chat[0]
						}
						return
					}
				}
			})
			a.cfgNow = a.cfg.Get()
			a.fetchMsg = locales.In(a.cfgNow.Language).ModelsFetchSuccess(len(chat))
		})
	}()
}
