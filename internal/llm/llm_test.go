/*
 * [INPUT]: 依赖本地 httptest 服务、模型客户端与供应商配置。
 * [OUTPUT]: 验证流式取消、协议对齐、URL 拼接、鉴权头、thinking 守卫与端点解析。
 * [POS]: 模型客户端与端点解析的离线回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"konseki/internal/config"
	"konseki/internal/providers"
)

func TestStreamCancelReturnsError(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"part\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := c.Stream(ctx, Request{
		Kind:    OpenAICompatible,
		BaseURL: srv.URL + "/v1",
		Model:   "m",
		Prompt:  "hi",
	}, nil)
	if !errors.Is(err, ErrConnection) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected ErrConnection wrapping context.Canceled, got %v", err)
	}
}

func TestStreamOpenAIAlignmentAndAnthropicFallback(t *testing.T) {
	var captured struct {
		ReasoningEffort string          `json:"reasoning_effort"`
		Temperature     *float64        `json:"temperature"`
		ResponseFormat  *responseFormat `json:"response_format"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"model\":\"gpt-x\",\"choices\":[{\"delta\":{\"content\":\"hello \"},\"index\":0},{\"delta\":{\"content\":\"ALT\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(10 * time.Second)
	var deltas []string
	resp, err := c.Stream(context.Background(), Request{
		Kind:        OpenAICompatible,
		BaseURL:     srv.URL + "/v1",
		Model:       "gpt-x",
		Prompt:      "hi",
		Temperature: 0.7,
		Thinking:    "high",
		JSON:        true,
	}, func(d string) {
		deltas = append(deltas, d)
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if resp.Text != "hello world" {
		t.Fatalf("resp.Text = %q, want %q", resp.Text, "hello world")
	}
	if !reflect.DeepEqual(deltas, []string{"hello ", "world"}) {
		t.Fatalf("deltas = %v", deltas)
	}
	if captured.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", captured.ReasoningEffort)
	}
	if captured.Temperature == nil || *captured.Temperature != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", captured.Temperature)
	}
	if captured.ResponseFormat == nil || captured.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %+v", captured.ResponseFormat)
	}

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"rate limited\"}}\n\n")
	}))
	defer errSrv.Close()
	_, err = c.Stream(context.Background(), Request{Kind: OpenAICompatible, BaseURL: errSrv.URL, Model: "m", Prompt: "hi"}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Body != "rate limited" {
		t.Fatalf("expected APIError with rate limited, got %v", err)
	}

	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer emptySrv.Close()
	_, err = c.Stream(context.Background(), Request{Kind: OpenAICompatible, BaseURL: emptySrv.URL, Model: "m", Prompt: "hi"}, nil)
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("expected ErrEmptyResponse, got %v", err)
	}

	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"claude","content":[{"type":"text","text":"anth-ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer anthSrv.Close()
	var anthDelta string
	anthResp, err := c.Stream(context.Background(), Request{Kind: AnthropicMessages, BaseURL: anthSrv.URL, Model: "claude", Prompt: "hi"}, func(d string) {
		anthDelta += d
	})
	if err != nil {
		t.Fatalf("Anthropic fallback Stream failed: %v", err)
	}
	if anthDelta != "anth-ok" || anthResp.Text != "anth-ok" {
		t.Fatalf("Anthropic delta=%q text=%q", anthDelta, anthResp.Text)
	}
}

func TestEndpointAndAnthropicURLAssembly(t *testing.T) {
	cases := []struct {
		base, path, want string
	}{
		{"https://api.anthropic.com", "v1/messages", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/", "v1/messages", "https://api.anthropic.com/v1/messages"},
		{"https://proxy.example/v1", "v1/messages", "https://proxy.example/v1/messages"},
		{"https://proxy.example/v1/", "v1/messages", "https://proxy.example/v1/messages"},
		{"https://proxy.example/v1/messages", "v1/messages", "https://proxy.example/v1/messages"},
		{"https://proxy.example/anthropic", "v1/messages", "https://proxy.example/anthropic/v1/messages"},
		{"https://res.openai.azure.com/openai/deployments/d?api-version=2024-02-01", "chat/completions", "https://res.openai.azure.com/openai/deployments/d/chat/completions?api-version=2024-02-01"},
		{"https://res.openai.azure.com/openai/deployments/d/chat/completions?api-version=2024-02-01", "chat/completions", "https://res.openai.azure.com/openai/deployments/d/chat/completions?api-version=2024-02-01"},
	}
	for _, tc := range cases {
		if got := endpoint(tc.base, tc.path); got != tc.want {
			t.Errorf("endpoint(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}

	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
			return
		}
		fmt.Fprint(w, `{"model":"c","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	c := New(10 * time.Second)
	for _, base := range []string{srv.URL, srv.URL + "/v1", srv.URL + "/v1/messages"} {
		if _, err := c.Complete(context.Background(), Request{Kind: AnthropicMessages, BaseURL: base, Model: "c", Prompt: "hi"}); err != nil {
			t.Fatalf("Complete(%q): %v", base, err)
		}
	}
	for i, got := range paths {
		if got != "/v1/messages" {
			t.Fatalf("call %d path = %q, want /v1/messages", i, got)
		}
	}

	paths = nil
	if _, err := c.ListModels(context.Background(), srv.URL+"/v1/chat/completions?api-version=1", "k", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListModels(context.Background(), srv.URL+"/v1/messages", "k", nil); err != nil {
		t.Fatal(err)
	}
	wantList := []string{"/v1/models?api-version=1", "/v1/models"}
	if !reflect.DeepEqual(paths, wantList) {
		t.Fatalf("ListModels paths = %v, want %v", paths, wantList)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAnthropicPresetListModelsPathAndHeaders(t *testing.T) {
	preset, ok := providers.ByID("anthropic")
	if !ok {
		t.Fatal("missing anthropic preset")
	}
	pc := config.ProviderConfig{
		Preset:   preset.ID,
		APIKey:   "sk-ant-test",
		ChatBase: preset.Chat,
	}
	resolved, err := ResolveEndpoint(pc, true)
	if err != nil {
		t.Fatal(err)
	}
	var gotURL, gotXAPIKey, gotVersion, gotAuth string
	c := New(10 * time.Second)
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotXAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"claude-sonnet-4-5"}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	ids, err := c.ListModelsForKind(context.Background(), resolved.Kind, resolved.BaseURL, pc.APIKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://api.anthropic.com/v1/models" {
		t.Fatalf("Anthropic ListModels URL = %q, want https://api.anthropic.com/v1/models", gotURL)
	}
	if gotXAPIKey != "sk-ant-test" || gotVersion != "2023-06-01" || gotAuth != "" {
		t.Fatalf("unexpected Anthropic headers: x-api-key=%q anthropic-version=%q auth=%q", gotXAPIKey, gotVersion, gotAuth)
	}
	if !reflect.DeepEqual(ids, []string{"claude-sonnet-4-5"}) {
		t.Fatalf("ids = %v", ids)
	}
}

func TestAuthHeadersAnthropicVersionAndAzure(t *testing.T) {
	var seenAnthVersion, seenXAPIKey, seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAnthVersion = r.Header.Get("anthropic-version")
		seenXAPIKey = r.Header.Get("x-api-key")
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/messages" {
			fmt.Fprint(w, `{"model":"c","content":[{"type":"text","text":"ok"}]}`)
			return
		}
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
			return
		}
		fmt.Fprint(w, `{"model":"g","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := New(10 * time.Second)
	if _, err := c.Complete(context.Background(), Request{Kind: AnthropicMessages, BaseURL: srv.URL, Model: "c", Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	if seenAnthVersion != "2023-06-01" || seenXAPIKey != "" || seenAuth != "" {
		t.Fatalf("empty-key anthropic headers: version=%q x-api-key=%q auth=%q", seenAnthVersion, seenXAPIKey, seenAuth)
	}

	if _, err := c.Complete(context.Background(), Request{
		Kind:    OpenAICompatible,
		BaseURL: srv.URL + "/v1",
		APIKey:  "secret-key",
		Header:  map[string]string{"api-key": "az-key"},
		Model:   "g",
		Prompt:  "hi",
	}); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "" {
		t.Fatalf("Azure request must not send Authorization header, got %q", seenAuth)
	}

	if _, err := c.ListModels(context.Background(), srv.URL+"/v1", "secret-key", map[string]string{"api-key": "az-key"}); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "" {
		t.Fatalf("Azure ListModels must not send Authorization header, got %q", seenAuth)
	}
}

func TestAnthropicThinkingBudgetGuard(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"c","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	c := New(10 * time.Second)
	for _, tc := range []struct {
		thinking string
		maxTok   int
		wantOn   bool
	}{
		{"invalid-level", 4096, false},
		{"off", 4096, false},
		{"", 4096, false},
		{"low", 2048, false},
		{"low", 4096, true},
		{"max", 4096, true},
	} {
		_, err := c.Complete(context.Background(), Request{
			Kind:        AnthropicMessages,
			BaseURL:     srv.URL,
			Model:       "c",
			Prompt:      "hi",
			MaxTokens:   tc.maxTok,
			Temperature: 0.5,
			Thinking:    tc.thinking,
		})
		if err != nil {
			t.Fatal(err)
		}
		got := bodies[len(bodies)-1]
		_, hasThinking := got["thinking"]
		if hasThinking != tc.wantOn {
			t.Fatalf("thinking=%q maxTok=%d: hasThinking=%v, want %v (body=%v)", tc.thinking, tc.maxTok, hasThinking, tc.wantOn, got)
		}
		if tc.wantOn {
			if _, hasTemp := got["temperature"]; hasTemp {
				t.Fatalf("thinking=%q must clear temperature", tc.thinking)
			}
		}
	}
}

func TestResolveAndResolveEndpoint(t *testing.T) {
	customNoModel := config.ProviderConfig{
		Preset:   "custom",
		Label:    "Local",
		ChatBase: "http://127.0.0.1:8080/v1",
	}
	ep, err := ResolveEndpoint(customNoModel, false)
	if err != nil {
		t.Fatalf("ResolveEndpoint without model failed: %v", err)
	}
	if ep.BaseURL != "http://127.0.0.1:8080/v1" || ep.Model != "" {
		t.Fatalf("unexpected ResolveEndpoint result: %+v", ep)
	}
	if _, err := Resolve(customNoModel, false); !errors.Is(err, ErrNoModel) {
		t.Fatalf("Resolve without model should return ErrNoModel, got %v", err)
	}

	rEnabled, err := Resolve(config.ProviderConfig{
		Preset:        "zhipu",
		Region:        "coding",
		EnabledModels: []string{"custom-glm"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rEnabled.Model != "custom-glm" {
		t.Fatalf("expected EnabledModels[0] fallback before preset.Models[0], got %q", rEnabled.Model)
	}

	rRegionModel, err := Resolve(config.ProviderConfig{
		Preset:        "volcengine",
		Region:        "coding",
		EnabledModels: []string{"user-model"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rRegionModel.Model != "ark-code-latest" {
		t.Fatalf("expected region.Models[0] before EnabledModels[0], got %q", rRegionModel.Model)
	}

	rDefaultModel, err := Resolve(config.ProviderConfig{
		Preset:        "volcengine",
		Region:        "coding",
		DefaultModel:  "pinned-model",
		EnabledModels: []string{"user-model"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rDefaultModel.Model != "pinned-model" {
		t.Fatalf("expected DefaultModel first, got %q", rDefaultModel.Model)
	}

	if _, err := ResolveEndpoint(config.ProviderConfig{
		Preset: "zhipu",
		Region: "nonexistent-region",
	}, false); !errors.Is(err, ErrNoEndpoint) || !strings.Contains(err.Error(), "nonexistent-region") {
		t.Fatalf("expected ErrNoEndpoint for unknown region on multi-region preset, got %v", err)
	}
}
