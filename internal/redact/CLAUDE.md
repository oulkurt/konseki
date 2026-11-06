# redact/
> L2 | 父级: ../CLAUDE.md
redact.go: 按 Options 预编译规则，用进程级 salt 生成稳定占位符；Mask 遮蔽、Restore 还原，反查表只留内存。
rules.go: 定义各 kind 的正则、边界校验和校验和检查，返回互不重叠的区间。
redact_test.go: 验证占位符稳定、往返还原、放行、私网地址、证件与银行卡校验和、误报和并发。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
