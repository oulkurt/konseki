/*
 * [INPUT]: 依赖 httptest、协议请求体与响应样本。
 * [OUTPUT]: 验证 OpenAI Responses、Gemini 与鉴权头、请求体、解析和错误映射。
 * [POS]: llm 包的离线协议回归；不联网。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc) (*Client, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(5 * time.Second), srv.URL
}

func decodeJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("读取请求体失败: %v", err)
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Errorf("请求体不是 JSON: %v (%s)", err, raw)
		return nil
	}
	return out
}

func objField(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, _ := m[key].(map[string]any)
	if v == nil {
		t.Errorf("缺少对象字段 %q: %v", key, m)
	}
	return v
}

func sliceField(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, _ := m[key].([]any)
	if len(v) == 0 {
		t.Errorf("缺少数组字段 %q: %v", key, m)
	}
	return v
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, body)
}

func TestCompleteResponsesOAuthRequestAndParse(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("方法 = %s，期望 POST", r.Method)
		}
		if r.URL.Path != "/v1/responses" {
			t.Errorf("路径 = %s，期望 /v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("OAuth 请求不该带 x-api-key，得到 %q", got)
		}
		body := decodeJSON(t, r)
		if body["model"] != "gpt-5.2" {
			t.Errorf("model = %v", body["model"])
		}
		if body["instructions"] != "be brief" {
			t.Errorf("instructions = %v", body["instructions"])
		}
		if body["max_output_tokens"] != float64(512) {
			t.Errorf("max_output_tokens = %v", body["max_output_tokens"])
		}
		if body["stream"] != false {
			t.Errorf("stream = %v，期望 false", body["stream"])
		}
		if effort := objField(t, body, "reasoning")["effort"]; effort != "high" {
			t.Errorf("reasoning.effort = %v", effort)
		}
		format := objField(t, objField(t, body, "text"), "format")
		if format["type"] != "json_object" {
			t.Errorf("text.format.type = %v", format["type"])
		}
		input := sliceField(t, body, "input")[0].(map[string]any)
		if input["role"] != "user" {
			t.Errorf("input[0].role = %v", input["role"])
		}
		content := input["content"].([]any)[0].(map[string]any)
		if content["type"] != "input_text" || content["text"] != "hello" {
			t.Errorf("input[0].content[0] = %v", content)
		}
		writeJSON(w, `{"model":"gpt-5.2-2025","output":[
			{"type":"reasoning","content":[{"type":"reasoning_text","text":"thinking"}]},
			{"type":"message","content":[{"type":"output_text","text":"he"},{"type":"output_text","text":"llo"}]}
		],"usage":{"input_tokens":11,"output_tokens":7}}`)
	})

	resp, err := client.Complete(context.Background(), Request{
		Kind:      OpenAIResponses,
		BaseURL:   base + "/v1",
		APIKey:    "oauth-token",
		AuthKind:  "oauth",
		Model:     "gpt-5.2",
		System:    "be brief",
		Prompt:    "hello",
		MaxTokens: 512,
		Thinking:  "high",
		JSON:      true,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello" {
		t.Errorf("Text = %q，期望只拼接 output_text", resp.Text)
	}
	if resp.Model != "gpt-5.2-2025" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.Usage.Input != 11 || resp.Usage.Output != 7 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestCompleteResponsesOmitsReasoningWhenOff(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body := decodeJSON(t, r)
		if _, ok := body["reasoning"]; ok {
			t.Errorf("Thinking 为 off 时不该下发 reasoning: %v", body["reasoning"])
		}
		if _, ok := body["text"]; ok {
			t.Errorf("非 JSON 输出时不该下发 text: %v", body["text"])
		}
		writeJSON(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
	})
	_, err := client.Complete(context.Background(), Request{
		Kind:     OpenAIResponses,
		BaseURL:  base,
		Model:    "gpt-5.2",
		Prompt:   "hello",
		Thinking: "off",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestCompleteResponsesErrors(t *testing.T) {
	t.Run("error body", func(t *testing.T) {
		client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"error":{"message":"boom"}}`)
		})
		_, err := client.Complete(context.Background(), Request{Kind: OpenAIResponses, BaseURL: base, Model: "m", Prompt: "p"})
		var api *APIError
		if !errors.As(err, &api) || api.Body != "boom" {
			t.Fatalf("err = %v，期望 APIError(boom)", err)
		}
	})
	t.Run("non 2xx", func(t *testing.T) {
		client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			io.WriteString(w, "upstream down")
		})
		_, err := client.Complete(context.Background(), Request{Kind: OpenAIResponses, BaseURL: base, Model: "m", Prompt: "p"})
		var api *APIError
		if !errors.As(err, &api) || api.Status != http.StatusBadGateway {
			t.Fatalf("err = %v，期望 APIError(502)", err)
		}
	})
	t.Run("empty output", func(t *testing.T) {
		client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"output":[]}`)
		})
		_, err := client.Complete(context.Background(), Request{Kind: OpenAIResponses, BaseURL: base, Model: "m", Prompt: "p"})
		if !errors.Is(err, ErrEmptyResponse) {
			t.Fatalf("err = %v，期望 ErrEmptyResponse", err)
		}
	})
}

func TestCompleteGeminiOAuthRequestAndParse(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("方法 = %s，期望 POST", r.Method)
		}
		if r.URL.Path != "/v1internal:generateContent" {
			t.Errorf("路径 = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("model"); got != "gemini-2.5-pro" {
			t.Errorf("model 查询参数 = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-google-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("OAuth 请求不该带 x-api-key，得到 %q", got)
		}
		body := decodeJSON(t, r)
		for _, banned := range []string{"reasoning", "thinking"} {
			if _, ok := body[banned]; ok {
				t.Errorf("Gemini 不该下发 %q", banned)
			}
		}
		contents := sliceField(t, body, "contents")
		first := contents[0].(map[string]any)
		if first["role"] != "user" {
			t.Errorf("contents[0].role = %v", first["role"])
		}
		part := first["parts"].([]any)[0].(map[string]any)
		if part["text"] != "hello" {
			t.Errorf("contents[0].parts[0].text = %v", part["text"])
		}
		sys := objField(t, body, "systemInstruction")
		if sys["parts"].([]any)[0].(map[string]any)["text"] != "be brief" {
			t.Errorf("systemInstruction = %v", sys)
		}
		gen := objField(t, body, "generationConfig")
		if gen["maxOutputTokens"] != float64(512) {
			t.Errorf("maxOutputTokens = %v", gen["maxOutputTokens"])
		}
		if gen["responseMimeType"] != "application/json" {
			t.Errorf("responseMimeType = %v", gen["responseMimeType"])
		}
		writeJSON(w, `{"candidates":[{"content":{"parts":[{"text":"a"},{"text":"b"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3},"modelVersion":"gemini-2.5-pro"}`)
	})

	resp, err := client.Complete(context.Background(), Request{
		Kind:      GeminiGenerate,
		BaseURL:   base + "/v1internal",
		APIKey:    "fake-google-token",
		AuthKind:  "oauth",
		Model:     "gemini-2.5-pro",
		System:    "be brief",
		Prompt:    "hello",
		MaxTokens: 512,
		Thinking:  "high",
		JSON:      true,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "ab" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Model != "gemini-2.5-pro" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.Usage.Input != 5 || resp.Usage.Output != 3 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestCompleteGeminiOmitsEmptySystem(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body := decodeJSON(t, r)
		if _, ok := body["systemInstruction"]; ok {
			t.Errorf("空系统提示不该下发: %v", body["systemInstruction"])
		}
		gen := objField(t, body, "generationConfig")
		if _, ok := gen["responseMimeType"]; ok {
			t.Errorf("非 JSON 输出时不该下发 responseMimeType: %v", gen)
		}
		writeJSON(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	})
	_, err := client.Complete(context.Background(), Request{Kind: GeminiGenerate, BaseURL: base + "/v1internal", Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestCompleteGeminiErrors(t *testing.T) {
	t.Run("error body", func(t *testing.T) {
		client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"error":{"message":"quota"}}`)
		})
		_, err := client.Complete(context.Background(), Request{Kind: GeminiGenerate, BaseURL: base + "/v1internal", Model: "m", Prompt: "p"})
		var api *APIError
		if !errors.As(err, &api) || api.Body != "quota" {
			t.Fatalf("err = %v，期望 APIError(quota)", err)
		}
	})
	t.Run("empty candidates", func(t *testing.T) {
		client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"candidates":[]}`)
		})
		_, err := client.Complete(context.Background(), Request{Kind: GeminiGenerate, BaseURL: base + "/v1internal", Model: "m", Prompt: "p"})
		if !errors.Is(err, ErrEmptyResponse) {
			t.Fatalf("err = %v，期望 ErrEmptyResponse", err)
		}
	})
}

func TestAnthropicOAuthHeaders(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("OAuth 请求不该带 x-api-key，得到 %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		writeJSON(w, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	_, err := client.Complete(context.Background(), Request{
		Kind:     AnthropicMessages,
		BaseURL:  base,
		APIKey:   "oauth-token",
		AuthKind: "oauth",
		Model:    "claude-sonnet-5-5",
		Prompt:   "hi",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestAnthropicAPIKeyHeaders(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("apikey 模式不该带 Authorization，得到 %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "" {
			t.Errorf("apikey 模式不该带 anthropic-beta，得到 %q", got)
		}
		writeJSON(w, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	_, err := client.Complete(context.Background(), Request{
		Kind:    AnthropicMessages,
		BaseURL: base,
		APIKey:  "sk-ant",
		Model:   "claude-sonnet-5-5",
		Prompt:  "hi",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestListModelsOpenAIFormat(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("请求 = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(w, `{"data":[{"id":"m1"},{"id":""},{"id":"m2"}]}`)
	})
	ids, err := client.ListModels(context.Background(), Request{Kind: OpenAIResponses, BaseURL: base + "/v1", APIKey: "key"})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestListModelsGeminiFormat(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal/models" {
			t.Errorf("路径 = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-google-token" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(w, `{"models":[
			{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent","countTokens"]},
			{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]},
			{"name":"models/gemini-2.5-flash"}
		]}`)
	})
	ids, err := client.ListModels(context.Background(), Request{
		Kind:     GeminiGenerate,
		BaseURL:  base + "/v1internal",
		APIKey:   "fake-google-token",
		AuthKind: "oauth",
	})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"gemini-2.5-pro", "gemini-2.5-flash"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v，期望 %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v，期望 %v", ids, want)
		}
	}
}

func TestListModelsErrors(t *testing.T) {
	client, base := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "no")
	})
	_, err := client.ListModels(context.Background(), Request{BaseURL: base, APIKey: "bad"})
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v，期望 APIError(401)", err)
	}
	if _, err := client.ListModels(context.Background(), Request{}); !errors.Is(err, ErrNoEndpoint) {
		t.Fatalf("空 base 的 err = %v，期望 ErrNoEndpoint", err)
	}
}
