// Package providers 是模型供应商目录：把用户能接入的每一家模型服务，
// 统一描述成 Preset（预设），供上层在「选模型 / 配 key / 生成代理 URL」
// 时直接查表，而不是到处散落 if/switch。
//
// 为什么拆成 providers.go / presets.go / local.go 三个文件：
//   - providers.go 只放对外的类型和纯查询函数（All/ByID/Models），
//     方便一眼看清 API 边界；
//   - presets.go 放那一大张预设表，数据长、且以后会频繁增删，单独放
//     避免淹没逻辑；
//   - local.go 放 DetectLocal，它依赖本机文件系统与 macOS 的 security
//     命令，和「静态目录」是两种完全不同的职责。
package providers

import "time"

// Kind 描述一个预设接入的是哪一类服务：
//   - vendor：官方直连的模型厂商（如 OpenAI、Anthropic）；
//   - relay：中转/聚合服务（如 OpenRouter、SiliconFlow），走 OpenAI 兼容协议；
//   - local：跑在本机的推理服务（如 Ollama、LM Studio），默认不需要 key；
//   - plan：订阅套餐，通常按周期付费、有独立的限额窗口和专属 base url。
type Kind string

const (
	KindVendor Kind = "vendor"
	KindRelay  Kind = "relay"
	KindLocal  Kind = "local"
	KindPlan   Kind = "plan"
)

// Plan 描述一个订阅套餐。价格查不到就填 0，并在 Notes 里写清楚。
type Plan struct {
	Name        string  // 套餐名，如 "Pro"
	PriceUSD    float64 // 价格（美元）；0 表示未知或需另行确认
	Period      string  // "month" / "year"
	WindowHours float64 // 限额窗口小时数；0 表示未知
	Notes       string  // 套餐补充说明，必须写清「价格未知」等不确定信息
}

// Preset 是模型供应商目录里的一条记录。
type Preset struct {
	ID        string // 内部唯一标识，用 kebab-case
	Name      string // 显示名
	Short     string // 短名/代码里常用的简称
	Kind      Kind
	Icon      string // 前端图标名
	Chat      string // OpenAI 兼容 base url（/v1 之前的根路径，按需拼 /v1）
	Anthropic string // Anthropic Messages base url；不支持则留空
	Note      string // 一句人话说明
	Website   string
	KeysURL   string // 去拿 API key 的页面
	Models    []string
	NoKey     bool  // 为 true 表示本地服务通常不需要 API key
	Plan      *Plan // 订阅套餐；非 nil 表示这是按套餐付费的入口
}

// All 返回全部预设。为什么直接返回切片而不是 map：
// 上层通常要按顺序渲染列表，切片保序、无额外依赖；需要按 ID 查找时用 ByID。
func All() []Preset {
	return presets()
}

// ByID 按 ID 查预设。第二个返回值表示是否命中，避免零值误判。
func ByID(id string) (Preset, bool) {
	for _, p := range presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Models 返回某个预设支持的模型列表；ID 不存在时返回空切片，
// 绝不为「查不到」而 panic——调用方可以安全地 range。
func Models(id string) []string {
	p, ok := ByID(id)
	if !ok {
		return nil
	}
	return p.Models
}

// Credential 是 DetectLocal 从本机发现的一组凭证。
// 为什么用「发现」而不是「解析所有」：这些文件/钥匙串的格式会随 CLI
// 版本变化，DetectLocal 只做最保守的字段提取，读不到就跳过。
type Credential struct {
	Source       string // 凭证来源，如 "claude" / "codex"
	Label        string // 给人看的标签
	Kind         string // 凭证类型，如 "api_key" / "oauth"
	AccessToken  string
	RefreshToken string
	AccountID    string
	ExpiresAt    time.Time
}

// DetectLocal 扫描本机已知的凭证存放位置，返回能读到的凭证。
// 设计约束：
//   - 绝不 panic：任何文件缺失、JSON 结构不符、命令失败都返回空结果；
//   - 绝不打日志打印 token 明文：这里不引入日志，调用方拿到 token 后自行负责；
//   - 不做网络请求、不引入第三方依赖。
func DetectLocal() []Credential {
	return detectLocal()
}

// MustByIDForTest 是给测试用的取预设入口。
//
// 生产代码用 ByID 拿 (Preset, bool)，测试里写 `if !ok { t.Fatal }`
// 会让每个用例多三行噪音，而测试里预设不存在就是纯粹的写错了。
// 导出它而不是在测试包里复制一份查找逻辑，是为了让两边查的
// 是同一张表。
func MustByIDForTest(id string) Preset {
	p, ok := ByID(id)
	if !ok {
		panic("unknown preset: " + id)
	}
	return p
}
