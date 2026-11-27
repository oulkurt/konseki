/*
 * [INPUT]: 依赖 llm.go 的 Request、Response、post 与错误种类。
 * [OUTPUT]: 提供公开 OpenAI Responses JSON 与 Codex 订阅 SSE 调用。
 * [POS]: llm 包的协议分支；ChatGPT/Codex 订阅与 OpenAI 新版端点走这里。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Responses 是 OpenAI 的新协议。和 chat/completions 的关键差异：
//   - 输入按 item 组织，用户文本块要标 input_text；
//   - 系统提示放 instructions，不占 messages；
//   - 输出可能混着 reasoning item，只取 message 里的 output_text。
type responsesRequest struct {
	Model           string              `json:"model"`
	Instructions    string              `json:"instructions,omitempty"`
	Input           []responsesInput    `json:"input"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
	Text            *responsesText      `json:"text,omitempty"`
	Stream          bool                `json:"stream"`
	Store           bool                `json:"store"`
}

type codexResponsesRequest struct {
	responsesRequest
	Tools             []json.RawMessage `json:"tools"`
	ToolChoice        string            `json:"tool_choice"`
	ParallelToolCalls bool              `json:"parallel_tool_calls"`
	Include           []string          `json:"include"`
}

type responsesInput struct {
	Type    string             `json:"type,omitempty"`
	Role    string             `json:"role"`
	Content []responsesContent `json:"content"`
}

type responsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesReasoning struct {
	Effort string `json:"effort"`
}

type responsesText struct {
	Format responsesFormat `json:"format"`
}

type responsesFormat struct {
	Type string `json:"type"`
}

type responsesItem struct {
	Type    string             `json:"type"`
	Content []responsesContent `json:"content"`
}

func (i responsesItem) appendText(out *strings.Builder) {
	if i.Type != "message" {
		return
	}
	for _, c := range i.Content {
		if c.Type == "output_text" {
			out.WriteString(c.Text)
		}
	}
}

type providerError struct {
	Message string `json:"message"`
}

type responsesResponse struct {
	Model  string          `json:"model"`
	Output []responsesItem `json:"output"`
	Usage  struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		InputDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
	Error             *providerError `json:"error"`
	IncompleteDetails struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

func (r responsesResponse) result(fallback string) (*Response, error) {
	if r.Error != nil && r.Error.Message != "" {
		return nil, &APIError{Body: r.Error.Message}
	}
	var text strings.Builder
	for _, item := range r.Output {
		item.appendText(&text)
	}
	output := text.String()
	if output == "" {
		output = fallback
	}
	if strings.TrimSpace(output) == "" {
		return nil, ErrEmptyResponse
	}
	return &Response{
		Text:  output,
		Model: r.Model,
		Usage: Usage{
			Input:  r.Usage.InputTokens,
			Output: r.Usage.OutputTokens,
			Cached: r.Usage.InputDetails.CachedTokens,
		},
	}, nil
}

func (c *Client) completeResponses(ctx context.Context, req Request) (*Response, error) {
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, ErrNoEndpoint
	}
	body := responsesRequest{
		Model:           req.Model,
		Instructions:    req.System,
		MaxOutputTokens: orDefault(req.MaxTokens, 4096),
		Input: []responsesInput{{
			Role:    "user",
			Content: []responsesContent{{Type: "input_text", Text: req.Prompt}},
		}},
		Stream: false,
	}
	// off 与空都不下发：订阅端点上的默认档由服务端决定。
	if req.Thinking != "" && req.Thinking != "off" {
		body.Reasoning = &responsesReasoning{Effort: req.Thinking}
	}
	if req.JSON {
		body.Text = &responsesText{Format: responsesFormat{Type: "json_object"}}
	}

	url := endpoint(req.BaseURL, "responses")
	if req.AuthKind == "oauth" {
		body.Input[0].Type = "message"
		body.Instructions = firstNonEmpty(body.Instructions, "Respond to the user request.")
		body.MaxOutputTokens = 0
		body.Stream = true
		if req.JSON {
			body.Instructions += "\nReturn only a JSON object."
			body.Text = nil
		}
		stream, err := c.postResponse(ctx, url, req, codexResponsesRequest{
			responsesRequest: body,
			Tools:            []json.RawMessage{},
			ToolChoice:       "auto",
			Include:          []string{},
		}, "text/event-stream")
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		return readResponsesStream(stream)
	}

	raw, err := c.post(ctx, url, req, body)
	if err != nil {
		return nil, err
	}
	var out responsesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	return out.result("")
}

type responsesEvent struct {
	Type     string            `json:"type"`
	Delta    string            `json:"delta"`
	Item     responsesItem     `json:"item"`
	Response responsesResponse `json:"response"`
	Message  string            `json:"message"`
	Error    *providerError    `json:"error"`
}

func readResponsesStream(stream io.Reader) (*Response, error) {
	sc := bufio.NewScanner(stream)
	sc.Buffer(make([]byte, 64<<10), 32<<20)
	var data, items, deltas strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(value, " "))
			continue
		}
		if line != "" || data.Len() == 0 {
			continue
		}
		payload := data.String()
		data.Reset()
		if payload == "[DONE]" {
			break
		}
		var event responsesEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
		}
		switch event.Type {
		case "response.output_text.delta":
			deltas.WriteString(event.Delta)
		case "response.output_item.done":
			event.Item.appendText(&items)
		case "response.completed":
			fallback := items.String()
			if fallback == "" {
				fallback = deltas.String()
			}
			return event.Response.result(fallback)
		case "response.failed", "response.incomplete", "error":
			message := event.Message
			if event.Response.Error != nil {
				message = event.Response.Error.Message
			} else if event.Error != nil {
				message = event.Error.Message
			}
			return nil, &APIError{Body: firstNonEmpty(message, event.Response.IncompleteDetails.Reason, event.Type)}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: stream ended before response.completed", ErrInvalidResponse)
}
