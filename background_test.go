/*
 * [INPUT]: 依赖临时存储、假模型服务与界面任务队列。
 * [OUTPUT]: 验证摘要后台任务经队列回写界面状态，以及连接测试报告的模型和耗时。
 * [POS]: 应用后台并发契约的行为测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
)

func TestSummaryBackgroundPublication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("请求路径: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "fake", "choices": []any{map[string]any{"message": map[string]string{"content": `{"headline":"测试摘要","summary":"完成工作"}`}}}})
	}))
	defer server.Close()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.llmc.HTTP = server.Client()
	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{{Preset: "test", ChatBase: server.URL + "/v1", DefaultModel: "fake"}}
	}); err != nil {
		t.Fatal(err)
	}
	jobs := make(chan func(), 8)
	a.post = func(f func()) { jobs <- f }
	a.rebuildGenerator()
	now := time.Now()
	a.blocks = []capture.Block{{Start: now.Add(-time.Minute), End: now, App: "Editor", Samples: 1}}
	a.runSummary(true)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for a.sumRunning {
		select {
		case f := <-jobs:
			f()
		case <-timer.C:
			t.Fatal("摘要任务超时")
		}
	}
	if a.sumErr != "" || len(a.sumEntries) != 1 || a.sumEntries[0].Headline != "测试摘要" {
		t.Fatalf("摘要回写失败: entries=%+v error=%q", a.sumEntries, a.sumErr)
	}
}

func TestConnectionReportsModelAndLatency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "actual-model", "choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer server.Close()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.llmc.HTTP = server.Client()
	if err := a.cfg.Update(func(c *config.Config) {
		c.Language = "en"
		c.Providers = []config.ProviderConfig{{Preset: "test", ChatBase: server.URL, DefaultModel: "requested"}}
	}); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	jobs := make(chan func(), 1)
	a.post = func(f func()) { jobs <- f }
	a.testConnection(0)
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("连接测试超时")
	}
	if !a.testOK || a.testModel != "actual-model" || a.testLatency < 0 {
		t.Fatalf("连接结果错误：模型=%q 耗时=%d", a.testModel, a.testLatency)
	}
}
