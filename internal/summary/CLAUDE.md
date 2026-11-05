# summary/
> L2 | 父级: ../CLAUDE.md
summary.go: 按时间窗生成摘要并存入 JSONL，屏幕文本作为不可信数据。
summary_test.go: 验证摘要生成、存储和脱敏行为。
prompt_test.go: 验证屏幕文本边界、双语声明，以及边界字符不截断脱敏。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
