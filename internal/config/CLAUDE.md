# config/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
schema.go: 定义设置契约。全指针线上结构叠到默认值。读取时完成迁移和归一化。空列表写成 []，重读不会恢复默认值。脱敏总开关关闭时返回空规则。键鼠计数默认关闭。旧键 inputMonitor 不迁移。
schema_test.go: 验证 v1 迁移、备份与字段归一化。验证缺失值、显式零值和空列表往返。验证旧键被丢弃和键鼠计数设置往返。
read_test.go: 验证 Read 不写文件、缺失时返回默认值、脱敏总开关关闭时规则为空。
store.go: 持久化设置，密钥另存 0600 的 secrets.json。两者均用临时文件加改名写盘。Update 先归一化再落盘。Read 只读设置，缺失时返回默认值。Read 不写文件，也不读密钥。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
