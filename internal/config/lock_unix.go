//go:build unix

/*
 * [INPUT]: 依赖配置目录路径。
 * [OUTPUT]: 提供跨实例和跨进程的配置目录独占文件锁。
 * [POS]: Unix 平台下配置加载与双文件提交边界的目录锁。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockConfigDir(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".config.lock"), os.O_CREATE|os.O_RDWR, 0o600)
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
