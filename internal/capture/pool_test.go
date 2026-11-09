/*
 * [INPUT]: 依赖采集模块的 Go 源码语法树。
 * [OUTPUT]: 检查自动释放池只由线程绑定边界创建。
 * [POS]: 原生调用约束的跨平台结构检查。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPoolOwnership(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var start, end token.Pos
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "withPool" && fn.Body != nil {
				start, end = fn.Body.Pos(), fn.Body.End()
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, _ := strconv.Unquote(lit.Value)
			if (strings.Contains(value, "NSAutoreleasePool") || strings.Contains(value, "objc_autoreleasePoolPush")) && (start == 0 || lit.Pos() < start || lit.End() > end) {
				t.Errorf("%s: 在 withPool 外创建池", name)
			}
			return true
		})
	}
}
