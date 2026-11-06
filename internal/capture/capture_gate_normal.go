//go:build !e2e

/*
 * [INPUT]: 依赖 !e2e 构建标签，不读取环境。
 * [OUTPUT]: 恒允许正式构建的采集与权限调用。
 * [POS]: 正式构建的许可策略。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

func realCaptureAllowed() bool { return true }
