/*
 * [INPUT]: 依赖供应商配置、模型客户端与显式后台任务状态。
 * [OUTPUT]: 提供凭据扫描、带作用域和护栏令牌的连接测试与模型列表请求。
 * [POS]: 供应商设置的后台请求边界。结果经界面队列发布并受 epoch 校验。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"konseki/internal/config"
	"konseki/internal/llm"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func (a *app) startCredentialScan() {
	if a.credScan.phase != scanIdle || a.scanCredentials == nil || a.tasks.isClosed() {
		return
	}
	a.credScan.phase = scanRunning
	a.credScan.epoch++
	epoch := a.credScan.epoch
	scan := a.scanCredentials
	ok := a.tasks.goTask(func(context.Context) {
		credentials := scan()
		a.postUI(func() {
			if a.credScan.epoch != epoch {
				return
			}
			a.credScan.detected = credentials
			a.credScan.phase = scanDone
		})
	})
	if !ok {
		a.credScan.phase = scanIdle
	}
}

func (a *app) testConnection(idx int) {
	if a.llmc == nil || idx < 0 || idx >= len(a.cfgNow.Providers) || a.tasks.isClosed() {
		return
	}
	pc, client := a.cfgNow.Providers[idx], a.llmc
	pid := providerKey(pc)
	epoch, ctx, cancel := a.providerTest.start(a.tasks.ensure(), pid, 45*time.Second)
	ok := a.tasks.goTask(func(context.Context) {
		defer cancel()
		started := time.Now()
		model, latency := "", 0
		r, err := llm.Resolve(pc, true)
		if err == nil {
			key := pc.APIKey
			if pc.Auth == "oauth" {
				key = pc.AccessToken
			}
			resp, e := client.Complete(ctx, llm.Request{Kind: r.Kind, BaseURL: r.CallBase(), APIKey: key, Header: headersFor(pc, r), AuthKind: r.AuthKind, Model: r.Model, Prompt: "Reply with OK.", MaxTokens: 32})
			err = e
			if err == nil {
				model = resp.Model
				if model == "" {
					model = r.Model
				}
				latency = int(time.Since(started).Milliseconds())
			}
		}
		a.postUI(func() {
			a.providerTest.finish(epoch, pid, model, latency, err)
		})
	})
	if !ok {
		cancel()
		a.providerTest.resetForProvider(pid)
	}
}

func headersFor(pc config.ProviderConfig, r llm.Resolved) map[string]string {
	h := map[string]string{}
	for k, v := range r.Headers {
		h[k] = v
	}
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

func withGlass(el ui.Element, clarity float32) ui.Element {
	return el.Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: clarity})
}

// fetchModels 真的去问供应商要模型列表。
//
// 为什么不在本地维护：模型 id 每周都在变（deepseek-flash 这种名字
// 跟文档里写的 v4.1-flash 完全对不上），硬编码一份必然过期，
// 而过期的表现是「配好了但请求 400」。
//
// 拿到的列表写成发现清单（全量），并默认全选进 EnabledModels。
// 分开存是为了让用户取消勾选：发现清单留着，界面上才能把取消的模型显示回来。
func (a *app) fetchModels(idx int) {
	if a.llmc == nil || idx < 0 || idx >= len(a.cfgNow.Providers) || a.tasks.isClosed() {
		return
	}
	pc, client := a.cfgNow.Providers[idx], a.llmc
	pid := providerKey(pc)
	epoch, ctx, cancel := a.fetchState.start(a.tasks.ensure(), pid, 30*time.Second)
	ok := a.tasks.goTask(func(context.Context) {
		defer cancel()
		resolved, err := llm.ResolveEndpoint(pc, false)
		var ids []string
		if err == nil {
			key := pc.APIKey
			if pc.Auth == "oauth" && pc.AccessToken != "" {
				key = pc.AccessToken
			}
			ids, err = client.ListModels(ctx, llm.Request{Kind: resolved.Kind, BaseURL: resolved.CallBase(), APIKey: key, Header: headersFor(pc, resolved), AuthKind: resolved.AuthKind})
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
		a.postUI(func() {
			if a.fetchState.epoch != epoch || a.fetchState.providerID != pid {
				return
			}
			if err != nil {
				a.fetchState.finish(epoch, pid, 0, err)
				return
			}
			if len(chat) == 0 {
				a.fetchState.finish(epoch, pid, 0, nil)
				return
			}
			found := false
			if updateErr := a.cfg.Update(func(c *config.Config) {
				for i := range c.Providers {
					p := &c.Providers[i]
					if providerKey(*p) == pid {
						found = true
						p.DiscoveredModels = slices.Clone(chat)
						p.EnabledModels = slices.Clone(chat)
						if p.DefaultModel == "" {
							p.DefaultModel = chat[0]
						}
						return
					}
				}
			}); updateErr != nil {
				a.err = updateErr
				a.fetchState.finish(epoch, pid, 0, updateErr)
				return
			}
			a.cfgNow = a.cfg.Get()
			if !found {
				a.fetchState.clear()
				return
			}
			a.rebuildGenerator()
			a.fetchState.finish(epoch, pid, len(chat), nil)
		})
	})
	if !ok {
		cancel()
		a.fetchState.clear()
	}
}
