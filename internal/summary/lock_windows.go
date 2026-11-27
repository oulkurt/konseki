//go:build windows

/*
 * [INPUT]: 依赖摘要目录路径与 Windows kernel32 文件锁原语。
 * [OUTPUT]: 提供进程退出由系统自动释放的摘要目录独占锁。
 * [POS]: Windows 平台下 Store 读-改-写提交边界的文件锁。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package summary

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32Summary     = syscall.NewLazyDLL("kernel32.dll")
	procLockSummaryEx   = kernel32Summary.NewProc("LockFileEx")
	procUnlockSummaryEx = kernel32Summary.NewProc("UnlockFileEx")
)

const summaryLockExclusive = 0x00000002

func lockStoreFile(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var ol syscall.Overlapped
	r1, _, callErr := procLockSummaryEx.Call(uintptr(f.Fd()), summaryLockExclusive, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r1 == 0 {
		_ = f.Close()
		return nil, callErr
	}
	return func() {
		var uol syscall.Overlapped
		_, _, _ = procUnlockSummaryEx.Call(uintptr(f.Fd()), 0, 1, 0, uintptr(unsafe.Pointer(&uol)))
		_ = f.Close()
	}, nil
}
