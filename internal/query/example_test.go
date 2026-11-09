/*
 * [INPUT]: 依赖两天夹具和 KONSEKI_QUERY_EXAMPLE。
 * [OUTPUT]: 把五条命令和原样输出写进指定文件，供人工验收。
 * [POS]: 查询命令的样例产出工具，未设置环境变量时跳过。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryExample(t *testing.T) {
	path := os.Getenv("KONSEKI_QUERY_EXAMPLE")
	if path == "" {
		t.Skip("KONSEKI_QUERY_EXAMPLE 未设置")
	}
	f := standardFixture(t)
	commands := [][]string{
		{"status"},
		{"days"},
		{"day"},
		{"snapshots", "--ocr", "--limit", "3"},
		{"search", "alpha"},
	}
	var b strings.Builder
	for _, args := range commands {
		out, stderr, code := runQuery(t, f.env, args...)
		if code != 0 {
			t.Fatalf("%v 退出码 %d: %s", args, code, stderr)
		}
		fmt.Fprintf(&b, "$ konseki query %s\n%s", strings.Join(args, " "), out)
		if !strings.HasSuffix(out, "\n") {
			b.WriteString("\n")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
