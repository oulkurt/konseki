/*
 * [INPUT]: 依赖查询命令入口、设置目录、可执行文件和系统软链接。
 * [OUTPUT]: 提供 runCLI 与 linkExecutable。
 * [POS]: 进程入口的分流点。runCLI 只接管 query、help、-h、--help，其余参数照常启动界面。
 * linkExecutable 在启动时维护数据目录里的命令链接，返回原始错误，界面用 errorText 选文案。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"konseki/internal/config"
	"konseki/internal/query"
)

// runCLI 在第一个参数是查询命令时接管进程。
// 返回 ok=false 表示不是查询命令，调用方继续启动界面。
func runCLI(args []string, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	env := query.Env{Dir: config.AppDir(), Now: time.Now}
	switch args[0] {
	case "query":
		return query.Main(args[1:], env, stdout, stderr), true
	case "help", "-h", "--help":
		return query.Main(args, env, stdout, stderr), true
	}
	return 0, false
}

// linkExecutable 在数据目录里维护指向当前可执行文件的软链接。
// 失败时返回原始错误，不终止启动；界面用 errorText 按错误种类选文案。
func linkExecutable(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	return query.Link(dir, resolved)
}
