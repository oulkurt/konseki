/*
 * [INPUT]: 依赖独占临时目录和文件系统软链接操作。
 * [OUTPUT]: 提供命令链接写入与可区分的链接错误。
 * [POS]: 查询命令的入口链接。只在 App 启动时写入，命令本身不修改它。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	linkTempPrefix = ".konseki-link-"
	staleLinkAge   = 10 * time.Minute
	linkAttempts   = 3
)

var (
	ErrExecutableNotAbsolute = errors.New("query: executable path is not absolute")
	ErrLinkOccupied          = errors.New("query: link location is occupied")
)

// Link 在 dir 里维护指向 exe 的软链接。
//
// 已经指向 exe 时不动；指向别处时先在临时 path 建链接再改名，替换是原子的。
// 同名普通文件或目录不覆盖，返回错误。dir 不存在时报错，不创建目录。
func Link(dir, exe string) error {
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("%w: %q", ErrExecutableNotAbsolute, exe)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	exe = filepath.Clean(exe)
	path := filepath.Join(absDir, LinkName)
	for attempt := 0; attempt < linkAttempts; attempt++ {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			err = os.Symlink(exe, path)
			if err == nil {
				return nil
			}
			if errors.Is(err, os.ErrExist) && attempt+1 < linkAttempts {
				continue
			}
			return err
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%w: %s", ErrLinkOccupied, path)
		}
		target, err := os.Readlink(path)
		if errors.Is(err, os.ErrNotExist) && attempt+1 < linkAttempts {
			continue
		}
		if err != nil {
			return err
		}
		if filepath.Clean(resolve(absDir, target)) == exe {
			return nil
		}
		return replaceLink(absDir, path, exe)
	}
	return fmt.Errorf("query: failed to link %s", path)
}

func replaceLink(dir, path, exe string) error {
	cleanStaleLinkTempDirs(dir)
	tmpDir, err := os.MkdirTemp(dir, linkTempPrefix)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmp := filepath.Join(tmpDir, LinkName)
	if err := os.Symlink(exe, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cleanStaleLinkTempDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), linkTempPrefix) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if time.Since(info.ModTime()) < staleLinkAge {
			continue
		}
		sub, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		if len(sub) == 0 || (len(sub) == 1 && sub[0].Name() == LinkName && sub[0].Type()&os.ModeSymlink != 0) {
			_ = os.RemoveAll(p)
		}
	}
}

func resolve(dir, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(dir, target)
}
