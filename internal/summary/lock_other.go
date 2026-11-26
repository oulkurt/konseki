//go:build !unix && !windows

/*
 * [INPUT]: 依赖摘要目录路径。
 * [OUTPUT]: 提供跨实例和跨进程的摘要目录独占锁回退实现。
 * [POS]: 非 Unix 非 Windows 平台下 Store 读-改-写提交边界的文件锁。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package summary

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

func lockStoreFile(dir string) (func(), error) {
	lockPath := filepath.Join(dir, ".lock")
	deadline := time.Now().Add(10 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
