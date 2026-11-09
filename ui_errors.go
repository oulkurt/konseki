/*
 * [INPUT]: 依赖模型与命令链接错误种类和双语目录。
 * [OUTPUT]: 提供界面错误说明和命令链接初始化。
 * [POS]: 根模块的错误展示边界；内部错误映射文案，外部错误保留细节。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"konseki/internal/llm"
	"konseki/internal/query"
	"konseki/locales"
)

func errorText(lang string, err error) string {
	msg := locales.In(lang)
	var api *llm.APIError
	var request *llm.RequestError
	var count clearCountError
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
		detail := strings.TrimSpace(api.Body)
		if len(detail) > 400 {
			detail = detail[:400] + "…"
		}
		if api.Status != 0 {
			if detail != "" {
				detail = fmt.Sprintf("%d: %s", api.Status, detail)
			} else {
				detail = fmt.Sprint(api.Status)
			}
		}
		return msg.ErrorProvider() + detail
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

// linkError 保留错误类型，供界面选择文案。
func linkError(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	return query.Link(dir, exe)
}
