/*
 * [INPUT]: 依赖 JSON 设置、本机目录环境变量和脱敏规则。
 * [OUTPUT]: 提供设置类型、默认值、版本迁移和脱敏规则翻译；旧键鼠配置键不迁移。
 * [POS]: 配置模块的语义边界，供 store 持久化。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"konseki/internal/redact"
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
	// ResponsesBase 覆盖 OpenAI Responses 端点，和 ChatBase 并列。
	ResponsesBase string `json:"responsesBase,omitempty"`

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

	// Region 是用户在这一家里选的档位 id。
	//
	// 像小米 MiMo 的中国/新加坡/欧洲、火山方舟的 Coding/Agent/按量，
	// 每档有自己的 host 和 key 适用范围。空表示用预设的默认档。
	Region string `json:"region,omitempty"`

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

// Options 把界面上的脱敏开关翻译成脱敏规则。
// 总开关关闭时返回零值，什么都不遮；以前这里仍会遮 Words 和显式打开的 Kinds，
// 和界面语义不符。
func (r RedactConfig) Options() redact.Options {
	if !r.Enabled {
		return redact.Options{}
	}
	return redact.Options{
		Secrets:  r.Secrets,
		Personal: r.Personal,
		Kinds:    r.Kinds,
		Allow:    r.Allow,
		Words:    r.Words,
	}
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
}

// SkillConfig 保存旧版技能导出的目录。
type SkillConfig struct {
	// ExportDir 是旧版技能导出写到的目录。新版不再导出，只用它找旧文件。
	ExportDir string `json:"exportDir,omitempty"`
}

type Config struct {
	Version   int              `json:"version"`
	Language  string           `json:"language"`
	Recorder  Recorder         `json:"recorder"`
	Providers []ProviderConfig `json:"providers"`
	Redact    RedactConfig     `json:"redact"`
	Summary   SummaryConfig    `json:"summary"`
	Skill     SkillConfig      `json:"skill"`
}

// Recorder 是采集设置。时间字段一律是毫秒整数，和 Alma 一致，方便手改文件。
// 运行时由 package main 换成 capture.Settings，capture 不依赖本包。
type Recorder struct {
	Enabled       bool      `json:"enabled"`
	PausedUntil   time.Time `json:"pausedUntil,omitzero"`
	DebounceMs    int       `json:"debounceMs"`
	HeartbeatMs   int       `json:"heartbeatMs"`
	IdleMs        int       `json:"idleMs"`
	TypingPauseMs int       `json:"typingPauseMs"`
	ScreenPollMs  int       `json:"screenPollMs"`
	JPEGQuality   int       `json:"jpegQuality"`
	OCR           OCRConfig `json:"ocr"`
	InputCounts   bool      `json:"inputCounts"`
	SensitiveApps []string  `json:"sensitiveApps"`
	MaxStorageMB  int       `json:"maxStorageMB"`
	RetentionDays int       `json:"retentionDays"`
	OutputDir     string    `json:"outputDir,omitempty"`
}

type OCRConfig struct {
	Enabled   bool     `json:"enabled"`
	Languages []string `json:"languages"`
	EveryN    int      `json:"everyN"`
}

func Default() Config {
	return Config{
		Version:  2,
		Language: "zh-Hans",
		Recorder: Recorder{
			Enabled:       true,
			DebounceMs:    4000,
			HeartbeatMs:   120000,
			IdleMs:        30000,
			TypingPauseMs: 1200,
			ScreenPollMs:  12000,
			JPEGQuality:   55,
			OCR: OCRConfig{
				Enabled:   true,
				Languages: []string{"en-US", "zh-Hans", "zh-Hant", "ja"},
				EveryN:    3,
			},
			SensitiveApps: []string{
				"com.apple.keychainaccess",
				"com.1password.1password",
				"com.agilebits.onepassword7",
				"org.bitwarden.desktop",
				"com.lastpass.LastPass",
				"com.dashlane.dashlanephonefinal",
				"com.apple.Passwords",
			},
			MaxStorageMB:  10240,
			RetentionDays: 30,
		},
		Redact:  RedactConfig{Enabled: true, Secrets: true, Personal: true, ShowHits: true},
		Summary: SummaryConfig{IntervalMinutes: 30, ThinkingEffort: "medium", WindowMinutes: 60},
	}
}

func AppDir() string {
	if d := os.Getenv("KONSEKI_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Konseki")
}

func (r Recorder) Dir(appDir string) string {
	if r.OutputDir != "" {
		return r.OutputDir
	}
	return appDir
}

type wireConfig struct {
	Version         *int            `json:"version,omitempty"`
	Language        *string         `json:"language,omitempty"`
	Recorder        *wireRecorder   `json:"recorder,omitempty"`
	Providers       *[]wireProvider `json:"providers,omitempty"`
	Redact          *wireRedact     `json:"redact,omitempty"`
	Summary         *wireSummary    `json:"summary,omitempty"`
	Skill           *wireSkill      `json:"skill,omitempty"`
	LegacyPaused    *bool           `json:"capturePaused,omitempty"`
	LegacyRetention *int            `json:"retentionDays,omitempty"`
}

type wireRecorder struct {
	Enabled       *bool      `json:"enabled,omitempty"`
	PausedUntil   *time.Time `json:"pausedUntil,omitempty"`
	DebounceMs    *int       `json:"debounceMs,omitempty"`
	HeartbeatMs   *int       `json:"heartbeatMs,omitempty"`
	IdleMs        *int       `json:"idleMs,omitempty"`
	TypingPauseMs *int       `json:"typingPauseMs,omitempty"`
	ScreenPollMs  *int       `json:"screenPollMs,omitempty"`
	JPEGQuality   *int       `json:"jpegQuality,omitempty"`
	OCR           *wireOCR   `json:"ocr,omitempty"`
	InputCounts   *bool      `json:"inputCounts,omitempty"`
	SensitiveApps *[]string  `json:"sensitiveApps,omitempty"`
	MaxStorageMB  *int       `json:"maxStorageMB,omitempty"`
	RetentionDays *int       `json:"retentionDays,omitempty"`
	OutputDir     *string    `json:"outputDir,omitempty"`
}

type wireOCR struct {
	Enabled   *bool     `json:"enabled,omitempty"`
	Languages *[]string `json:"languages,omitempty"`
	EveryN    *int      `json:"everyN,omitempty"`
}

type wireProvider struct {
	Preset        *string    `json:"preset,omitempty"`
	Label         *string    `json:"label,omitempty"`
	ChatBase      *string    `json:"chatBase,omitempty"`
	AnthropicBase *string    `json:"anthropicBase,omitempty"`
	ResponsesBase *string    `json:"responsesBase,omitempty"`
	Auth          *string    `json:"auth,omitempty"`
	AccountID     *string    `json:"accountId,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	Region        *string    `json:"region,omitempty"`
	EnabledModels *[]string  `json:"enabledModels,omitempty"`
	DefaultModel  *string    `json:"defaultModel,omitempty"`
}

type wireRedact struct {
	Enabled  *bool            `json:"enabled,omitempty"`
	Secrets  *bool            `json:"secrets,omitempty"`
	Personal *bool            `json:"personal,omitempty"`
	Kinds    *map[string]bool `json:"kinds,omitempty"`
	Allow    *[]string        `json:"allow,omitempty"`
	Words    *[]string        `json:"words,omitempty"`
	ShowHits *bool            `json:"showHits,omitempty"`
}

type wireSummary struct {
	Auto            *bool   `json:"auto,omitempty"`
	IntervalMinutes *int    `json:"intervalMinutes,omitempty"`
	ProviderID      *string `json:"providerId,omitempty"`
	Model           *string `json:"model,omitempty"`
	ThinkingEffort  *string `json:"thinkingEffort,omitempty"`
	WindowMinutes   *int    `json:"windowMinutes,omitempty"`
	LegacyLanguage  *string `json:"language,omitempty"`
}

type wireSkill struct {
	ExportDir *string `json:"exportDir,omitempty"`
}

func parse(data []byte) (Config, bool, error) {
	var w wireConfig
	if err := json.Unmarshal(data, &w); err != nil {
		return Config{}, false, err
	}
	// 线上结构的字段全是指针，文件里没写的字段是 nil，编码时被 omitempty 略去。
	// 再把它解到默认值上，没写的字段保留默认值，写了的 false 和 0 照常覆盖。
	c := Default()
	b, err := json.Marshal(w)
	if err != nil {
		return Config{}, false, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return Config{}, false, err
	}
	legacy := w.Version == nil
	if legacy {
		c.Language = "zh-Hans"
		if w.Summary != nil && w.Summary.LegacyLanguage != nil && *w.Summary.LegacyLanguage == "en" {
			c.Language = "en"
		}
		c.Recorder = Default().Recorder
		if w.LegacyPaused != nil {
			c.Recorder.Enabled = !*w.LegacyPaused
		}
		c.Recorder.RetentionDays = 14
		if w.LegacyRetention != nil && *w.LegacyRetention > 0 {
			c.Recorder.RetentionDays = *w.LegacyRetention
		}
	}
	normalize(&c)
	return c, legacy, nil
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// clean 去空白、去重并保序。结果总是非 nil：nil 落盘是 null，重读时等同字段缺失，
// 用户清空的列表会被默认值顶回来。
func clean(in []string, fold bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		key := v
		if fold {
			key = strings.ToLower(key)
		}
		if v != "" && !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	return out
}

func normalize(c *Config) {
	c.Version = 2
	if strings.HasPrefix(strings.ToLower(c.Language), "en") {
		c.Language = "en"
	} else {
		c.Language = "zh-Hans"
	}
	r := &c.Recorder
	r.DebounceMs = clamp(r.DebounceMs, 3000, 30000)
	r.HeartbeatMs = clamp(r.HeartbeatMs, 60000, 300000)
	r.IdleMs = clamp(r.IdleMs, 10000, 600000)
	r.TypingPauseMs = clamp(r.TypingPauseMs, 800, 5000)
	if r.ScreenPollMs != 0 {
		r.ScreenPollMs = clamp(r.ScreenPollMs, 10000, 30000)
	}
	r.JPEGQuality = clamp(r.JPEGQuality, 30, 95)
	r.OCR.EveryN = clamp(r.OCR.EveryN, 1, 20)
	r.MaxStorageMB = clamp(r.MaxStorageMB, 100, 200000)
	if r.RetentionDays != 0 {
		r.RetentionDays = clamp(r.RetentionDays, 1, 3650)
	}
	r.OCR.Languages = clean(r.OCR.Languages, false)
	if len(r.OCR.Languages) == 0 {
		r.OCR.Languages = Default().Recorder.OCR.Languages
	}
	r.SensitiveApps = clean(r.SensitiveApps, true)
}
