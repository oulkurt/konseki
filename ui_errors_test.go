/*
 * [INPUT]: 依赖内部错误种类、临时存储、模拟模型服务和离屏页面检查。
 * [OUTPUT]: 验证错误翻译、外部细节与页面语言。
 * 验证摘要、清除、命令链接和旧文件的失败路径。
 * 验证连接测试成功后切换语言。
 * [POS]: 界面错误展示边界的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/query"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func TestErrorTextLanguages(t *testing.T) {
	external := "外部细节 external detail"
	cases := []struct {
		name   string
		err    error
		zh, en string
		detail bool
	}{
		{"endpoint", fmt.Errorf("provider: %w", llm.ErrNoEndpoint), "尚未配置接口地址", "No endpoint configured", false},
		{"model", fmt.Errorf("provider: %w", llm.ErrNoModel), "尚未选择模型", "No model selected", false},
		{"response", &llm.RequestError{Kind: llm.ErrInvalidResponse, Cause: errors.New(external)}, "无法解析模型响应", "Could not parse the model response", false},
		{"empty", llm.ErrEmptyResponse, "模型没有返回内容", "The model returned no content", false},
		{"connection", &llm.RequestError{Kind: llm.ErrConnection, Cause: errors.New(external)}, "无法连接模型接口：" + external, "Could not connect to the model service: " + external, true},
		{"api", &llm.APIError{Status: 429, Body: external}, "模型接口返回错误：429: " + external, "The model service returned an error: 429: " + external, true},
		{"link-path", fmt.Errorf("secret: %w", query.ErrExecutableNotAbsolute), "程序路径不是绝对路径", "The executable path is not absolute", false},
		{"link-exists", fmt.Errorf("secret: %w", query.ErrLinkOccupied), "命令链接的位置已被其他文件占用", "A file already exists at the command link location", false},
		{"clear-count", clearCountError(2), "2 个文件无法删除", "2 files could not be deleted", false},
		{"unknown", errors.New(external), "操作失败：" + external, "Operation failed: " + external, true},
	}
	for _, c := range cases {
		for _, lang := range []string{"zh-Hans", "en"} {
			t.Run(c.name+"/"+lang, func(t *testing.T) {
				got := errorText(lang, c.err)
				want := c.zh
				if lang == "en" {
					want = c.en
				}
				if got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
				data := []string(nil)
				if c.detail {
					data = append(data, external)
				}
				if bad := langViolations(stripUserData([]string{got}, data...), lang, settingsAllow); len(bad) > 0 {
					t.Fatalf("语言错误 %q: %q", bad, got)
				}
			})
		}
	}
}

func TestErrorPagesLanguages(t *testing.T) {
	const external = "外部细节 external detail"
	cases := []struct {
		page, name string
		err        error
	}{
		{"global", "system", errors.New(external)},
		{"timeline", "system", errors.New(external)},
		{"provider", "endpoint", llm.ErrNoEndpoint},
		{"provider", "model", llm.ErrNoModel},
		{"provider", "response", &llm.RequestError{Kind: llm.ErrInvalidResponse, Cause: errors.New(external)}},
		{"provider", "empty", llm.ErrEmptyResponse},
		{"provider", "connection", &llm.RequestError{Kind: llm.ErrConnection, Cause: errors.New(external)}},
		{"provider", "api", &llm.APIError{Status: 429, Body: external}},
		{"provider", "other", errors.New(external)},
		{"clear", "count", clearCountError(2)},
		{"clear", "system", errors.New(external)},
		{"agent-link", "relative", query.ErrExecutableNotAbsolute},
		{"agent-link", "occupied", query.ErrLinkOccupied},
		{"agent-link", "system", errors.New(external)},
		{"agent-legacy", "system", errors.New(external)},
	}
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, tc := range cases {
			t.Run(lang+"/"+tc.page+"/"+tc.name, func(t *testing.T) {
				a, cleanup := newTestApp(t)
				defer cleanup()
				a.cfgNow.Language = lang
				a.tab = 1
				a.lastLoad = time.Now().Add(time.Hour)
				msg := locales.In(lang)
				shown := errorText(lang, tc.err)
				want := ""
				switch tc.page {
				case "global", "timeline":
					if tc.page == "timeline" {
						a.tab = 0
					}
					a.err = tc.err
					want = msg.ShellReadError() + shown
				case "provider":
					a.settingsTab = 1
					pc := config.ProviderConfig{Preset: "custom", Label: "User provider", DefaultModel: "User model"}
					if err := a.cfg.Update(func(c *config.Config) { c.Providers = []config.ProviderConfig{pc} }); err != nil {
						t.Fatal(err)
					}
					a.cfgNow = a.cfg.Get()
					a.openProvider = providerKey(pc)
					a.detectStarted, a.detectDone = true, true
					a.testErr = tc.err
					want = shown
				case "clear":
					a.clearErr = tc.err
					want = shown
				case "agent-link":
					a.settingsTab = 2
					a.legacyChecked = true
					a.linkErr = tc.err
					want = msg.GeneralAgentLinkError(shown)
				case "agent-legacy":
					a.settingsTab = 2
					a.legacyChecked = true
					a.legacyErr = tc.err
					want = msg.GeneralAgentLegacyError(shown)
				}
				tt := ui.NewTester(a.view, 780, 3000)
				tt.SetLocale(lang)
				if !tt.HasText(want) {
					t.Fatalf("页面未显示 %q: %q", want, tt.Texts())
				}
				checkPageLanguage(t, tt, a, lang, external, "User provider", "User model")
			})
		}
	}
}

type errorTestTransport func(*http.Request) (*http.Response, error)

func (f errorTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConnectionErrorLanguages(t *testing.T) {
	const external = "外部响应 external response"
	cases := []struct {
		name, base, model, body string
		status                  int
		cause                   error
		kind                    error
	}{
		{name: "endpoint", model: "test-model", kind: llm.ErrNoEndpoint},
		{name: "model", base: "https://example.test", kind: llm.ErrNoModel},
		{name: "parse", base: "https://example.test", model: "test-model", body: "{", status: 200, kind: llm.ErrInvalidResponse},
		{name: "empty", base: "https://example.test", model: "test-model", body: `{"choices":[]}`, status: 200, kind: llm.ErrEmptyResponse},
		{name: "connect", base: "https://example.test", model: "test-model", cause: errors.New(external), kind: llm.ErrConnection},
		{name: "http", base: "https://example.test", model: "test-model", body: external, status: 429},
		{name: "api", base: "https://example.test", model: "test-model", body: `{"error":{"message":"外部响应 external response"}}`, status: 200},
	}
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, tc := range cases {
			t.Run(lang+"/"+tc.name, func(t *testing.T) {
				a, cleanup := newTestApp(t)
				defer cleanup()
				pc := config.ProviderConfig{Preset: "custom", Label: "provider", ChatBase: tc.base, DefaultModel: tc.model}
				if err := a.cfg.Update(func(c *config.Config) { c.Language = lang; c.Providers = []config.ProviderConfig{pc} }); err != nil {
					t.Fatal(err)
				}
				a.cfgNow = a.cfg.Get()
				a.tab, a.settingsTab = 1, 1
				a.openProvider = providerKey(pc)
				a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
					if tc.cause != nil {
						return nil, tc.cause
					}
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
				})
				queued := make(chan func(), 1)
				a.post = func(f func()) { queued <- f }
				a.testConnection(0)
				select {
				case f := <-queued:
					f()
				case <-time.After(10 * time.Second):
					t.Fatal("连接测试超时")
				}
				if a.testOK || a.testErr == nil {
					t.Fatalf("连接测试结果错误: %v", a.testErr)
				}
				var want string
				if tc.kind != nil {
					switch tc.kind {
					case llm.ErrNoEndpoint:
						want = locales.In(lang).ErrorNoEndpoint()
					case llm.ErrNoModel:
						want = locales.In(lang).ErrorNoModel()
					case llm.ErrInvalidResponse:
						want = locales.In(lang).ErrorInvalidResponse()
					case llm.ErrEmptyResponse:
						want = locales.In(lang).ErrorEmptyResponse()
					case llm.ErrConnection:
						want = locales.In(lang).ErrorConnection()
					}
				} else {
					want = locales.In(lang).ErrorProvider()
				}
				shown := errorText(lang, a.testErr)
				if !strings.HasPrefix(shown, want) {
					t.Fatalf("got %q, want prefix %q", shown, want)
				}
				tt := ui.NewTester(a.view, 780, 3000)
				tt.SetLocale(lang)
				if !tt.HasText(shown) {
					t.Fatalf("页面未显示错误: %q", tt.Texts())
				}
				detail := ""
				if tc.kind == llm.ErrConnection || tc.kind == nil {
					detail = strings.TrimPrefix(shown, want)
				}
				checkPageLanguage(t, tt, a, lang, detail, external, "provider", "test-model")
			})
		}
	}
}

func TestFetchModelsLanguages(t *testing.T) {
	const external = "外部响应 external response"
	cases := []struct {
		name, body string
		status     int
	}{
		{"error", external, 429},
		{"empty", `{"data":[]}`, 200},
		{"success", `{"data":[{"id":"test-model"}]}`, 200},
	}
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, tc := range cases {
			t.Run(lang+"/"+tc.name, func(t *testing.T) {
				a, cleanup := newTestApp(t)
				defer cleanup()
				pc := config.ProviderConfig{Preset: "custom", Label: "provider", ChatBase: "https://example.test", DefaultModel: "test-model"}
				if err := a.cfg.Update(func(c *config.Config) { c.Language = lang; c.Providers = []config.ProviderConfig{pc} }); err != nil {
					t.Fatal(err)
				}
				a.cfgNow = a.cfg.Get()
				a.tab, a.settingsTab = 1, 1
				a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
				})
				queued := make(chan func(), 1)
				a.post = func(f func()) { queued <- f }
				a.fetchModels(0)
				select {
				case f := <-queued:
					f()
				case <-time.After(10 * time.Second):
					t.Fatal("模型列表请求超时")
				}
				msg := locales.In(lang)
				want := ""
				switch tc.name {
				case "error":
					want = msg.ModelsFetchFailed() + msg.ErrorProvider() + "429: " + external
				case "empty":
					want = msg.ModelsFetchEmpty()
				case "success":
					want = msg.ModelsFetchSuccess(1)
				}
				if a.fetchMsg != want {
					t.Fatalf("got %q, want %q", a.fetchMsg, want)
				}
				tt := ui.NewTester(a.view, 780, 3000)
				tt.SetLocale(lang)
				checkPageLanguage(t, tt, a, lang, "provider", "test-model")
				if bad := langViolations(stripUserData([]string{a.fetchMsg}, external), lang, settingsAllow); len(bad) > 0 {
					t.Fatalf("语言错误 %q: %q", bad, a.fetchMsg)
				}
			})
		}
	}
}

func TestErrorLanguageSwitch(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab, a.settingsTab = 1, 1
	pc := config.ProviderConfig{Preset: "custom", Label: "provider", DefaultModel: "test-model"}
	if err := a.cfg.Update(func(c *config.Config) { c.Providers = []config.ProviderConfig{pc} }); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	a.openProvider = providerKey(pc)
	a.testErr = llm.ErrNoEndpoint
	tt := ui.NewTester(a.view, 780, 3000)
	for _, lang := range []string{"zh-Hans", "en"} {
		tt.SetLocale(lang)
		want := locales.In(lang).ErrorNoEndpoint()
		if !tt.HasText(want) {
			t.Fatalf("切换语言后没有显示 %q: %q", want, tt.Texts())
		}
		checkPageLanguage(t, tt, a, lang, "provider", "test-model")
	}
}

func TestSettingsSaveFailureLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			backup := a.appDir + "-saved"
			if err := os.Rename(a.appDir, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(a.appDir, []byte("blocked"), 0600); err != nil {
				t.Fatal(err)
			}
			a.tab = 1
			a.lastLoad = time.Now().Add(time.Hour)
			a.updateRecorder(func(r *config.Recorder) { r.Enabled = !r.Enabled })
			if a.err == nil {
				t.Fatal("设置保存应当失败")
			}
			tt := ui.NewTester(a.view, 780, 3000)
			tt.SetLocale(lang)
			want := locales.In(lang).ShellReadError() + errorText(lang, a.err)
			if !tt.HasText(want) {
				t.Fatalf("页面未显示错误: %q", tt.Texts())
			}
			checkPageLanguage(t, tt, a, lang, a.err.Error())
		})
	}
}

func TestSummaryFailureLanguages(t *testing.T) {
	const external = "外部响应 external response"
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			if err := a.cfg.Update(func(c *config.Config) {
				c.Language = lang
				c.Providers = []config.ProviderConfig{{Preset: "test", ChatBase: "https://example.test", DefaultModel: "test-model"}}
			}); err != nil {
				t.Fatal(err)
			}
			a.cfgNow = a.cfg.Get()
			a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(external)), Header: make(http.Header)}, nil
			})
			jobs := make(chan func(), 1)
			a.post = func(f func()) { jobs <- f }
			a.rebuildGenerator()
			now := time.Now()
			a.blocks = []capture.Block{{Start: now.Add(-time.Minute), End: now, App: "user-app", Samples: 1}}
			a.runSummary(true)
			select {
			case f := <-jobs:
				f()
			case <-time.After(10 * time.Second):
				t.Fatal("摘要任务超时")
			}
			want := locales.In(lang).ErrorProvider() + "429: " + external
			if a.sumErr != want {
				t.Fatalf("摘要错误 %q，期望 %q", a.sumErr, want)
			}
			a.lastLoad = time.Now().Add(time.Hour)
			tt := ui.NewTester(a.view, 780, 1000)
			tt.SetLocale(lang)
			checkPageLanguage(t, tt, a, lang, "user-app", external)
			if tt.HasText(want) {
				t.Fatal("摘要错误状态已渲染，需更新页面测试")
			}
		})
	}
}

func TestClearFailureLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			path := filepath.Join(a.appDir, "summaries")
			if err := os.Rename(path, path+"-saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("blocked"), 0600); err != nil {
				t.Fatal(err)
			}
			a.tab = 1
			jobs := make(chan func(), 1)
			a.post = func(f func()) { jobs <- f }
			a.clearActivity()
			select {
			case f := <-jobs:
				f()
			case <-time.After(10 * time.Second):
				t.Fatal("清除任务超时")
			}
			if a.clearErr == nil {
				t.Fatal("清除应当失败")
			}
			tt := ui.NewTester(a.view, 780, 3000)
			tt.SetLocale(lang)
			if !tt.HasText(errorText(lang, a.clearErr)) {
				t.Fatalf("清除错误未显示：%q", tt.Texts())
			}
			checkPageLanguage(t, tt, a, lang, a.clearErr.Error())
		})
	}
}

func TestAgentErrorSourcesLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, source := range []string{"link", "legacy"} {
			t.Run(lang+"/"+source, func(t *testing.T) {
				a, cleanup := newTestApp(t)
				defer cleanup()
				a.tab, a.settingsTab = 1, 2
				a.lastLoad = time.Now().Add(time.Hour)
				if source == "link" {
					if err := os.WriteFile(filepath.Join(a.appDir, query.LinkName), []byte("occupied"), 0600); err != nil {
						t.Fatal(err)
					}
					a.linkErr = query.Link(a.appDir, filepath.Join(a.appDir, "app"))
					if !errors.Is(a.linkErr, query.ErrLinkOccupied) {
						t.Fatalf("链接错误: %v", a.linkErr)
					}
					a.legacyChecked = true
				} else {
					path := filepath.Join(a.appDir, "not-a-directory")
					if err := os.WriteFile(path, []byte("blocked"), 0600); err != nil {
						t.Fatal(err)
					}
					a.legacySkillDir = func() string { return path }
					a.checkLegacy(locales.In(lang))
					if a.legacyErr == nil {
						t.Fatal("旧文件检查应当失败")
					}
				}
				tt := ui.NewTester(a.view, 780, 3000)
				tt.SetLocale(lang)
				msg := locales.In(lang)
				if source == "link" {
					want := msg.GeneralAgentLinkError(errorText(lang, a.linkErr))
					if !tt.HasText(want) {
						t.Fatalf("链接错误未显示：%q", tt.Texts())
					}
					checkPageLanguage(t, tt, a, lang)
				} else {
					want := msg.GeneralAgentLegacyError(errorText(lang, a.legacyErr))
					if !tt.HasText(want) {
						t.Fatalf("旧文件错误未显示：%q", tt.Texts())
					}
					checkPageLanguage(t, tt, a, lang, a.legacyErr.Error())
				}
			})
		}
	}
}

func TestConnectionSuccessLanguageSwitch(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	pc := config.ProviderConfig{Preset: "custom", Label: "provider", ChatBase: "https://example.test", DefaultModel: "test-model"}
	if err := a.cfg.Update(func(c *config.Config) { c.Language = "zh-Hans"; c.Providers = []config.ProviderConfig{pc} }); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	a.tab, a.settingsTab, a.openProvider = 1, 1, providerKey(pc)
	a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"test-model","choices":[{"message":{"content":"ok"}}]}`)), Header: make(http.Header)}, nil
	})
	jobs := make(chan func(), 1)
	a.post = func(f func()) { jobs <- f }
	a.testConnection(0)
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("连接测试超时")
	}
	if !a.testOK {
		t.Fatalf("连接测试失败: %v", a.testErr)
	}
	tt := ui.NewTester(a.view, 780, 3000)
	for _, lang := range []string{"zh-Hans", "en"} {
		tt.SetLocale(lang)
		checkPageLanguage(t, tt, a, lang, "provider", "test-model")
	}
}
