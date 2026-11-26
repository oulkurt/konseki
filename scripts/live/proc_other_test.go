//go:build !unix && !windows

/*
 * [INPUT]: 无平台系统调用依赖。
 * [OUTPUT]: 提供非 Unix 非 Windows 平台下的进程存活检测桩函数。
 * [POS]: live 测试套件的跨平台进程存活判定实现（通用桩）。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

func processAlive(int) (bool, error) {
	return false, nil
}
