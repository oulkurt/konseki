//go:build windows

/*
 * [INPUT]: 依赖配置目录路径与 Windows kernel32 文件锁原语。
 * [OUTPUT]: 提供进程退出由系统自动释放的独占配置目录锁。
 * [POS]: Windows 平台下配置加载与双文件提交边界的目录锁。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32Config     = syscall.NewLazyDLL("kernel32.dll")
	procLockConfigEx   = kernel32Config.NewProc("LockFileEx")
	procUnlockConfigEx = kernel32Config.NewProc("UnlockFileEx")
)

const configLockExclusive = 0x00000002

func lockConfigDir(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".config.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var ol syscall.Overlapped
	r1, _, callErr := procLockConfigEx.Call(uintptr(f.Fd()), configLockExclusive, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r1 == 0 {
		_ = f.Close()
		return nil, callErr
	}
	return func() {
		var uol syscall.Overlapped
		_, _, _ = procUnlockConfigEx.Call(uintptr(f.Fd()), 0, 1, 0, uintptr(unsafe.Pointer(&uol)))
		_ = f.Close()
	}, nil
}
