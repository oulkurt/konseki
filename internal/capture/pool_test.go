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
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, _ := strconv.Unquote(lit.Value)
				if (strings.Contains(value, "NSAutoreleasePool") || strings.Contains(value, "objc_autoreleasePoolPush")) && fn.Name.Name != "withPool" {
					t.Errorf("%s: %s 在 withPool 外创建池", name, fn.Name.Name)
				}
				return true
			})
		}
	}
}
