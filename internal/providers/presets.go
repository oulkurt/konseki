package providers

// 这个文件是供应商目录的数据本体。
//
// 为什么数据要集中在这里，而不是散在各调用方：
//   - URL、官网、key 页面、套餐说明都属于「事实数据」，会随厂商公告变化；
//   - 集中成一张表后，上层只需要查 ID，新增厂商不用改任何逻辑；
//   - 测试可以直接遍历 All() 检查整张表的约束（id 唯一、Models 非空等）。
//
// Chat / Anthropic 字段的含义：
//   - Chat 是 OpenAI 兼容协议的 base url（调用方按需拼 /v1、/chat/completions）；
//   - Anthropic 是 Anthropic Messages 协议的 base url，不支持该协议的留空。
// 两者都直接照抄 magpie 的 presets.go，保证和真实服务一致。
//
// Models 是「静态提示」：某些厂商（Bedrock、Azure、各 Token Plan）不提供
// 可拉取的 /models 列表，这里给出文档里写明的代表模型；能动态拉列表的
// 厂商（OpenAI、DeepSeek 等）上层可以再覆盖，这里仍然给几个常见模型，
// 避免 Models(id) 为空。

var presetList = []Preset{
	{
		ID: "anthropic", Name: "Anthropic", Icon: "claude-color", Kind: KindVendor,
		Anthropic: "https://api.anthropic.com",
		Website:   "https://console.anthropic.com", KeysURL: "https://console.anthropic.com/settings/keys",
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"},
	},
	{
		ID: "openai", Name: "OpenAI", Icon: "openai", Kind: KindVendor,
		Chat:    "https://api.openai.com/v1",
		Website: "https://platform.openai.com", KeysURL: "https://platform.openai.com/api-keys",
		Models: []string{"gpt-5.5", "gpt-5.4-codex", "o4-mini"},
	},
	{
		ID: "google", Name: "Google Gemini", Icon: "gemini-color", Kind: KindVendor,
		Chat:    "https://generativelanguage.googleapis.com/v1beta/openai",
		Note:    "Gemini Developer API",
		Website: "https://aistudio.google.com", KeysURL: "https://aistudio.google.com/apikey",
		Models: []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-2.5-pro"},
	},
	{
		// Vertex AI 没有统一 base url：请求落在用户自己的 Google Cloud 项目里，
		// 由 gcloud 凭据签名，所以 Chat/Anthropic 留空，靠 Note 说明。
		ID: "vertex", Name: "Google Vertex AI", Short: "Vertex AI", Icon: "vertexai-color", Kind: KindVendor,
		Note:    "your Google Cloud project, with gcloud's sign-in",
		Website: "https://docs.cloud.google.com/gemini-enterprise-agent-platform/models",
		Models: []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-4-argon",
			"claude-opus-5-5", "claude-sonnet-5-5", "deepseek-ai/deepseek-v4.1-flash-maas", "zai-org/glm-5.2-maas"},
	},
	{
		ID: "deepseek", Name: "DeepSeek", Icon: "deepseek-color", Kind: KindVendor,
		Chat:      "https://api.deepseek.com/v1",
		Anthropic: "https://api.deepseek.com/anthropic",
		Website:   "https://platform.deepseek.com", KeysURL: "https://platform.deepseek.com/api_keys",
		Models: []string{"deepseek-chat", "deepseek-reasoner", "deepseek-v4-flash"},
	},
	{
		ID: "xai", Name: "xAI", Icon: "xai", Kind: KindVendor,
		Chat:      "https://api.x.ai/v1",
		Anthropic: "https://api.x.ai",
		Website:   "https://console.x.ai", KeysURL: "https://console.x.ai",
		Models: []string{"grok-4", "grok-4-fast", "grok-3"},
	},
	{
		ID: "moonshot", Name: "Kimi", Icon: "kimi", Kind: KindVendor,
		Chat:      "https://api.moonshot.ai/v1",
		Anthropic: "https://api.moonshot.ai/anthropic",
		Website:   "https://platform.kimi.ai", KeysURL: "https://platform.kimi.ai/console/api-keys",
		Models: []string{"kimi-k3", "kimi-k2.8-preview", "moonshot-v1-32k"},
	},
	{
		// Kimi Code 是会员订阅：会员才能用其专属 coding 端点，价格不公开，
		// 所以 Plan.PriceUSD 填 0 并在 Notes 里写清楚。
		ID: "kimi-code", Name: "Kimi Code", Icon: "kimi", Kind: KindVendor,
		Chat:      "https://api.kimi.ai/coding/v1",
		Anthropic: "https://api.kimi.ai/coding",
		Note:      "Membership",
		Website:   "https://www.kimi.ai/code", KeysURL: "https://www.kimi.ai/code/console",
		Models: []string{"kimi-k3", "kimi-for-coding", "kimi-k2.8-preview"},
		Plan:   &Plan{Name: "Kimi Code Membership", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "会员订阅价格未查到，以官网为准。"},
	},
	{
		// 智谱 GLM 的 Coding Plan 走独立端点，普通 pay-as-you-go key 会被拒。
		ID: "zhipu", Name: "Zhipu GLM", Icon: "zhipu-color", Kind: KindVendor,
		Chat:      "https://open.bigmodel.cn/api/paas/v4",
		Anthropic: "https://open.bigmodel.cn/api/anthropic",
		Website:   "https://open.bigmodel.cn", KeysURL: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys",
		Models: []string{"glm-5.3", "glm-5.3-flash", "glm-5.2"},
		Plan:   &Plan{Name: "GLM Coding Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "Coding Plan 端点：https://open.bigmodel.cn/api/coding/paas/v4；价格未查到，以官网为准。"},
	},
	{
		ID: "zai", Name: "Z.ai", Icon: "zai", Kind: KindVendor,
		Chat:      "https://api.z.ai/api/paas/v4",
		Anthropic: "https://api.z.ai/api/anthropic",
		Website:   "https://z.ai", KeysURL: "https://z.ai/manage-apikey/apikey-list",
		Models: []string{"glm-5.3", "glm-5.2", "glm-5.1"},
		Plan:   &Plan{Name: "Z.ai Coding Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "Coding Plan 端点：https://api.z.ai/api/coding/paas/v4；价格未查到，以官网为准。"},
	},
	{
		ID: "minimax", Name: "MiniMax", Icon: "minimax-color", Kind: KindVendor,
		Chat:      "https://api.minimax.io/v1",
		Anthropic: "https://api.minimax.io/anthropic",
		Website:   "https://platform.minimax.io", KeysURL: "https://platform.minimax.io/user-center/basic-information/interface-key",
		Models: []string{"minimax-m3", "minimax-m2.7", "abab7-chat"},
		Plan:   &Plan{Name: "MiniMax Coding Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "订阅价格未查到，以官网/控制台为准。"},
	},
	{
		// Step Plan 的 key 只能在 step_plan 端点花，pay-as-you-go 端点会拒绝。
		ID: "stepfun", Name: "StepFun", Icon: "stepfun-color", Kind: KindVendor,
		Chat:      "https://api.stepfun.ai/step_plan/v1",
		Anthropic: "https://api.stepfun.ai/step_plan",
		Website:   "https://platform.stepfun.ai", KeysURL: "https://platform.stepfun.ai/interface-key",
		Models: []string{"step-3", "step-3-mini", "step-2.5"},
		Plan:   &Plan{Name: "Step Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "Step Plan 订阅价格未查到，以官网为准；pay-as-you-go 端点：https://api.stepfun.ai/v1。"},
	},
	{
		ID: "xiaomi", Name: "Xiaomi MiMo", Icon: "mimocode", Kind: KindVendor,
		Chat:      "https://token-plan-cn.xiaomimimo.com/v1",
		Anthropic: "https://token-plan-cn.xiaomimimo.com/anthropic",
		Note:      "Token Plan · pay as you go",
		Website:   "https://platform.xiaomimimo.com", KeysURL: "https://platform.xiaomimimo.com/token-plan",
		Models: []string{"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.5"},
		Plan:   &Plan{Name: "Xiaomi MiMo Token Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "Token Plan 价格未查到，以官网为准。"},
	},
	{
		ID: "qwen", Name: "Qwen", Icon: "qwen-color", Kind: KindVendor,
		Chat:      "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		Anthropic: "https://dashscope-intl.aliyuncs.com/apps/anthropic",
		Note:      "DashScope · intl",
		Website:   "https://modelstudio.console.alibabacloud.com", KeysURL: "https://modelstudio.console.alibabacloud.com/?tab=playground#/api-key",
		Models: []string{"qwen3.8-max", "qwen3.8-flash", "qwen3.7-max", "qwen3.6-flash"},
	},
	{
		ID: "qwen-token-plan", Name: "Qwen Token Plan", Short: "Qwen Plan", Icon: "qwen-color", Kind: KindPlan,
		Chat:      "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1",
		Anthropic: "https://token-plan.maas.qianwenaiapi.com/apps/anthropic",
		Note:      "Qwen AI platform · subscription",
		Website:   "https://platform.qianwenai.com/docs/token-plan/overview", KeysURL: "https://platform.qianwenai.com/home/analytics/token-plan/individual",
		Models: []string{"auto", "qwen3.8-max", "qwen3.8-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.6-flash", "deepseek-v4.1-flash", "glm-5.3"},
		Plan:   &Plan{Name: "Qwen Token Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "订阅价格未查到，以平台订阅页为准。"},
	},
	{
		ID: "bailian-token-plan", Name: "Bailian Token Plan", Short: "Bailian Plan", Icon: "bailian-color", Kind: KindPlan,
		Chat:      "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		Anthropic: "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic",
		Note:      "Alibaba Cloud Bailian · subscription",
		Website:   "https://help.aliyun.com/zh/model-studio/token-plan-overview", KeysURL: "https://bailian.console.aliyun.com/cn-beijing/subscription/token-plan/personal",
		Models: []string{"auto", "qwen3.8-max", "qwen3.8-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.6-flash", "deepseek-v4.1-flash", "glm-5.3"},
		Plan:   &Plan{Name: "Bailian Token Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "订阅价格未查到，以阿里云百炼订阅页为准。"},
	},
	{
		// 火山方舟 Coding / Agent Plan 端点；pay-as-you-go 是 /api/v3。
		ID: "volcengine", Name: "Volcengine Ark", Icon: "volcengine-color", Kind: KindVendor,
		Chat:      "https://ark.cn-beijing.volces.com/api/coding/v3",
		Anthropic: "https://ark.cn-beijing.volces.com/api/coding",
		Note:      "火山方舟 · Coding / Agent Plan",
		Website:   "https://www.volcengine.com/docs/82379/1925114", KeysURL: "https://ark.volcengine.com/region:cn-beijing/apikey",
		Models: []string{"ark-code-latest", "doubao-seed-evolving", "doubao-seed-2.1-pro", "deepseek-v4.1-flash", "kimi-k3", "glm-5.3"},
		Plan:   &Plan{Name: "Volcengine Ark Coding Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "Coding/Agent Plan 价格未查到，以火山方舟控制台为准。"},
	},
	{
		ID: "tencent-cloud", Name: "Tencent Cloud", Icon: "tencentcloud-color", Kind: KindVendor,
		Chat:      "https://tokenhub.tencentmaas.com/plan/v3",
		Anthropic: "https://tokenhub.tencentmaas.com/plan/anthropic",
		Note:      "Token Plan · pay as you go",
		Website:   "https://cloud.tencent.com/document/product/1823/130060", KeysURL: "https://console.cloud.tencent.com/tokenhub/tokenplan",
		Models: []string{"tc-code-latest", "auto", "glm-5.3", "kimi-k3", "deepseek-v4.1-flash", "minimax-m3", "hy4-preview"},
		Plan:   &Plan{Name: "Tencent Cloud Token Plan", PriceUSD: 0, Period: "month", WindowHours: 0, Notes: "个人版/企业版 Token Plan 价格未查到，以腾讯云控制台为准。"},
	},
	{
		ID: "longcat", Name: "LongCat", Icon: "longcat", Kind: KindVendor,
		Chat:      "https://api.longcat.chat/openai/v1",
		Anthropic: "https://api.longcat.chat/anthropic",
		Website:   "https://longcat.chat/platform/docs/zh/api-docs", KeysURL: "https://longcat.chat/platform/api_keys",
		Models: []string{"longcat-flash", "longcat-pro", "longcat-reasoner"},
	},
	{
		ID: "mistral", Name: "Mistral", Icon: "mistral-color", Kind: KindVendor,
		Chat:    "https://api.mistral.ai/v1",
		Website: "https://console.mistral.ai", KeysURL: "https://console.mistral.ai/api-keys",
		Models: []string{"mistral-large-latest", "mistral-medium-latest", "codestral-latest"},
	},
	{
		ID: "groq", Name: "Groq", Icon: "groq", Kind: KindVendor,
		Chat:    "https://api.groq.com/openai/v1",
		Website: "https://console.groq.com", KeysURL: "https://console.groq.com/keys",
		Models: []string{"llama-4-maverick", "llama-4-scout", "mixtral-8x7b"},
	},
	{
		ID: "openrouter", Name: "OpenRouter", Icon: "openrouter", Kind: KindRelay,
		Chat:      "https://openrouter.ai/api/v1",
		Anthropic: "https://openrouter.ai/api",
		Website:   "https://openrouter.ai", KeysURL: "https://openrouter.ai/keys",
		Models: []string{"anthropic/claude-sonnet-5-5", "openai/gpt-5.5", "google/gemini-3.8-flash"},
	},
	{
		ID: "siliconflow", Name: "SiliconFlow", Icon: "siliconcloud-color", Kind: KindRelay,
		Chat:    "https://api.siliconflow.cn/v1",
		Website: "https://cloud.siliconflow.cn", KeysURL: "https://cloud.siliconflow.cn/account/ak",
		Models: []string{"deepseek-ai/deepseek-v4-flash", "z-ai/glm-5.3", "qwen/qwen3.8-max"},
	},
	{
		// Bedrock 按区域提供端点，这里给 us-east-1 作为默认；模型列表来自文档。
		ID: "bedrock", Name: "Amazon Bedrock", Icon: "bedrock-color", Kind: KindVendor,
		Chat:      "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1",
		Anthropic: "https://bedrock-runtime.us-east-1.amazonaws.com/anthropic",
		Note:      "Bedrock API key",
		Website:   "https://aws.amazon.com/bedrock/", KeysURL: "https://console.aws.amazon.com/bedrock/home#/api-keys",
		Models: []string{"global.anthropic.claude-opus-5-5", "global.anthropic.claude-sonnet-5-5", "global.openai.gpt-6-astra", "openai.gpt-oss-120b-1:0"},
	},
	{
		// Azure OpenAI 的地址是用户自己的资源，没有固定 URL；这里留空并用 Note 说明。
		ID: "azure", Name: "Azure OpenAI", Icon: "azure-color", Kind: KindVendor,
		Note:    "your resource's endpoint and key",
		Website: "https://ai.azure.com", KeysURL: "https://portal.azure.com/#view/Microsoft_Azure_ProjectOxford/CognitiveServicesHub/~/OpenAI",
		Models: []string{"gpt-5.5", "gpt-4.1", "o4-mini"},
	},
	{
		ID: "ollama", Name: "Ollama", Icon: "ollama", Kind: KindLocal, NoKey: true,
		Chat:      "http://localhost:11434/v1",
		Anthropic: "http://localhost:11434",
		Note:      "your local models",
		Website:   "https://ollama.com",
		Models:    []string{"llama3.3", "qwen3", "mistral"},
	},
	{
		ID: "ollama-cloud", Name: "Ollama Cloud", Icon: "ollama", Kind: KindVendor,
		Chat:      "https://ollama.com/v1",
		Anthropic: "https://ollama.com",
		Note:      "cloud models, with an API key",
		Website:   "https://docs.ollama.com/cloud", KeysURL: "https://ollama.com/settings/keys",
		Models: []string{"llama3.3", "qwen3", "mistral"},
	},
	{
		ID: "lmstudio", Name: "LM Studio", Icon: "lmstudio", Kind: KindLocal, NoKey: true,
		Chat:    "http://localhost:1234/v1",
		Note:    "local server, port 1234 by default",
		Website: "https://lmstudio.ai",
		Models:  []string{"local-model", "qwen2.5", "llama3.2"},
	},
	{
		ID: "nous", Name: "Nous Portal", Icon: "nousresearch", Kind: KindVendor,
		Chat:    "https://inference-api.nousresearch.com/v1",
		Note:    "Nous Research · Hermes and other models",
		Website: "https://portal.nousresearch.com", KeysURL: "https://portal.nousresearch.com",
		Models: []string{"hermes-4", "hermes-3", "deepseek-v4-flash"},
	},
	{
		ID: "together", Name: "Together AI", Icon: "together-color", Kind: KindRelay,
		Chat:    "https://api.together.xyz/v1",
		Website: "https://api.together.ai", KeysURL: "https://api.together.ai/settings/api-keys",
		Models: []string{"meta-llama/llama-4-maverick", "qwen/qwen3.8-max", "deepseek-ai/deepseek-v4-flash"},
	},
	{
		// GitHub Copilot 的 OpenAI 兼容端点在 api.githubcopilot.com（不是 GitHub 主站）。
		ID: "github-copilot", Name: "GitHub Copilot", Short: "Copilot", Icon: "github", Kind: KindVendor,
		Chat:    "https://api.githubcopilot.com",
		Note:    "GitHub account · Copilot models",
		Website: "https://github.com/features/copilot", KeysURL: "https://github.com/settings/copilot",
		Models: []string{"gpt-5.5", "gpt-4.1", "claude-sonnet-5-5"},
	},
	{
		// Claude Code 本身不是一个独立 API 厂商，而是用 Anthropic 官方端点的订阅入口。
		// Pro/Max 是同一预设下的两档订阅，Plan 只放一个指针，所以把两档写进 Notes。
		ID: "claude-code", Name: "Claude Code", Short: "Claude Code", Icon: "claudecode-color", Kind: KindPlan,
		Anthropic: "https://api.anthropic.com",
		Website:   "https://www.anthropic.com/claude-code", KeysURL: "https://console.anthropic.com/settings/keys",
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"},
		Plan:   &Plan{Name: "Pro / Max", PriceUSD: 20, Period: "month", WindowHours: 5, Notes: "Pro $20/月；Max 另有更高档位（$100/月起），窗口与额度以官网为准。"},
	},
	{
		// Codex 用 OpenAI 的 Responses API（chatgpt.com 后端），订阅是 ChatGPT Plus/Pro。
		ID: "codex", Name: "Codex", Short: "Codex", Icon: "codex-color", Kind: KindPlan,
		Chat:    "https://chatgpt.com/backend-api/codex",
		Website: "https://chatgpt.com/codex", KeysURL: "https://platform.openai.com/api-keys",
		Models: []string{"gpt-5.5-codex", "gpt-5.4-codex", "o4-mini"},
		Plan:   &Plan{Name: "ChatGPT Plus / Pro", PriceUSD: 20, Period: "month", WindowHours: 0, Notes: "Plus $20/月，Pro $200/月；以 OpenAI 官网为准。"},
	},
}

// presets 返回预设表。
//
// 直接返回切片而不是拷贝：调用方只读，拷贝一份 33 条记录的表在
// 每次 UI 渲染时都是白费。真要改的场合（加自定义供应商）在上层
// 自己 append，不要动这里。
func presets() []Preset { return presetList }
