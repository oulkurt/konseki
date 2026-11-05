# config/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本模块的成员与职责边界。
schema.go: 定义设置契约，解析版本并归一化边界。
schema_test.go: 验证设置迁移、默认值与磁盘往返。
store.go: 持久化设置与独立密钥，使用临时文件替换。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
