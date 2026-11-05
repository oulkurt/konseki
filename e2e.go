//go:build e2e

/**
 * [INPUT]: 依赖 MyGo 自动化插件和显式套接字环境变量。
 * [OUTPUT]: 在测试构建中注册窗口自动化服务。
 * [POS]: 根模块的测试构建入口，正式构建不包含插件。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import _ "github.com/egoist/mygo/plugins/automation"
