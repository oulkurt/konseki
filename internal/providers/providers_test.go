package providers

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 预设表必须能支撑 UI 的三种画法：一个网格、一个详情面板、一个档位选择。
// 这个测试检查的是「表本身有没有硬伤」，不是某家供应商的 URL 对不对 ——
// URL 的正确性靠跟 magpie 对齐，改那边时重新导出即可。

func TestPresetsNonEmpty(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("预设表是空的")
	}
	// magpie 有 62 个预设，其中 7 个是纯路由决策（decide），不提供对话，
	// Konseki 用不上，导出时剔除。剩下的 55 个才是这里该有的量。
	if len(all) < 55 {
		t.Errorf("预设数 = %d，期望至少 55（magpie 62 个减去 7 个纯决策）", len(all))
	}
}

func TestPresetIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range All() {
		if p.ID == "" {
			t.Error("有预设没有 id")
			continue
		}
		if seen[p.ID] {
			t.Errorf("id 重复：%s", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestByID(t *testing.T) {
	for _, id := range []string{"anthropic", "openai", "deepseek", "openrouter", "ollama"} {
		if _, ok := ByID(id); !ok {
			t.Errorf("找不到预设 %q", id)
		}
	}
	if _, ok := ByID("definitely-not-a-provider"); ok {
		t.Error("不存在的 id 不该返回命中")
	}
}

// 每一家至少要有一个能用的端点，否则进了 UI 也配不起来。
func TestEveryPresetHasEndpoint(t *testing.T) {
	for _, p := range All() {
		eps := Endpoints(p)
		if len(eps) == 0 {
			t.Errorf("%s 没有可用端点", p.ID)
			continue
		}
		hasURL := false
		for _, e := range eps {
			if e.Chat != "" || e.Anthropic != "" || p.Endpoint != "" {
				hasURL = true
				break
			}
		}
		if !hasURL {
			t.Errorf("%s 的所有档位都没有地址（也没有要用户填地址的 Endpoint）", p.ID)
		}
	}
}

// 有 regions 的家，每个 region 都要有名字，并且端点是完整的 ——
// 缺的字段已经在 Endpoints() 里从预设补上了，这里验证补得对。
func TestRegionsInheritPreset(t *testing.T) {
	for _, p := range All() {
		if len(p.Regions) == 0 {
			continue
		}
		eps := Endpoints(p)
		if len(eps) != len(p.Regions) {
			t.Errorf("%s 的档位数不对：%d vs %d", p.ID, len(eps), len(p.Regions))
		}
		for _, e := range eps {
			if e.Name == "" {
				t.Errorf("%s/%s 没有名字", p.ID, e.ID)
			}
			if e.Chat == "" && e.Anthropic == "" {
				t.Errorf("%s/%s 补完继承后仍然没有地址", p.ID, e.ID)
			}
		}
	}
}

// 没有 regions 的家，Endpoints 应该返回它自己这一条。
func TestSingleEndpointForSimplePreset(t *testing.T) {
	p, ok := ByID("deepseek")
	if !ok {
		t.Skip("没有 deepseek")
	}
	eps := Endpoints(p)
	if len(eps) != 1 {
		t.Fatalf("期望 1 个端点，得到 %d", len(eps))
	}
	if eps[0].Chat != p.Chat {
		t.Errorf("端点地址被改写了：%q vs %q", eps[0].Chat, p.Chat)
	}
}

func TestDisplayNameFallsBack(t *testing.T) {
	if got := DisplayName(Preset{ID: "x", Short: "S", Name: "Long"}); got != "S" {
		t.Errorf("DisplayName 应优先 Short，得到 %q", got)
	}
	if got := DisplayName(Preset{ID: "x", Name: "Long"}); got != "Long" {
		t.Errorf("没有 Short 时用 Name，得到 %q", got)
	}
	if got := DisplayName(Preset{ID: "x"}); got != "x" {
		t.Errorf("都没有时用 ID，得到 %q", got)
	}
}

// 扫描只读临时 HOME 和假的钥匙串。测试绝不碰开发者本机的真实凭据。
func TestDetectLocalParsesInjectedSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	orig := readClaudeKeychain
	t.Cleanup(func() { readClaudeKeychain = orig })
	claude := `{"claudeAiOauth":{"accessToken":"fake-claude-token","refreshToken":"r","expiresAt":"2026-11-06T03:00:00Z"}}`
	readClaudeKeychain = func() ([]byte, error) { return []byte(claude), nil }
	files := map[string]string{
		".claude/.credentials.json": claude,
		".codex/auth.json":          `{"OPENAI_API_KEY":"fake-key","tokens":{"access_token":"fake-codex-token","account_id":"acc"}}`,
	}
	for name, body := range files {
		path := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := map[string]string{}
	for _, c := range DetectLocal() {
		got[c.Source+"|"+c.Kind] = c.AccessToken
		if strings.Contains(c.Label, c.AccessToken) {
			t.Errorf("Label %q 里出现了令牌", c.Label)
		}
	}
	want := map[string]string{
		"claude|oauth":  "fake-claude-token",
		"codex|api_key": "fake-key",
		"codex|oauth":   "fake-codex-token",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("扫描结果 %v，期望 %v", got, want)
	}
}

func TestDetectLocalEmptyHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := readClaudeKeychain
	t.Cleanup(func() { readClaudeKeychain = orig })
	readClaudeKeychain = func() ([]byte, error) { return nil, os.ErrNotExist }
	if creds := DetectLocal(); len(creds) != 0 {
		t.Fatalf("没有任何 CLI 时应返回空，得到 %d 条", len(creds))
	}
}
