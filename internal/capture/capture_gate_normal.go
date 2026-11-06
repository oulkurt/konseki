//go:build !e2e

/*
 * [INPUT]: 依赖构建标签和环境授权。
 * [OUTPUT]: 限制真实采集与权限请求。
 * [POS]: 平台入口的测试安全边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

func realCaptureAllowed() bool { return true }
