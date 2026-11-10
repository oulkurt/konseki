/*
 * [INPUT]: 依赖供应商预设与用户配置。
 * [OUTPUT]: 验证协议选择、订阅端点写死、候选模型过滤与既有厂商的解析回归。
 * [POS]: resolve.go 的离线回归测试；不联网。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"errors"
	"slices"
	"testing"

	"konseki/internal/config"
	"konseki/internal/providers"
)

func TestResolveCodexOAuthUsesSubscriptionResponses(t *testing.T) {
	r, err := Resolve(config.ProviderConfig{Preset: "openai", Auth: "oauth", DefaultModel: "gpt-5.2"}, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Kind != OpenAIResponses {
		t.Errorf("Kind = %q，期望 %q", r.Kind, OpenAIResponses)
	}
	if r.BaseURL != codexSubscriptionBase {
		t.Errorf("BaseURL = %q，期望 %q", r.BaseURL, codexSubscriptionBase)
	}
	if r.AuthKind != "oauth" {
		t.Errorf("AuthKind = %q", r.AuthKind)
	}
	if r.CallBase() != codexSubscriptionBase {
		t.Errorf("CallBase = %q", r.CallBase())
	}
}

func TestResolveOpenAIAPIKeyUsesResponses(t *testing.T) {
	r, err := Resolve(config.ProviderConfig{Preset: "openai", DefaultModel: "gpt-5.2"}, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Kind != OpenAIResponses {
		t.Errorf("Kind = %q，期望 %q", r.Kind, OpenAIResponses)
	}
	if r.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q", r.BaseURL)
	}
	if r.AuthKind != "apikey" {
		t.Errorf("AuthKind = %q，期望 apikey", r.AuthKind)
	}
}

func TestResolveOpenAIResponsesOverride(t *testing.T) {
	r, err := Resolve(config.ProviderConfig{Preset: "openai", ResponsesBase: "https://relay.test/v1", DefaultModel: "m"}, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Kind != OpenAIResponses || r.BaseURL != "https://relay.test/v1" {
		t.Fatalf("Kind = %q BaseURL = %q", r.Kind, r.BaseURL)
	}
}

func TestResolveGeminiCLIOAuth(t *testing.T) {
	r, err := Resolve(config.ProviderConfig{Preset: "gemini-cli", Auth: "oauth"}, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Kind != GeminiGenerate {
		t.Errorf("Kind = %q，期望 %q", r.Kind, GeminiGenerate)
	}
	if r.GeminiBase != geminiCLIBase {
		t.Errorf("GeminiBase = %q", r.GeminiBase)
	}
	if r.BaseURL != "" {
		t.Errorf("BaseURL = %q，期望留空", r.BaseURL)
	}
	if r.CallBase() != geminiCLIBase {
		t.Errorf("CallBase = %q", r.CallBase())
	}
	if r.Model != geminiCLIDefaultModel {
		t.Errorf("Model = %q，期望 %q", r.Model, geminiCLIDefaultModel)
	}
	if r.AuthKind != "oauth" {
		t.Errorf("AuthKind = %q", r.AuthKind)
	}
}

func TestResolveGoogleStaysOpenAICompatible(t *testing.T) {
	r, err := Resolve(config.ProviderConfig{Preset: "google", DefaultModel: "gemini-2.5-flash"}, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Kind != OpenAICompatible {
		t.Errorf("Kind = %q", r.Kind)
	}
	if r.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Errorf("BaseURL = %q", r.BaseURL)
	}
}

// 原有厂商的结果必须和引入新协议之前一致。
func TestResolveRegressionExistingVendors(t *testing.T) {
	cases := []struct {
		name            string
		pc              config.ProviderConfig
		preferAnthropic bool
		wantKind        Kind
		wantBase        string
		wantAuth        string
	}{
		{
			name:     "zhipu-apikey",
			pc:       config.ProviderConfig{Preset: "zhipu", DefaultModel: "glm-4.6"},
			wantKind: OpenAICompatible,
			wantBase: "https://open.bigmodel.cn/api/paas/v4",
			wantAuth: "apikey",
		},
		{
			name:     "deepseek-chat",
			pc:       config.ProviderConfig{Preset: "deepseek", DefaultModel: "deepseek-chat"},
			wantKind: OpenAICompatible,
			wantBase: "https://api.deepseek.com/v1",
			wantAuth: "apikey",
		},
		{
			name:            "deepseek-anthropic",
			pc:              config.ProviderConfig{Preset: "deepseek", DefaultModel: "deepseek-chat"},
			preferAnthropic: true,
			wantKind:        AnthropicMessages,
			wantBase:        "https://api.deepseek.com/anthropic",
			wantAuth:        "apikey",
		},
		{
			name:            "anthropic-apikey",
			pc:              config.ProviderConfig{Preset: "anthropic", DefaultModel: "claude-sonnet-5-5"},
			preferAnthropic: true,
			wantKind:        AnthropicMessages,
			wantBase:        "https://api.anthropic.com",
			wantAuth:        "apikey",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Resolve(tc.pc, tc.preferAnthropic)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if r.Kind != tc.wantKind || r.BaseURL != tc.wantBase || r.AuthKind != tc.wantAuth {
				t.Fatalf("Kind = %q BaseURL = %q AuthKind = %q，期望 %q %q %q",
					r.Kind, r.BaseURL, r.AuthKind, tc.wantKind, tc.wantBase, tc.wantAuth)
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	if _, err := Resolve(config.ProviderConfig{Preset: "custom", DefaultModel: "m"}, true); !errors.Is(err, ErrNoEndpoint) {
		t.Errorf("没有端点的 err = %v，期望 ErrNoEndpoint", err)
	}
	if _, err := Resolve(config.ProviderConfig{Preset: "custom", ChatBase: "https://example.test"}, true); !errors.Is(err, ErrNoModel) {
		t.Errorf("没有模型的 err = %v，期望 ErrNoModel", err)
	}
	// 订阅预设不走 API key 模式：没有公开端点可用。
	if _, err := Resolve(config.ProviderConfig{Preset: "gemini-cli", DefaultModel: "gemini-2.5-pro"}, true); !errors.Is(err, ErrNoEndpoint) {
		t.Errorf("gemini-cli/apikey 的 err = %v，期望 ErrNoEndpoint", err)
	}
}

// 有发现清单时勾选是唯一依据：取消勾选的预设模型不进候选，
// 只有拉取到的模型才能补进列表。
func TestResolveDiscoveredModelsFiltersByEnabled(t *testing.T) {
	preset, ok := providers.ByID("baidu-qianfan")
	if !ok || len(preset.Models) < 2 {
		t.Fatal("预设夹具失效：baidu-qianfan 需要至少两个模型")
	}
	kept, cancelled := preset.Models[0], preset.Models[1]
	extra := "discovered-only"
	pc := config.ProviderConfig{
		Preset:           preset.ID,
		EnabledModels:    []string{kept, extra},
		DiscoveredModels: append(slices.Clone(preset.Models), extra),
	}
	r, err := Resolve(pc, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := []string{kept, extra}; !slices.Equal(r.Models, want) {
		t.Fatalf("Models = %v，期望 %v", r.Models, want)
	}
	if slices.Contains(r.Models, cancelled) {
		t.Fatalf("取消勾选的预设模型 %q 仍在候选里：%v", cancelled, r.Models)
	}
}

// 没有发现清单时保持旧行为：EnabledModels 直接并入预设，空列表仍表示全用预设。
func TestResolveWithoutDiscoveredKeepsUnion(t *testing.T) {
	preset, ok := providers.ByID("baidu-qianfan")
	if !ok || len(preset.Models) == 0 {
		t.Fatal("预设夹具失效：baidu-qianfan 需要模型列表")
	}
	extra := "legacy-extra"
	r, err := Resolve(config.ProviderConfig{Preset: preset.ID, EnabledModels: []string{extra}}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := append(slices.Clone(preset.Models), extra)
	if !slices.Equal(r.Models, want) {
		t.Fatalf("Models = %v，期望 %v", r.Models, want)
	}
	empty, err := Resolve(config.ProviderConfig{Preset: preset.ID}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !slices.Equal(empty.Models, preset.Models) {
		t.Fatalf("空勾选的 Models = %v，期望预设全集 %v", empty.Models, preset.Models)
	}
}
