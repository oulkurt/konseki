/*
 * [INPUT]: 依赖脱敏设置、两天夹具和全部查询命令。
 * [OUTPUT]: 验证密钥、邮箱和自定义词在五条命令里都被遮蔽，放行值保留，总开关关闭时原样输出。
 * [POS]: query 包脱敏边界的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"konseki/internal/config"
	"konseki/internal/summary"
)

func sensitiveFixture(t *testing.T, enabled bool) (*testFixture, map[string]string) {
	t.Helper()
	cfg := config.Default()
	cfg.Redact.Enabled = enabled
	cfg.Redact.Words = []string{"ProjectFalcon"}
	cfg.Redact.Allow = []string{"allow@example.com"}

	secrets := map[string]string{
		"key":   "sk-ant-" + strings.Repeat("a1b2c3d4", 6),
		"email": "alice@example.com",
		"allow": "allow@example.com",
		"word":  "ProjectFalcon",
	}
	f := newFixture(t, cfg)
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	f.addSnapshot(at, "20261105-090000.000-0001", "Editor", "com.example.editor",
		secrets["word"]+" "+secrets["key"],
		"mail "+secrets["email"]+" allow "+secrets["allow"]+" alpha text", false)
	f.addSnapshot(at.Add(time.Hour), "20261105-100000.000-0002", "Editor", "com.example.editor",
		"plain", strings.Repeat("x", 55)+" "+secrets["key"]+" "+strings.Repeat("y", 20), false)
	f.addSnapshot(at.Add(2*time.Hour), "20261105-110000.000-0003", "Terminal", "com.apple.Terminal",
		`quote " backslash \ newline
next`, "say \"hi\" \\ done "+secrets["email"]+" alpha", false)
	f.addEntry(summary.Entry{
		ID: "2026-11-05-secret", Day: "2026-11-05", Start: at, End: at.Add(time.Hour),
		Headline: secrets["word"] + " 进展", Summary: "联系 " + secrets["email"],
		Topics: []string{secrets["key"]},
	})
	return f, secrets
}

func TestRedactionAcrossCommands(t *testing.T) {
	f, secrets := sensitiveFixture(t, true)
	for _, args := range [][]string{
		{"status"},
		{"days"},
		{"day"},
		{"snapshots", "--ocr"},
		{"snapshots", "--ocr", "--limit", "100"},
		{"search", "alpha"},
	} {
		out, stderr, code := runQuery(t, f.env, args...)
		if code != 0 {
			t.Fatalf("%v 退出码 %d: %s", args, code, stderr)
		}
		for name, value := range secrets {
			if name == "allow" {
				continue
			}
			if strings.Contains(out, value) {
				t.Fatalf("%v 输出了 %s: %s", args, name, out)
			}
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(out), &raw); err != nil {
			t.Fatalf("%v 输出不是合法 JSON: %v", args, err)
		}
	}
	out, _, _ := runQuery(t, f.env, "snapshots", "--ocr")
	if !strings.Contains(out, secrets["allow"]) {
		t.Fatalf("放行值被遮蔽了: %s", out)
	}
	for _, banned := range []string{"words", "allow", "ProjectFalcon", "alice"} {
		status, _, _ := runQuery(t, f.env, "status")
		if strings.Contains(status, banned) {
			t.Fatalf("status 输出了 %q: %s", banned, status)
		}
	}
}

func TestRedactionOffKeepsText(t *testing.T) {
	f, secrets := sensitiveFixture(t, false)
	out, stderr, code := runQuery(t, f.env, "snapshots", "--ocr")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	for name, value := range secrets {
		if !strings.Contains(out, value) {
			t.Fatalf("总开关关闭后仍遮了 %s: %s", name, out)
		}
	}
}

func TestRedactedSnippetHasNoHalfKey(t *testing.T) {
	f, secrets := sensitiveFixture(t, true)
	out, _, code := runQuery(t, f.env, "search", "xxxxxx")
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	key := secrets["key"]
	if strings.Contains(out, key) || strings.Contains(out, key[:12]) {
		t.Fatalf("片段里有密钥或半截密钥: %s", out)
	}
	var got searchOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || !strings.Contains(got.Matches[0].Snippet, "xxxxxx") {
		t.Fatalf("片段 = %+v", got.Matches)
	}
}
