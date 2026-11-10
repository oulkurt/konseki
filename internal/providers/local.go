/*
 * [INPUT]: 依赖系统钥匙串命令与 CLI 凭据文件。
 * [OUTPUT]: 只读发现 Claude Code、Codex 与 Gemini CLI 凭据；读取 Codex 模型目录；缺失时跳过。
 * [POS]: 目录的本机凭据适配；不写凭据，不参与 query。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package providers

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 这个文件只做一件事：从本机已知位置「发现」凭证，绝不修改任何东西。
//
// 为什么 macOS 和 Linux/其他系统走两条路：
//   - macOS 上 Claude Code 把 OAuth 凭证放进钥匙串，普通文件里没有；
//   - 其他系统（以及无钥匙串的环境）才回落到 ~/.claude/.credentials.json。
// 两者 JSON 结构不同，所以解析代码分开，读不到就返回空切片。
//
// 为什么「读不到就跳过」：
//   未登录是很正常的状态，不是错误。这里没有任何日志，调用方拿到空结果
//   就当作「本机没有这个凭证」处理；绝不 panic，也绝不把 token 写进日志。

func detectLocal() []Credential {
	var out []Credential

	// Claude Code：macOS 读钥匙串，其他系统读文件。
	if runtime.GOOS == "darwin" {
		out = append(out, claudeFromKeychain()...)
	} else {
		out = append(out, claudeFromFile()...)
	}

	// Codex：所有平台都读 ~/.codex/auth.json。
	out = append(out, codexFromFile()...)

	// Gemini CLI：所有平台都读 ~/.gemini/oauth_creds.json。
	out = append(out, geminiFromFile()...)

	return out
}

// readClaudeKeychain 用 security(1) 读 Claude Code 的钥匙串项。
// -s 是服务名，"Claude Code-credentials" 是 Claude Code 官方写死的服务名；
// -w 只输出密码（这里的密码就是一份 JSON），不打印元数据。
// 做成变量是为了让测试换掉它：测试绝不能把开发者本机的真实令牌读进进程。
var readClaudeKeychain = func() ([]byte, error) {
	return exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
}

func claudeFromKeychain() []Credential {
	b, err := readClaudeKeychain()
	if err != nil {
		return nil
	}
	return claudeFromJSON(b)
}

// claudeFromFile 读 ~/.claude/.credentials.json（非 macOS 的落盘位置）。
func claudeFromFile() []Credential {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		return nil
	}
	return claudeFromJSON(b)
}

// claudeFromJSON 解析 Claude 的凭证 JSON。两种结构都要兼容：
//   - 官方钥匙串/新文件：{"claudeAiOauth": {accessToken, refreshToken, expiresAt}}
//   - 旧文件可能直接平铺：{accessToken, refreshToken, expiresAt}
func claudeFromJSON(b []byte) []Credential {
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil
	}

	oauth, ok := root["claudeAiOauth"].(map[string]any)
	if !ok {
		// 平铺结构：直接把 root 当凭证对象。
		oauth = root
	}

	cred := Credential{Source: "claude", Label: "Claude Code", Kind: "oauth"}
	cred.AccessToken = stringField(oauth, "accessToken")
	cred.RefreshToken = stringField(oauth, "refreshToken")
	cred.ExpiresAt = parseTime(stringField(oauth, "expiresAt"))

	if cred.AccessToken == "" && cred.RefreshToken == "" {
		return nil
	}
	return []Credential{cred}
}

// codexFromFile 读 ~/.codex/auth.json。Codex 可能同时有 API key 和 OAuth token，
// 所以各返回一条 Credential，调用方按 Kind 选用。
func codexFromFile() []Credential {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil
	}

	var out []Credential

	if key := stringField(root, "OPENAI_API_KEY"); key != "" {
		out = append(out, Credential{Source: "codex", Label: "Codex API key", Kind: "api_key", AccessToken: key})
	}

	if tokens, ok := root["tokens"].(map[string]any); ok {
		cred := Credential{Source: "codex", Label: "Codex", Kind: "oauth"}
		cred.AccessToken = stringField(tokens, "access_token")
		cred.RefreshToken = stringField(tokens, "refresh_token")
		cred.AccountID = stringField(tokens, "account_id")
		if cred.AccessToken != "" || cred.RefreshToken != "" {
			out = append(out, cred)
		}
	}

	return out
}

// stringField 从 JSON 对象里取字符串字段；类型不对或缺失都返回空串，
// 因为上游文件格式会变，这里只做「能读就读」。
func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

// geminiFromFile 读 ~/.gemini/oauth_creds.json。
// 只收带 cloud-platform 权限的凭据：缺这个权限，Code Assist 端点会拒绝。
func geminiFromFile() []Credential {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".gemini", "oauth_creds.json"))
	if err != nil {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil
	}
	if !hasCloudPlatformScope(root["scope"]) {
		return nil
	}
	cred := Credential{Source: "gemini", Label: "Gemini CLI", Kind: "oauth"}
	cred.AccessToken = stringField(root, "access_token")
	cred.RefreshToken = stringField(root, "refresh_token")
	cred.ExpiresAt = parseMillisTime(root["expiry_date"])
	if cred.AccessToken == "" && cred.RefreshToken == "" {
		return nil
	}
	return []Credential{cred}
}

// hasCloudPlatformScope 兼容字符串和数组两种 scope 写法。
func hasCloudPlatformScope(v any) bool {
	switch scope := v.(type) {
	case string:
		return strings.Contains(scope, "cloud-platform")
	case []any:
		for _, item := range scope {
			if s, _ := item.(string); strings.Contains(s, "cloud-platform") {
				return true
			}
		}
	}
	return false
}

// parseMillisTime 解析毫秒级 Unix 时间戳。
// Gemini CLI 存的是数字，也见过存成字符串的版本，两种都收。
func parseMillisTime(v any) time.Time {
	var ms int64
	switch x := v.(type) {
	case float64:
		ms = int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return time.Time{}
		}
		ms = n
	default:
		return time.Time{}
	}
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// codexModelEntry 兼容 Codex 模型目录的两种字段名：
// 官方导出用 id，本机 magpie 配置用 slug。
type codexModelEntry struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// CodexModels 读 ~/.codex/magpie-models.json，给导入时填真实模型列表。
// 顶层可能是 {"models":[...]}，也可能直接是数组；读不到返回空。
// 这里的模型名只用于选择，不发请求，所以宁可宽容也不要报错。
func CodexModels() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "magpie-models.json"))
	if err != nil {
		return nil
	}
	var entries []codexModelEntry
	var wrap struct {
		Models []codexModelEntry `json:"models"`
	}
	if json.Unmarshal(b, &wrap) == nil && len(wrap.Models) > 0 {
		entries = wrap.Models
	} else if json.Unmarshal(b, &entries) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			id = strings.TrimSpace(e.Slug)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// parseTime 尽力解析时间字符串：支持 RFC3339 和纯 Unix 秒。
// 解析不了就返回零值，绝不为坏数据 panic。
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	// 有些客户端存的是秒级 Unix 时间戳。
	if sec, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(sec, 0)
	}
	return time.Time{}
}
