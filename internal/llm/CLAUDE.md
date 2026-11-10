# llm/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
llm.go: 调用 OpenAI 兼容及 Anthropic 接口。按凭据类型选择鉴权头。暴露错误种类与模型列表。
llm_responses.go: 调用 OpenAI Responses 接口。ChatGPT/Codex 订阅与 OpenAI 新版端点走这里。
llm_gemini.go: 调用 Gemini generateContent 接口。Code Assist 订阅走这里。解析 Gemini 模型列表。
llm_protocols_test.go: 验证四种协议的鉴权头、请求体、响应解析与错误映射。不联网。
resolve.go: 把供应商预设和用户配置解析为协议、端点与鉴权方式。订阅端点在这里写死。
resolve_test.go: 验证协议选择与既有厂商的解析回归。不联网。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
