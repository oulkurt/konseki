/*
 * [INPUT]: 依赖临时 HOME 目录与伪造的 CLI 凭据文件。
 * [OUTPUT]: 验证 Gemini 凭据过滤与 Codex 模型目录的宽容解析。
 * [POS]: providers 包的本机发现离线测试；不读真实凭据。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package providers

import (
	"os"
	"path/filepath"
	"testing"
)

// writeHomeFile 在临时 HOME 下写一个文件，权限收窄到只读凭据该有的范围。
func writeHomeFile(t *testing.T, home, name, body string) {
	t.Helper()
	path := filepath.Join(home, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// isolateHome 把 HOME 换成空目录，并替换钥匙串读取，避免碰到开发者本机凭据。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	orig := readClaudeKeychain
	t.Cleanup(func() { readClaudeKeychain = orig })
	readClaudeKeychain = func() ([]byte, error) { return nil, os.ErrNotExist }
	return home
}

func findCredential(creds []Credential, source string) (Credential, bool) {
	for _, c := range creds {
		if c.Source == source {
			return c, true
		}
	}
	return Credential{}, false
}

func TestDetectGeminiCredential(t *testing.T) {
	home := isolateHome(t)
	writeHomeFile(t, home, ".gemini/oauth_creds.json", `{
		"access_token":"fake-gemini-access",
		"refresh_token":"fake-gemini-refresh",
		"expiry_date":1793071382993,
		"scope":"openid https://www.googleapis.com/auth/cloud-platform"
	}`)

	cred, ok := findCredential(DetectLocal(), "gemini")
	if !ok {
		t.Fatal("没有发现 Gemini 凭据")
	}
	if cred.Kind != "oauth" || cred.Label != "Gemini CLI" {
		t.Errorf("Kind = %q Label = %q", cred.Kind, cred.Label)
	}
	if cred.AccessToken != "fake-gemini-access" || cred.RefreshToken != "fake-gemini-refresh" {
		t.Errorf("令牌解析错误: %+v", cred)
	}
	if got := cred.ExpiresAt.UnixMilli(); got != 1793071382993 {
		t.Errorf("ExpiresAt = %d，期望 1793071382993", got)
	}
}

func TestDetectGeminiSkipsWithoutCloudPlatform(t *testing.T) {
	home := isolateHome(t)
	writeHomeFile(t, home, ".gemini/oauth_creds.json", `{
		"access_token":"fake-gemini-access",
		"expiry_date":1793071382993,
		"scope":"openid https://www.googleapis.com/auth/userinfo.email"
	}`)

	if _, ok := findCredential(DetectLocal(), "gemini"); ok {
		t.Fatal("没有 cloud-platform 权限的凭据不该被收下")
	}
}

func TestDetectGeminiAcceptsArrayScope(t *testing.T) {
	home := isolateHome(t)
	writeHomeFile(t, home, ".gemini/oauth_creds.json", `{
		"access_token":"fake-gemini-access",
		"expiry_date":"1793071382993",
		"scope":["openid","https://www.googleapis.com/auth/cloud-platform"]
	}`)

	cred, ok := findCredential(DetectLocal(), "gemini")
	if !ok {
		t.Fatal("数组形式的 scope 也应被识别")
	}
	if got := cred.ExpiresAt.UnixMilli(); got != 1793071382993 {
		t.Errorf("字符串时间戳解析 = %d", got)
	}
}

func TestCodexModelsReadsWrappedFile(t *testing.T) {
	home := isolateHome(t)
	writeHomeFile(t, home, ".codex/magpie-models.json", `{"models":[
		{"id":"gpt-5.2-codex"},
		{"slug":"auto/deepseek-v4.1-flash"},
		{"id":""},
		{"id":"gpt-5.2-codex"}
	]}`)

	got := CodexModels()
	want := []string{"gpt-5.2-codex", "auto/deepseek-v4.1-flash"}
	if len(got) != len(want) {
		t.Fatalf("CodexModels = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CodexModels = %v，期望 %v", got, want)
		}
	}
}

func TestCodexModelsReadsBareArray(t *testing.T) {
	home := isolateHome(t)
	writeHomeFile(t, home, ".codex/magpie-models.json", `[{"id":"gpt-5.2"}]`)

	got := CodexModels()
	if len(got) != 1 || got[0] != "gpt-5.2" {
		t.Fatalf("CodexModels = %v", got)
	}
}

func TestCodexModelsMissingFile(t *testing.T) {
	isolateHome(t)
	if got := CodexModels(); len(got) != 0 {
		t.Fatalf("没有文件时应返回空，得到 %v", got)
	}
}
