// Package config 保存用户在 Konseki 里的全部设置。
//
// 一个 JSON 文件放在数据目录里，人可读、可手改、可以进 git。
// 密钥单独存，并且设 0600 —— 配置会被人复制粘贴到 issue 里，
// key 不能跟着一起走。
//
// 这里刻意不做热加载：设置页改一次就 Save 一次，进程内只有一份
// 内存副本。省掉一轮「文件变了要不要重载、重载会不会打断正在跑的
// 摘要任务」的复杂度。
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProviderConfig 是一个用户配好的模型供应商。
//
// 之所以把凭据和预设分开存：预设（internal/providers）是随版本走的常量，
// 会变；用户选了什么、填了什么，必须能跨版本存活。所以这里只存
// 「预设 id + 覆盖项 + 凭据」，读的时候再和预设合并。
type ProviderConfig struct {
	// Preset 是 internal/providers 里的预设 id，空表示用户自建的 custom。
	Preset string `json:"preset"`
	// Label 是用户给它起的名字，空则用预设的 Name。
	Label string `json:"label,omitempty"`

	// 覆盖预设里的端点。用户接中转站或者自建网关时要填。
	ChatBase      string `json:"chatBase,omitempty"`
	AnthropicBase string `json:"anthropicBase,omitempty"`

	// APIKey 只在内存里以明文出现；落盘时由 Store 拆出去单独存。
	APIKey string `json:"-"`

	// Auth 说明用哪种方式鉴权。
	//   "apikey" —— 走 Authorization: Bearer（默认）
	//   "oauth"  —— 从本机 CLI 导入的订阅凭据，会过期，需要刷新
	Auth string `json:"auth,omitempty"`

	// 导入的订阅凭据。刷新逻辑在 internal/llm。
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"-"`
	AccountID    string    `json:"accountId,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`

	// 订阅套餐：用户选的是哪个 plan（只影响展示，不影响请求）。
	Plan string `json:"plan,omitempty"`

	// 用户启用/禁用了哪些模型。空表示全用预设给的。
	EnabledModels []string `json:"enabledModels,omitempty"`
	// DefaultModel 是这个供应商下的默认模型 id。
	DefaultModel string `json:"defaultModel,omitempty"`
}

// RedactConfig 是脱敏开关。
//
// 默认值按「宁可多遮」给：一条时间轴里混着代码、聊天记录和邮件，
// 只有遮住了才敢发给第三方模型。
type RedactConfig struct {
	Enabled  bool `json:"enabled"`
	Secrets  bool `json:"secrets"`
	Personal bool `json:"personal"`
	// Kinds 是个人数据细分开关，未出现的分类用 providers 的默认值。
	Kinds map[string]bool `json:"kinds,omitempty"`
	// Allow 是用户明确放行的值（自家域名、常用账号）。
	Allow []string `json:"allow,omitempty"`
	// Words 是用户自己的词：项目代号、同事名字。这些不是「敏感数据」，
	// 但用户不希望出现在别人的服务器上。
	Words []string `json:"words,omitempty"`
	// ShowHits 让 UI 显示「这条摘要遮掉了什么」，默认开。
	// 关掉是为了截图分享时不泄露原文。
	ShowHits bool `json:"showHits"`
}

// SummaryConfig 控制自动摘要。
type SummaryConfig struct {
	// Auto 让 Konseki 在空闲时自己跑当天的摘要。
	Auto bool `json:"auto"`
	// IntervalMinutes 是自动摘要的节流间隔。
	IntervalMinutes int `json:"intervalMinutes,omitempty"`
	// ProviderID / Model 指定用哪个模型。空则用第一个可用供应商的默认模型。
	ProviderID string `json:"providerId,omitempty"`
	Model      string `json:"model,omitempty"`
	// ThinkingEffort 是推理档位：off / low / medium / high / max。
	ThinkingEffort string `json:"thinkingEffort,omitempty"`
	// WindowMinutes 是一个摘要块覆盖的时间跨度。
	WindowMinutes int `json:"windowMinutes,omitempty"`
	// Language 是摘要输出的语言，空表示跟随系统。
	Language string `json:"language,omitempty"`
}

// SkillConfig 控制给外部 Agent 用的导出。
type SkillConfig struct {
	// ExportDir 是 skill 的输出位置，空表示用默认目录。
	ExportDir string `json:"exportDir,omitempty"`
	// IncludeOCR 决定导出时带不带原始 OCR。默认关 —— 那是最脏的数据。
	IncludeOCR bool `json:"includeOCR"`
	// RedactOnExport 在导出前再遮一次。即使入库时已经遮过，导出
	// 是第二道外发口，值得再走一遍。
	RedactOnExport bool `json:"redactOnExport"`
	// Days 是导出最近多少天。
	Days int `json:"days,omitempty"`
}

// Config 是整个应用的设置。
type Config struct {
	Providers []ProviderConfig `json:"providers"`
	Redact    RedactConfig     `json:"redact"`
	Summary   SummaryConfig    `json:"summary"`
	Skill     SkillConfig      `json:"skill"`

	// Capture 沿用采集层的运行参数，放这里是让用户能调。
	CapturePaused bool `json:"capturePaused"`
	RetentionDays int  `json:"retentionDays,omitempty"`
}

// Default 返回一份可以直接用的设置。
func Default() Config {
	return Config{
		Redact: RedactConfig{
			Enabled:  true,
			Secrets:  true,
			Personal: true,
			ShowHits: true,
		},
		Summary: SummaryConfig{
			Auto:            false,
			IntervalMinutes: 30,
			ThinkingEffort:  "medium",
			WindowMinutes:   60,
		},
		Skill: SkillConfig{
			Days:           7,
			RedactOnExport: true,
		},
		RetentionDays: 14,
	}
}

// Store 是设置文件的读写句柄。
type Store struct {
	dir string
	mu  sync.Mutex
	cfg Config
}

// secretsFile 是密钥的落盘位置，和主配置分开。
const secretsFile = "secrets.json"

// Open 打开（或创建）数据目录里的设置。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, cfg: Default()}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) path() string       { return filepath.Join(s.dir, "config.json") }
func (s *Store) secretPath() string { return filepath.Join(s.dir, secretsFile) }

// load 读主配置和密钥。文件不存在不算错 —— 第一次启动本来就没有。
func (s *Store) load() error {
	b, err := os.ReadFile(s.path())
	switch {
	case err == nil:
		// 从一个「零值」开始反序列化，再补齐缺省值。
		// 这样旧版本的配置文件升级后，新加字段能拿到合理默认，
		// 而不是全部变成零值（ShowHits=false 会静默关掉一个功能）。
		var raw Config
		if err := json.Unmarshal(b, &raw); err != nil {
			return err
		}
		s.cfg = mergeDefaults(raw)
	case errors.Is(err, os.ErrNotExist):
		s.cfg = Default()
	default:
		return err
	}
	return s.loadSecrets()
}

// mergeDefaults 用零值判断哪些字段是「旧文件里没有的」。
//
// 不能整体 merge，因为 bool 的零值 false 是合法配置（用户就是想关掉）。
// 折中：只对「默认开」的开关做补齐，其余尊重文件。
func mergeDefaults(raw Config) Config {
	d := Default()
	// 脱敏默认是开的。旧文件如果整个 redact 段缺失（Git 检测不到），
	// 反序列化后 Enabled 会是 false，从而静默把隐私保护关掉。
	// 用一个哨兵字段区分「缺失」和「显式写 false」不值得，所以
	// 这里直接按「文件里没有 redact 段」判断。
	if !raw.Redact.Enabled && !raw.Redact.Secrets && !raw.Redact.Personal && raw.Redact.Kinds == nil {
		raw.Redact = d.Redact
	}
	if raw.Summary.ThinkingEffort == "" {
		raw.Summary.ThinkingEffort = d.Summary.ThinkingEffort
	}
	if raw.Summary.IntervalMinutes == 0 {
		raw.Summary.IntervalMinutes = d.Summary.IntervalMinutes
	}
	if raw.Summary.WindowMinutes == 0 {
		raw.Summary.WindowMinutes = d.Summary.WindowMinutes
	}
	if raw.Skill.Days == 0 {
		raw.Skill.Days = d.Skill.Days
	}
	if raw.RetentionDays == 0 {
		raw.RetentionDays = d.RetentionDays
	}
	return raw
}

type secretEntry struct {
	APIKey       string `json:"apiKey,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

func (s *Store) loadSecrets() error {
	b, err := os.ReadFile(s.secretPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var m map[string]secretEntry
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for i := range s.cfg.Providers {
		p := &s.cfg.Providers[i]
		if e, ok := m[p.Preset+"|"+p.Label]; ok {
			p.APIKey = e.APIKey
			p.AccessToken = e.AccessToken
			p.RefreshToken = e.RefreshToken
		}
	}
	return nil
}

// writeSecrets 把密钥单独写一份 0600 的文件。
func (s *Store) writeSecrets() error {
	m := map[string]secretEntry{}
	for _, p := range s.cfg.Providers {
		if p.APIKey == "" && p.AccessToken == "" && p.RefreshToken == "" {
			continue
		}
		m[p.Preset+"|"+p.Label] = secretEntry{
			APIKey: p.APIKey, AccessToken: p.AccessToken, RefreshToken: p.RefreshToken,
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// 先写临时文件再 rename：中途崩溃不会留下半截的密钥文件。
	tmp := s.secretPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.secretPath())
}

// RetentionDays 改保留天数的便捷入口。
//
// 单独一个方法而不是让上层拼 Update(func(*Config){...})：
// 设置页里这类「改一个标量」的操作太多，每次写闭包会让 UI
// 代码被样板淹掉。
func (s *Store) SetRetentionDays(days int) error {
	return s.Update(func(c *Config) { c.RetentionDays = days })
}

// Get 返回当前设置的副本。
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.clone()
}

// Update 用 fn 改设置并落盘。fn 拿到的是副本，改动成功后才会生效 ——
// 这样 fn 里 panic 或返回逻辑错误时，内存和磁盘不会不一致。
func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.clone()
	fn(&next)
	s.cfg = next
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		return err
	}
	return s.writeSecrets()
}

func (c Config) clone() Config {
	out := c
	if c.Providers != nil {
		out.Providers = make([]ProviderConfig, len(c.Providers))
		for i, p := range c.Providers {
			out.Providers[i] = p
			if p.EnabledModels != nil {
				out.Providers[i].EnabledModels = append([]string(nil), p.EnabledModels...)
			}
		}
	}
	if c.Redact.Kinds != nil {
		out.Redact.Kinds = make(map[string]bool, len(c.Redact.Kinds))
		for k, v := range c.Redact.Kinds {
			out.Redact.Kinds[k] = v
		}
	}
	if c.Redact.Allow != nil {
		out.Redact.Allow = append([]string(nil), c.Redact.Allow...)
	}
	if c.Redact.Words != nil {
		out.Redact.Words = append([]string(nil), c.Redact.Words...)
	}
	return out
}
