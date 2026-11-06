/*
 * [INPUT]: 依赖 KONSEKI_HOME、runCLI 和查询命令入口。
 * [OUTPUT]: 验证查询命令分流、帮助输出和普通启动参数不被接管。
 * [POS]: 进程入口分流的回归测试，不启动窗口。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRunCLIQuery(t *testing.T) {
	t.Setenv("KONSEKI_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code, ok := runCLI([]string{"query", "status"}, &stdout, &stderr)
	if !ok || code != 0 {
		t.Fatalf("ok=%v code=%d stderr=%s", ok, code, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, stdout.String())
	}
	if out["dataDir"] == "" {
		t.Fatalf("缺少 dataDir: %s", stdout.String())
	}
}

func TestRunCLIHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		var stdout, stderr bytes.Buffer
		code, ok := runCLI(args, &stdout, &stderr)
		if !ok || code != 0 {
			t.Fatalf("%v: ok=%v code=%d", args, ok, code)
		}
		if !bytes.Contains(stdout.Bytes(), []byte("usage: konseki query")) {
			t.Fatalf("%v: stdout = %q", args, stdout.String())
		}
	}
}

func TestRunCLIIgnoresAppLaunch(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"status"}, {"--verbose"}} {
		var stdout, stderr bytes.Buffer
		if code, ok := runCLI(args, &stdout, &stderr); ok || code != 0 {
			t.Fatalf("%v 被当成了查询命令", args)
		}
	}
}
