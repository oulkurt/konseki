//go:build windows

/*
 * [INPUT]: 依赖 Windows 进程号与系统调用。
 * [OUTPUT]: 提供 Windows 平台下子进程存活检测辅助函数。
 * [POS]: live 测试套件的跨平台进程存活判定实现（Windows）。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"syscall"
)

const (
	processQueryLimitedInfo = 0x1000
	stillActive             = 259
)

func processAlive(pid int) (bool, error) {
	h, err := syscall.OpenProcess(processQueryLimitedInfo, false, uint32(pid))
	if err != nil {
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return true, nil
		}
		return false, nil
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false, err
	}
	return code == stillActive, nil
}
