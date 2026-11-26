/*
 * [INPUT]: 依赖调用方提供的请求、密钥与上下文。
 * [OUTPUT]: 提供模型协议客户端、响应与可区分的错误种类。
 * [POS]: 模型服务的网络边界；摘要层负责脱敏。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Package llm 是和模型供应商说话的唯一出口。
//
// 只支持两种线上协议：
//   - OpenAI 兼容的 /chat/completions —— 覆盖了绝大多数供应商
//   - Anthropic 的 /messages —— Claude 自己的格式
//
// 其他格式（Gemini 原生、Bedrock SigV4、Vertex 的 OAuth）不进这一层。
// 那些供应商基本都提供了 OpenAI 兼容端点，用它比在这里重写四套签名
// 逻辑划算得多。
//
// 另一个不在这里做的事：脱敏。调用方（internal/summary）负责在把文本
// 递进来之前遮好，在这里还原。理由是一层只干一件事 —— 这里只管网络和
// 协议，出问题时能明确知道是「网断了」还是「字被遮坏了」。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Kind 是协议类型。
type Kind string

const (
	OpenAICompatible  Kind = "openai"
	AnthropicMessages Kind = "anthropic"
)

// Request 是一次模型调用。
type Request struct {
	Kind Kind

	// BaseURL 是完整的调用地址（含路径），由调用方按预设拼好。
	BaseURL string
	APIKey  string
	// Header 是额外请求头，Azure 的 api-key、OpenRouter 的 attribution 走这里。
	Header map[string]string

	Model  string
	System string
	Prompt string

	// MaxTokens 为 0 时按 4096 处理 —— 摘要用不到更长，但给足了余量。
	MaxTokens int
	// Temperature 为 0 时不下发这个字段，让各家的默认值生效。
	Temperature float64
	// Thinking 是推理档位，空或 "off" 表示不带。
	Thinking string
	// JSON 要求模型输出 JSON（能支持的供应商会开 response_format）。
	JSON bool
}

// Usage 是一次调用的 token 用量。
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Cached int `json:"cached,omitempty"`
}

// Response 是一次调用的结果。
type Response struct {
	Text  string
	Usage Usage
	Model string
}

// Client 是带超时和连接的 HTTP 客户端。
//
// 单独一个 Client 而不是用 http.DefaultClient：摘要任务可能同时跑
// 几个（今天 + 昨天），各自要有独立的超时预算。
type Client struct {
	HTTP *http.Client
}

// 错误种类供界面选择文案；错误文字只供日志使用。
var (
	ErrNoEndpoint      = errors.New("没有配置接口地址")
	ErrNoModel         = errors.New("还没有选模型")
	ErrInvalidResponse = errors.New("解析模型响应失败")
	ErrEmptyResponse   = errors.New("模型没有返回任何内容")
	ErrConnection      = errors.New("连不上模型接口")
)

// RequestError 保留错误种类与外部原因。
type RequestError struct {
	Kind  error
	Cause error
}

func (e *RequestError) Error() string        { return e.Kind.Error() + ": " + e.Cause.Error() }
func (e *RequestError) Unwrap() error        { return e.Cause }
func (e *RequestError) Is(target error) bool { return target == e.Kind }

// New 返回一个能用的客户端。timeout 为 0 时给 5 分钟 ——
// 一个小时的屏幕内容摘要，慢模型两步推理确实能跑到几分钟。
func New(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Client{HTTP: &http.Client{Timeout: timeout}}
}

// APIError 是供应商返回的非 2xx。
//
// 单独一个类型是为了让上层能区分「key 错了」（不要重试）和
// 「限流了」（可以等一会再试）。把状态码和响应体都带上，
// 用户在设置页点「测试」时能直接看到供应商到底在抱怨什么。
type APIError struct {
	Status int
	Body   string
}

// Detail 返回状态码和响应体。响应体按字符截到 400 个，不切断多字节字符。
// 界面把它当外部细节，跟在本地化前缀后面显示；Error 也用它，两处不各写一份截断。
func (e *APIError) Detail() string {
	body := strings.TrimSpace(e.Body)
	if r := []rune(body); len(r) > 400 {
		body = string(r[:400]) + "…"
	}
	switch {
	case e.Status == 0:
		return body
	case body == "":
		return fmt.Sprint(e.Status)
	default:
		return fmt.Sprintf("%d: %s", e.Status, body)
	}
}

func (e *APIError) Error() string { return "模型接口返回 " + e.Detail() }

// Retryable 说这个错误值得再试一次。
func (e *APIError) Retryable() bool {
	return e.Status == 429 || e.Status >= 500
}

// Complete 发起一次非流式调用。
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	switch req.Kind {
	case AnthropicMessages:
		return c.completeAnthropic(ctx, req)
	default:
		return c.completeOpenAI(ctx, req)
	}
}

// ---------- OpenAI 兼容 ----------

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model           string          `json:"model"`
	Messages        []openAIMessage `json:"messages"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			// 推理模型的思维链不在 Content 里。有些中转把它塞在
			// reasoning_content，我们不读 —— 摘要要的是结论。
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *Client) completeOpenAI(ctx context.Context, req Request) (*Response, error) {
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, ErrNoEndpoint
	}
	body := buildOpenAIRequest(req, false)

	raw, err := c.post(ctx, endpoint(req.BaseURL, "chat/completions"), req, body, false)
	if err != nil {
		return nil, err
	}

	var out openAIResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, &APIError{Body: out.Error.Message}
	}
	if len(out.Choices) == 0 {
		return nil, ErrEmptyResponse
	}
	return &Response{
		Text:  out.Choices[0].Message.Content,
		Model: out.Model,
		Usage: Usage{
			Input:  out.Usage.PromptTokens,
			Output: out.Usage.CompletionTokens,
			Cached: out.Usage.PromptTokensDetails.CachedTokens,
		},
	}, nil
}

func buildOpenAIRequest(req Request, stream bool) openAIRequest {
	body := openAIRequest{
		Model:     req.Model,
		MaxTokens: orDefault(req.MaxTokens, 4096),
		Stream:    stream,
	}
	if req.System != "" {
		body.Messages = append(body.Messages, openAIMessage{Role: "system", Content: req.System})
	}
	body.Messages = append(body.Messages, openAIMessage{Role: "user", Content: req.Prompt})
	if req.Temperature > 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	if req.Thinking != "" && req.Thinking != "off" {
		body.ReasoningEffort = req.Thinking
	}
	if req.JSON {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	return body
}

// ---------- Anthropic Messages ----------

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
	Thinking    *anthropicThinking `json:"thinking,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *Client) completeAnthropic(ctx context.Context, req Request) (*Response, error) {
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, ErrNoEndpoint
	}
	maxTok := orDefault(req.MaxTokens, 4096)
	body := anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTok,
		System:    req.System,
		Messages:  []anthropicMessage{{Role: "user", Content: req.Prompt}},
	}
	if req.Temperature > 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	// Anthropic 的 thinking 和 temperature 互斥，而且 budget 必须小于
	// max_tokens。给的是 max_tokens 的一半，够推理又不至于把答案挤掉。
	if budget := ThinkingBudget(req.Thinking); budget > 0 && maxTok/2 > 1024 {
		if maxTok/2 < budget {
			budget = maxTok / 2
		}
		body.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget}
		body.Temperature = nil
	}

	raw, err := c.post(ctx, endpoint(req.BaseURL, "v1/messages"), req, body, true)
	if err != nil {
		return nil, err
	}

	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, &APIError{Body: out.Error.Message}
	}
	var sb strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			sb.WriteString(blk.Text)
		}
	}
	if sb.Len() == 0 {
		return nil, ErrEmptyResponse
	}
	return &Response{
		Text:  sb.String(),
		Model: out.Model,
		Usage: Usage{
			Input:  out.Usage.InputTokens,
			Output: out.Usage.OutputTokens,
			Cached: out.Usage.CacheReadInputTokens,
		},
	}, nil
}

// ThinkingBudget 把档位换成 token 预算。
func ThinkingBudget(level string) int {
	switch strings.ToLower(level) {
	case "low":
		return 2048
	case "medium":
		return 4096
	case "high":
		return 8192
	case "max":
		return 16384
	}
	return 0
}

// ---------- 公共 ----------

func splitQuery(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return strings.TrimRight(raw[:i], "/"), raw[i:]
	}
	return strings.TrimRight(raw, "/"), ""
}

// endpoint 把 base 和路径拼起来，容忍两种常见写法：
// base 已经是完整的 .../chat/completions（中转站常这么给），或者只到 /v1。
func endpoint(base, path string) string {
	basePath, query := splitQuery(base)
	path = strings.TrimLeft(path, "/")
	if strings.HasSuffix(basePath, "/"+path) {
		return basePath + query
	}
	if strings.HasPrefix(path, "v1/") && strings.HasSuffix(basePath, "/v1") {
		return basePath + "/" + strings.TrimPrefix(path, "v1/") + query
	}
	return basePath + "/" + path + query
}

func hasHeader(h map[string]string, key string) bool {
	for k := range h {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

func applyAuthHeaders(h http.Header, apiKey string, extra map[string]string, anthropic bool) {
	if anthropic {
		h.Set("anthropic-version", "2023-06-01")
		if apiKey != "" {
			h.Set("x-api-key", apiKey)
		}
	} else if apiKey != "" && !hasHeader(extra, "api-key") {
		h.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range extra {
		h.Set(k, v)
	}
}

func (c *Client) post(ctx context.Context, url string, req Request, body any, anthropic bool) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyAuthHeaders(httpReq.Header, req.APIKey, req.Header, anthropic)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &RequestError{Kind: ErrConnection, Cause: err}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: string(raw)}
	}
	return raw, nil
}

// Stream 边收边给回调。onDelta 拿到的是增量文本。
//
// 只在 OpenAI 兼容协议上实现。Anthropic 的 SSE 事件格式不同，
// 而摘要场景本来就不需要流式 —— 用户要的是最后那段文字，
// 中间过程显示「思考中」就够了。留着这个函数是为了以后做
// 「边生成边显示摘要」时不用改结构。
func (c *Client) Stream(ctx context.Context, req Request, onDelta func(string)) (*Response, error) {
	if req.Kind == AnthropicMessages {
		resp, err := c.Complete(ctx, req)
		if err != nil {
			return nil, err
		}
		if onDelta != nil && resp.Text != "" {
			onDelta(resp.Text)
		}
		return resp, nil
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, ErrNoEndpoint
	}
	body := buildOpenAIRequest(req, true)

	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(req.BaseURL, "chat/completions"), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyAuthHeaders(httpReq.Header, req.APIKey, req.Header, false)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &RequestError{Kind: ErrConnection, Cause: ctx.Err()}
		}
		return nil, &RequestError{Kind: ErrConnection, Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, &APIError{Status: resp.StatusCode, Body: string(raw)}
	}

	var sb strings.Builder
	var nonData strings.Builder
	var sawData bool
	var usage Usage
	var model string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			if !sawData && line != "" && nonData.Len() < 64<<10 {
				nonData.WriteString(line)
			}
			continue
		}
		sawData = true
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 心跳或厂商自定义事件，跳过
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return nil, &APIError{Body: chunk.Error.Message}
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage.Input = chunk.Usage.PromptTokens
			usage.Output = chunk.Usage.CompletionTokens
			usage.Cached = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		if len(chunk.Choices) > 0 {
			if delta := chunk.Choices[0].Delta.Content; delta != "" {
				sb.WriteString(delta)
				if onDelta != nil {
					onDelta(delta)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, &RequestError{Kind: ErrConnection, Cause: err}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawData && nonData.Len() > 0 {
		var errBody struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(nonData.String()), &errBody) == nil && errBody.Error != nil && errBody.Error.Message != "" {
			return nil, &APIError{Body: errBody.Error.Message}
		}
	}
	if sb.Len() == 0 {
		return nil, ErrEmptyResponse
	}
	return &Response{Text: sb.String(), Usage: usage, Model: model}, nil
}

func (c *Client) ListModels(ctx context.Context, base, apiKey string, header map[string]string) ([]string, error) {
	basePath, _ := splitQuery(base)
	kind := OpenAICompatible
	if strings.HasSuffix(basePath, "/v1/messages") || strings.HasSuffix(basePath, "/messages") {
		kind = AnthropicMessages
	}
	return c.ListModelsForKind(ctx, kind, base, apiKey, header)
}

func (c *Client) ListModelsForKind(ctx context.Context, kind Kind, base, apiKey string, header map[string]string) ([]string, error) {
	basePath, query := splitQuery(base)
	if basePath == "" {
		return nil, ErrNoEndpoint
	}
	targetPath := "models"
	anthropic := false
	if kind == AnthropicMessages {
		for _, suffix := range []string{"/v1/messages", "/messages"} {
			if strings.HasSuffix(basePath, suffix) {
				basePath = strings.TrimSuffix(basePath, suffix)
				break
			}
		}
		targetPath = "v1/models"
		anthropic = true
	} else {
		basePath = strings.TrimSuffix(basePath, "/chat/completions")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(basePath+query, targetPath), nil)
	if err != nil {
		return nil, err
	}
	applyAuthHeaders(httpReq.Header, apiKey, header, anthropic)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &RequestError{Kind: ErrConnection, Cause: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: string(raw)}
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
