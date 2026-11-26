//go:build unix

/*
 * [INPUT]: 依赖摘要目录路径。
 * [OUTPUT]: 提供跨实例和跨进程的摘要目录独占文件锁。
 * [POS]: Unix 平台下 Store 读-改-写提交边界的进程锁。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package summary

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockStoreFile(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
