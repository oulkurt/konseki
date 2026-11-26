# config/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
schema.go: 定义设置契约。全指针线上结构叠到默认值。读取时完成迁移和归一化，并为新建供应商提供不可复用的随机 ID 分配。空列表写成 []，重读不会恢复默认值。脱敏总开关关闭时返回空规则。键鼠计数默认关闭。旧键 inputMonitor 不迁移。
schema_test.go: 验证 v1 迁移、备份与字段归一化。验证缺失值、显式零值和空列表往返。验证旧键被丢弃、键鼠计数设置往返及重建供应商不复用旧 ID。
read_test.go: 验证 Read 不写文件、常量内存流式跳过大体积或异常事务密钥字段、缺失时返回默认值、脱敏总开关关闭时规则为空。
store.go: 通过目录级 OS 锁、.config.tx.json 元数据与 0600 .secrets.tx 密钥预写文件协调双文件原子提交，按版本校验、锁内重读、随机持久 ID 与 unclaimed| 隔离保护高版本未知字段与多实例并发；Read 只读且不写文件。
tx_stream.go: 提供 O(1) 常量内存的逐字节 JSON 流式读取，仅从 .config.tx.json 提取 config 字段并忽略其余字段。
lock_unix.go: Unix 平台下配置目录独占文件锁。
lock_windows.go: Windows 平台下配置目录独占系统锁。
lock_other.go: 非 Unix 非 Windows 平台下配置目录锁回退实现。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
