//go:build unix

/*
 * [INPUT]: 依赖 Unix 进程号与系统调用。
 * [OUTPUT]: 提供 Unix 平台下子进程存活检测辅助函数。
 * [POS]: live 测试套件的跨平台进程存活判定实现（Unix）。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"syscall"
)

func processAlive(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, err
}
