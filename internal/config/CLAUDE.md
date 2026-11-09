# config/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
schema.go: 定义设置契约。读文件时经全指针线上结构叠到默认值上，一次完成迁移和归一化；列表清空后写成 [] 而不是 null，重读不会被默认值顶回；RedactConfig.Options 在总开关关闭时返回空规则；键鼠计数默认关闭，旧键 inputMonitor 不迁移。
schema_test.go: 验证 v1 迁移与备份、缺失和显式零值、逐字段归一化、清空列表的往返，以及旧键 inputMonitor 被丢弃、inputCounts 往返不变。
read_test.go: 验证 Read 不写文件、缺失时返回默认值、脱敏总开关关闭时规则为空。
store.go: 持久化设置，密钥另存 0600 的 secrets.json，都用临时文件加改名写盘；Update 先归一化再落盘；Read 只读设置，缺失时返回默认值，不写文件、不读密钥。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
