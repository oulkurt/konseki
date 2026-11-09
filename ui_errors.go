/*
 * [INPUT]: 依赖 internal/llm 与 internal/query 的错误种类、settings_danger.go 的 clearCountError 和双语目录。
 * [OUTPUT]: 提供 errorText，按错误种类返回当前语言的界面说明。
 * [POS]: 根模块的错误展示边界。页面在渲染时调用它，切换语言后错误说明跟着换。
 * 我们自己的错误只显示文案；外部细节跟在本地化前缀后面。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"errors"

	"konseki/internal/llm"
	"konseki/internal/query"
	"konseki/locales"
)

// errorText 把 err 换成 lang 的界面说明。
// 认不出的错误显示通用前缀，原文作为外部细节跟在后面。
func errorText(lang string, err error) string {
	msg := locales.In(lang)
	var api *llm.APIError
	var request *llm.RequestError
	var count clearCountError
	var timeout interface{ Timeout() bool }
	switch {
	case errors.As(err, &count):
		return msg.RecorderDeleteFailed(int(count))
	case errors.Is(err, llm.ErrNoEndpoint):
		return msg.ErrorNoEndpoint()
	case errors.Is(err, llm.ErrNoModel):
		return msg.ErrorNoModel()
	case errors.Is(err, llm.ErrInvalidResponse):
		return msg.ErrorInvalidResponse()
	case errors.Is(err, llm.ErrEmptyResponse):
		return msg.ErrorEmptyResponse()
	case errors.As(err, &api):
		return msg.ErrorProvider() + api.Detail()
	// 超时排在连接失败前面：慢模型最常见的失败是超时，原文是 Go 运行时的英文。
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
		return msg.ErrorTimeout()
	case errors.As(err, &request) && errors.Is(err, llm.ErrConnection):
		return msg.ErrorConnection() + request.Cause.Error()
	case errors.Is(err, query.ErrExecutableNotAbsolute):
		return msg.ErrorLinkNotAbsolute()
	case errors.Is(err, query.ErrLinkOccupied):
		return msg.ErrorLinkExists()
	default:
		return msg.ErrorUnknown() + err.Error()
	}
}
