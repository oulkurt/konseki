# providers/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
providers.go: 定义公开目录类型，查询预设并展开区域端点。
presets.go: 由离线脚本生成预设数据，初始化 table.go 的切片。
table.go: 声明目录存储，隔离生成数据与公开 API。追加 Konseki 自有的 gemini-cli 预设。
local.go: 只读发现 Claude Code、Codex、Gemini CLI 凭据。过滤 Gemini 的 cloud-platform 权限。读取 Codex 模型目录。钥匙串调用可替换。
local_test.go: 用临时 HOME 验证 Gemini 凭据过滤与 Codex 模型目录解析。
subscription.go: 读取本机 Claude Code 用量，估算滚动窗口。
providers_test.go: 用假钥匙串和临时目录验证目录及凭据解析。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
