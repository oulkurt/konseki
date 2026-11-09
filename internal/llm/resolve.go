/*
 * [INPUT]: 依赖供应商预设和用户配置。
 * [OUTPUT]: 提供模型协议、端点和候选模型的解析结果。
 * [POS]: 模型设置与网络请求共用的解析边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package llm

// 把「用户在设置里选的那个供应商 + 那个模型」解成一次请求需要的全部参数。
//
// 这一层存在的意义：设置里存的是「预设 id + 覆盖项」，而请求要的是
// 完整的 URL、协议、header。中间这层转换如果散在 UI 和各调用点，
// 很快就会出现「UI 里测试通过、摘要时 404」这种不一致。
//
// 规则尽量简单可预测：
//   - 用户填了 base，就用用户的；
//   - 没填就用预设的；
//   - Anthropic 官方和少数厂商支持原生 Messages 协议，能选就优先用它
//     （thinking、cache 这些都只有原生协议才有）；
//   - 其余一律走 OpenAI 兼容，因为它的覆盖面最广。

import (
	"fmt"
	"strings"

	"konseki/internal/config"
	"konseki/internal/providers"
)

// Resolved 是一次调用所需的全部参数。不含密钥，调用方自己填。
type Resolved struct {
	Kind    Kind
	BaseURL string
	Headers map[string]string
	// Model 是最终使用的模型 id。
	Model string
	// ProviderName 只用于 UI 展示。
	ProviderName string
	// SupportsAnthropic 说这个供应商有没有原生 Messages 端点。
	// UI 用它决定要不要显示「用原生协议」的开关。
	SupportsAnthropic bool
	// Models 是候选模型列表（预设 + 用户启用 + 已拉取的）。
	Models []string
}

// Resolve 把一个 ProviderConfig 解析成可调用的参数。
//
// preferAnthropic 只在供应商两种协议都支持时才有意义：
// 对有原生协议的厂商（Anthropic、DeepSeek、Kimi …）优先走原生，
// 因为 thinking 和 prompt cache 在兼容层里常常被丢掉。
func Resolve(pc config.ProviderConfig, preferAnthropic bool) (Resolved, error) {
	preset, _ := providers.ByID(pc.Preset)

	name := pc.Label
	if name == "" {
		name = preset.Name
	}
	if name == "" {
		name = pc.Preset
	}

	// 先找用户选的档位。档位里的端点可能只覆盖了一部分
	// （比如某区域只给了 Chat 没给 Anthropic），缺的字段落回预设。
	var region providers.Region
	for _, r := range providers.Endpoints(preset) {
		if r.ID == pc.Region {
			region = r
			break
		}
	}
	if region.ID == "" && len(preset.Regions) > 0 {
		// 没选或选了不存在的档位，用第一档。magpie 的约定是
		// 第一个 region 就是预设的默认端点。
		region = providers.Endpoints(preset)[0]
	}

	chatBase := firstNonEmpty(pc.ChatBase, region.Chat, preset.Chat)
	anthropicBase := firstNonEmpty(pc.AnthropicBase, region.Anthropic, preset.Anthropic)

	r := Resolved{
		ProviderName:      name,
		SupportsAnthropic: anthropicBase != "",
	}

	if pc.DefaultModel != "" {
		r.Model = pc.DefaultModel
	} else {
		r.Model = firstOf(preset.Models)
	}

	// 模型列表：档位的模型 + 用户启用的，都没有才退到预设全集。
	// 档位优先是因为某区域的模型列表才是那把 key 真能调的。
	base := region.Models
	if len(base) == 0 {
		base = preset.Models
	}
	seen := map[string]bool{}
	for _, m := range append(append([]string{}, base...), pc.EnabledModels...) {
		if m != "" && !seen[m] {
			seen[m] = true
			r.Models = append(r.Models, m)
		}
	}

	switch {
	case preferAnthropic && anthropicBase != "":
		r.Kind = AnthropicMessages
		r.BaseURL = anthropicBase
	case chatBase != "":
		r.Kind = OpenAICompatible
		r.BaseURL = chatBase
	case anthropicBase != "":
		// 只支持原生协议的供应商（Claude 官方）
		r.Kind = AnthropicMessages
		r.BaseURL = anthropicBase
	default:
		return r, fmt.Errorf("%s 没有配置接口地址", name)
	}

	if r.Model == "" {
		return r, fmt.Errorf("%s 还没有选模型", name)
	}

	// Azure 的部署名是模型 id，但请求要用 api-key 头而不是 Bearer。
	if strings.Contains(r.BaseURL, ".openai.azure.com") {
		r.Headers = map[string]string{"api-key": ""}
	}
	return r, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstOf(vals []string) string {
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}
