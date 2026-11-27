# summary/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
summary.go: 按时间窗生成摘要（含窗口覆盖追踪与提交边界 cfgSeq/代次防线），屏幕文本作为不可信数据。LLMConfig 透传鉴权方式。
store.go: 带目录级 .gen 代次隔离、严格读检验、提交守卫与只读保护的 JSONL 摘要存储。
lock_unix.go: Unix 平台下摘要目录独占文件锁。
lock_windows.go: Windows 平台下摘要目录独占系统锁。
lock_other.go: 非 Unix 非 Windows 平台下摘要目录独占锁回退实现。
summary_test.go: 验证摘要生成、存储、代次隔离、覆盖更新和脱敏行为。
prompt_test.go: 验证屏幕文本边界、双语声明，以及边界字符不截断脱敏。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
