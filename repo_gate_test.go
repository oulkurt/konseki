/*
 * [INPUT]: 依赖 Git 已跟踪文件、Go 源码和目录地图。
 * [OUTPUT]: 验证文件长度、契约和三级文档的成员清单。
 * [POS]: 根模块的仓库门禁；不访问应用数据。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const repoProtocol = "[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md"

func trackedFiles(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("Git 或工作树不可用: %v", err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

func repoRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Errorf("%s: 读取失败: %v", name, err)
	}
	return b
}

func generatedGo(b []byte) bool {
	lines := bytes.SplitN(b, []byte("\n"), 11)
	if len(lines) > 10 {
		lines = lines[:10]
	}
	head := bytes.Join(lines, []byte("\n"))
	return bytes.Contains(head, []byte("Code generated")) && bytes.Contains(head, []byte("DO NOT EDIT"))
}

func TestRepoGoLength(t *testing.T) {
	for _, f := range trackedFiles(t) {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		b := repoRead(t, f)
		if generatedGo(b) {
			continue
		}
		lines := bytes.Count(b, []byte("\n"))
		if len(b) > 0 && b[len(b)-1] != '\n' {
			lines++
		}
		if lines > 800 {
			t.Errorf("%s: 手写 Go 文件有 %d 行，超过 800 行", f, lines)
		}
	}
}

func TestRepoGoContracts(t *testing.T) {
	for _, f := range trackedFiles(t) {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		b := repoRead(t, f)
		if generatedGo(b) {
			continue
		}
		fs := token.NewFileSet()
		ast, err := parser.ParseFile(fs, f, b, parser.PackageClauseOnly|parser.ParseComments)
		if err != nil {
			t.Errorf("%s: 无法读取 package 子句: %v", f, err)
			continue
		}
		var comments strings.Builder
		for _, group := range ast.Comments {
			if group.End() < ast.Package {
				for _, c := range group.List {
					comments.WriteString(c.Text)
					comments.WriteByte('\n')
				}
			}
		}
		for _, tag := range []string{"[INPUT]", "[OUTPUT]", "[POS]", repoProtocol} {
			if !strings.Contains(comments.String(), tag) || tag == repoProtocol && !protocolLine(comments.String()) {
				t.Errorf("%s: package 前的注释缺少 %s", f, tag)
			}
		}
	}
}

func TestRepoGoDirectoryMaps(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range trackedFiles(t) {
		if strings.HasSuffix(f, ".go") {
			seen[path.Dir(f)] = true
		}
	}
	for dir := range seen {
		name := path.Join(dir, "CLAUDE.md")
		if _, err := os.Stat(name); err != nil {
			t.Errorf("%s: 含已跟踪 Go 文件的目录缺少 CLAUDE.md: %v", dir, err)
		}
	}
}

func protocolLine(body string) bool {
	pattern := `(?m)^\s*(?://|\*)?\s*` + regexp.QuoteMeta(repoProtocol) + `\s*(?:\*/)?\s*$`
	return regexp.MustCompile(pattern).MatchString(body)
}

// mapContains 只接受独立成员名，避免短名称匹配其他文件名。
func mapContains(body, member string) bool {
	pattern := `(^|[^[:alnum:]_.-])` + regexp.QuoteMeta(member) + `($|[^[:alnum:]_.-])`
	return regexp.MustCompile(pattern).MatchString(body)
}

func TestRepoModuleMaps(t *testing.T) {
	files := trackedFiles(t)
	for _, f := range files {
		if f == "CLAUDE.md" || path.Base(f) != "CLAUDE.md" {
			continue
		}
		body := string(repoRead(t, f))
		if !regexp.MustCompile(`(?m)^> L2 \| 父级: .+`).MatchString(body) {
			t.Errorf("%s: 缺少 L2 父级行", f)
		}
		if !protocolLine(body) {
			t.Errorf("%s: 缺少协议行", f)
		}
		var content []string
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasPrefix(line, "[PROTOCOL]") {
				content = append(content, line)
			}
		}
		body = strings.Join(content, "\n")
		dir := path.Dir(f)
		members := map[string]bool{}
		for _, candidate := range files {
			if strings.HasPrefix(candidate, dir+"/") {
				members[strings.SplitN(strings.TrimPrefix(candidate, dir+"/"), "/", 2)[0]] = true
			}
		}
		for member := range members {
			if !mapContains(body, member) {
				t.Errorf("%s: 成员清单缺少 %s", f, member)
			}
		}
	}
}

func TestRepoRootMap(t *testing.T) {
	body := string(repoRead(t, "CLAUDE.md"))
	members := map[string]bool{}
	for _, f := range trackedFiles(t) {
		parts := strings.SplitN(f, "/", 2)
		if len(parts) == 1 && strings.HasSuffix(f, ".go") {
			members[f] = true
		} else if len(parts) == 2 && !strings.HasPrefix(parts[0], ".") {
			members[parts[0]] = true
		}
	}
	var names []string
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !mapContains(body, name) {
			t.Errorf("CLAUDE.md: 根目录清单缺少 %s", name)
		}
	}
}

func TestRepoResourcesNoMap(t *testing.T) {
	trackedFiles(t)
	if _, err := os.Stat("resources/CLAUDE.md"); err == nil {
		t.Error("resources/CLAUDE.md: 资源目录不能包含文档地图")
	} else if !os.IsNotExist(err) {
		t.Errorf("resources/CLAUDE.md: 无法检查: %v", err)
	}
}
