/*
 * [INPUT]: 依赖 llm.go 的 Request、Response、post 与错误种类。
 * [OUTPUT]: 提供 OpenAI Responses 协议的非流式调用。
 * [POS]: llm 包的协议分支；ChatGPT/Codex 订阅与 OpenAI 新版端点走这里。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"context"
	"encoding/json"
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
}

type responsesInput struct {
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

type responsesResponse struct {
	Model  string `json:"model"`
	Output []struct {
		Type    string             `json:"type"`
		Content []responsesContent `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
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

	raw, err := c.post(ctx, endpoint(req.BaseURL, "responses"), req, body)
	if err != nil {
		return nil, err
	}

	var out responsesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, &APIError{Body: out.Error.Message}
	}
	// reasoning item 也带 content，但它的块类型不是 output_text，直接跳过。
	var sb strings.Builder
	for _, item := range out.Output {
		for _, blk := range item.Content {
			if blk.Type == "output_text" {
				sb.WriteString(blk.Text)
			}
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return nil, ErrEmptyResponse
	}
	return &Response{
		Text:  sb.String(),
		Model: out.Model,
		Usage: Usage{
			Input:  out.Usage.InputTokens,
			Output: out.Usage.OutputTokens,
		},
	}, nil
}
