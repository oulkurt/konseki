# llm/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
llm.go: 调用 OpenAI 兼容及 Anthropic 模型接口。暴露错误种类。摘要层负责脱敏。
llm_test.go: 验证流式取消、协议对齐、URL 拼接、鉴权头、thinking 守卫与端点解析。
resolve.go: 把供应商预设和用户配置解析为完整请求参数或仅解析端点。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
