//go:build !unix && !windows

/*
 * [INPUT]: 依赖配置目录路径。
 * [OUTPUT]: 提供配置目录独占锁的非主流平台回退实现。
 * [POS]: 非 Unix 非 Windows 平台下配置提交边界的目录锁。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

func lockConfigDir(dir string) (func(), error) {
	lockPath := filepath.Join(dir, ".config.lock")
	deadline := time.Now().Add(10 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
