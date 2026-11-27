/*
 * [INPUT]: 依赖 llm.go 的 Request、Response、post 与错误种类。
 * [OUTPUT]: 提供 Gemini generateContent 协议的非流式调用。
 * [POS]: llm 包的协议分支；Code Assist 订阅端点走这里。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"strings"
)

type codeAssistRequest struct {
	Model        string        `json:"model"`
	Project      string        `json:"project"`
	UserPromptID string        `json:"user_prompt_id"`
	Request      geminiRequest `json:"request"`
}

type geminiRequest struct {
	Contents          []geminiContent     `json:"contents"`
	SystemInstruction *geminiContent      `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerateConf `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

type geminiGenerateConf struct {
	MaxOutputTokens  int    `json:"maxOutputTokens,omitempty"`
	ResponseMIMEType string `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

type codeAssistResponse struct {
	Response geminiResponse `json:"response"`
	Error    *providerError `json:"error"`
}

func (c *Client) completeGemini(ctx context.Context, req Request) (*Response, error) {
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, ErrNoEndpoint
	}
	body := geminiRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: req.Prompt}},
		}},
		GenerationConfig: &geminiGenerateConf{MaxOutputTokens: orDefault(req.MaxTokens, 4096)},
	}
	// 空系统提示要整个省略；发一个空 parts 会被接口拒绝。
	if req.System != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.System}}}
	}
	if req.JSON {
		body.GenerationConfig.ResponseMIMEType = "application/json"
	}
	// Gemini 没有 reasoning 档位参数，Thinking 忽略。

	base, query := splitQuery(req.BaseURL)
	base = strings.TrimSuffix(base, ":generateContent")
	project, err := c.codeAssistProject(ctx, base+":loadCodeAssist"+query, req)
	if err != nil {
		return nil, err
	}
	raw, err := c.post(ctx, base+":generateContent"+query, req, codeAssistRequest{
		Model: req.Model, Project: project, UserPromptID: rand.Text(), Request: body,
	})
	if err != nil {
		return nil, err
	}

	var envelope codeAssistResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	if envelope.Error != nil {
		return nil, &APIError{Body: envelope.Error.Message}
	}
	out := envelope.Response
	var sb strings.Builder
	for _, cand := range out.Candidates {
		for _, part := range cand.Content.Parts {
			if !part.Thought {
				sb.WriteString(part.Text)
			}
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return nil, ErrEmptyResponse
	}
	return &Response{
		Text:  sb.String(),
		Model: out.ModelVersion,
		Usage: Usage{
			Input:  out.UsageMetadata.PromptTokenCount,
			Output: out.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

func (c *Client) codeAssistProject(ctx context.Context, url string, req Request) (string, error) {
	raw, err := c.post(ctx, url, req, map[string]map[string]string{
		"metadata": {"ideType": "IDE_UNSPECIFIED", "platform": "PLATFORM_UNSPECIFIED", "pluginType": "GEMINI"},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Project string         `json:"cloudaicompanionProject"`
		Error   *providerError `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	if out.Error != nil {
		return "", &APIError{Body: out.Error.Message}
	}
	if out.Project == "" {
		return "", &APIError{Body: "Code Assist did not return a project. Complete setup in Gemini CLI first."}
	}
	return out.Project, nil
}
