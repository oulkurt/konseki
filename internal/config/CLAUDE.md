# config/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本模块的成员与职责边界。
schema.go: 定义设置契约。读文件时经全指针线上结构叠到默认值上，一次完成迁移和归一化；列表清空后写成 [] 而不是 null，重读不会被默认值顶回。
schema_test.go: 验证 v1 迁移与备份、缺失和显式零值、逐字段归一化、清空列表的往返。
store.go: 持久化设置，密钥另存 0600 的 secrets.json，都用临时文件加改名写盘；Update 先归一化再落盘。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
