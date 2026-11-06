# providers/
> L2 | 父级: ../CLAUDE.md
providers.go: 定义公开目录类型，查询预设并展开区域端点。
presets.go: 由离线脚本生成预设数据，初始化 table.go 的切片。
table.go: 声明目录存储，隔离生成数据与公开 API。
local.go: 只读发现 CLI 凭据；钥匙串调用可替换。
subscription.go: 读取本机 Claude Code 用量，估算滚动窗口。
providers_test.go: 用假钥匙串和临时目录验证目录及凭据解析。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
