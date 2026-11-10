/*
 * [INPUT]: 依赖 llm.go 的 Request、Response、post 与错误种类。
 * [OUTPUT]: 提供 Gemini generateContent 协议的非流式调用与模型列表解析。
 * [POS]: llm 包的协议分支；Code Assist 订阅端点走这里。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

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
	Text string `json:"text"`
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
	Error        *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// completeGemini 调 Gemini 原生 :generateContent。
//
// 地址形状由 Code Assist 的 v1internal 决定：
// POST {base}:generateContent?model=...
// 模型名放 query 而不是 body，这是该端点的约定；换模型不用重解析端点。
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

	u := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if !strings.HasSuffix(u, ":generateContent") {
		u += ":generateContent"
	}
	u += "?model=" + url.QueryEscape(req.Model)

	raw, err := c.post(ctx, u, req, body)
	if err != nil {
		return nil, err
	}

	var out geminiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, &APIError{Body: out.Error.Message}
	}
	var sb strings.Builder
	for _, cand := range out.Candidates {
		for _, part := range cand.Content.Parts {
			sb.WriteString(part.Text)
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

// parseGeminiModels 解析 Gemini 的模型列表。
// 名字带 models/ 前缀，列表里还混着只做 embedding 的模型，都要处理。
func parseGeminiModels(raw []byte) ([]string, error) {
	var out struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &RequestError{Kind: ErrInvalidResponse, Cause: err}
	}
	ids := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		name := strings.TrimPrefix(m.Name, "models/")
		if name == "" || !supportsGenerate(m.SupportedGenerationMethods) {
			continue
		}
		ids = append(ids, name)
	}
	return ids, nil
}

// supportsGenerate 说这个模型能不能对话。字段缺失时收下：
// 有些版本不返回它，宁可在列表里多一个，也不要整页空白。
func supportsGenerate(methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		if m == "generateContent" {
			return true
		}
	}
	return false
}
