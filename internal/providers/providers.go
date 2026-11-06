/*
 * [INPUT]: 依赖生成预设、端点继承规则与本机凭据发现。
 * [OUTPUT]: 提供供应商类型、目录查询、端点展开与凭据发现入口。
 * [POS]: 供应商目录的公开契约；界面与模型配置通过此接口消费预设。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Package providers 是模型供应商目录。
//
// 这张表直接跟 magpie 对齐（62 个预设），不手抄 URL —— 各家的端点和
// 模型名散落在不同文档里，手抄必然漂移，而漂移的表现是「测试连接
// 通过、摘要时 404」这种最难查的问题。
//
// 三种取用方式：
//   - All() 按 UI 顺序列出全部
//   - ByID() 按 id 查具体一家
//   - Endpoints() 把一家展开成「可选的端点组合」（区域、套餐、按量付费）
//
// 为什么要有 Endpoints：像小米 MiMo 有中国/新加坡/欧洲三个区域，
// 火山方舟有 Coding/Agent/按量三个套餐，每个都有自己的 host 和 key
// 适用范围。用户要选的是「哪一档」，不是「哪一家」。
package providers

import "time"

// Kind 描述预设属于哪一类服务。
type Kind string

const (
	KindVendor Kind = "vendor" // 官方直连
	KindRelay  Kind = "relay"  // 中转/聚合
	KindLocal  Kind = "local"  // 本机推理服务
)

// Region 是一家供应商的一个可选端点组合。
//
// isTheOne 说这个 region 是预设的默认档：没有区域概念的家只有一个，
// 有区域的家里第一个就是默认。
type Region struct {
	ID        string
	Name      string
	Chat      string
	Responses string
	Anthropic string
	Website   string
	KeysURL   string
	Models    []string
}

// Preset 是供应商目录里的一条记录。
//
// Chat 是 OpenAI 兼容端点，Anthropic 是原生 Messages 端点。两者都有
// 说明这家两种协议都支持，让用户选走哪条 —— 原生协议才能用上 thinking
// 和 prompt cache。
type Preset struct {
	ID           string
	Name         string
	Short        string
	Kind         Kind
	Icon         string
	Chat         string
	Responses    string
	Anthropic    string
	Note         string
	Website      string
	KeysURL      string
	Models       []string
	NoKey        bool   // 本地服务通常不需要 key
	NoList       bool   // 这家没有 /models 可拉，Models 就是它的列表
	KeyHint      string // 输入框下的提示，如「可选：免费模型不需要 key」
	Only         string // 套餐 key 只能用的模型前缀
	Endpoint     string // 用户自己的地址，如 Azure 的 <resource>.openai.azure.com
	EndpointHint string
	RegionLabel  string
	HeaderHints  []string
	Regions      []Region
	// Plan 是订阅套餐的说明。magpie 的预设里没有结构化的价格，
	// 所以这里保持 nil，靠 Note 和 RegionLabel 表达「这档是订阅」。
	Plan *Plan
}

// Plan 描述一个订阅套餐。
type Plan struct {
	Name        string
	PriceUSD    float64
	Period      string  // "month" / "year"
	WindowHours float64 // 限额窗口小时数，0 未知
	Notes       string
}

// All 返回全部预设。
func All() []Preset { return presets }

// ByID 按 id 查预设。
func ByID(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Models 返回某个预设的候选模型。
func Models(id string) []string {
	p, ok := ByID(id)
	if !ok {
		return nil
	}
	return p.Models
}

// Endpoints 把一家展开成用户可选的具体档位。
//
// 没有 regions 的家返回它自己这一条；有的家里每个 region 一条，
// 并且把 region 的模型并到预设的模型后面（预设的模型是全集，
// region 的模型是这一档能用的）。
func Endpoints(p Preset) []Region {
	if len(p.Regions) == 0 {
		return []Region{{
			ID:        p.ID,
			Name:      displayName(p),
			Chat:      p.Chat,
			Responses: p.Responses,
			Anthropic: p.Anthropic,
			Website:   p.Website,
			KeysURL:   p.KeysURL,
			Models:    p.Models,
		}}
	}
	out := make([]Region, 0, len(p.Regions))
	for _, r := range p.Regions {
		if r.Name == "" {
			r.Name = r.ID
		}
		if r.Chat == "" {
			r.Chat = p.Chat
		}
		if r.Anthropic == "" {
			r.Anthropic = p.Anthropic
		}
		if r.Responses == "" {
			r.Responses = p.Responses
		}
		if r.Website == "" {
			r.Website = p.Website
		}
		if r.KeysURL == "" {
			r.KeysURL = p.KeysURL
		}
		if len(r.Models) == 0 {
			r.Models = p.Models
		}
		out = append(out, r)
	}
	return out
}

// DisplayName 返回给人看的名字。
func DisplayName(p Preset) string { return displayName(p) }

func displayName(p Preset) string {
	if p.Short != "" {
		return p.Short
	}
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// Credential 是从本机已登录的 CLI 里读到的凭据。
type Credential struct {
	Source       string
	Label        string
	Kind         string // "oauth" / "api_key"
	AccessToken  string
	RefreshToken string
	AccountID    string
	ExpiresAt    time.Time
}

// DetectLocal 扫描本机凭据，读不到就跳过，绝不 panic。
func DetectLocal() []Credential { return detectLocal() }
