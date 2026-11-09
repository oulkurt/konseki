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
	path := filepath.Join(dir, LinkName)
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return os.Symlink(exe, path)
	case err != nil:
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%w: %s", ErrLinkOccupied, path)
	}
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if filepath.Clean(resolve(dir, target)) == filepath.Clean(exe) {
		return nil
	}
	tmpDir, err := os.MkdirTemp(dir, ".konseki-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmp := filepath.Join(tmpDir, LinkName)
	if err := os.Symlink(exe, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func resolve(dir, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(dir, target)
}
