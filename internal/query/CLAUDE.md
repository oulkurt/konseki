# query/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
query.go: 解析命令与 flag，装配设置、采集、摘要和遮蔽器；用法错误返回 2，运行错误返回 1，数据目录缺失时不创建。
commands.go: 生成 status、days、day、snapshots 的 JSON 视图；字符字段逐个遮蔽，快照按半开时间窗筛选，重复 OCR 只标 ocrRepeat；会话和快照保留键鼠计数的缺失与零值。
search.go: 在摘要字段、窗口标题和屏幕文字里找候选，遮蔽整段后重新定位；命令内固定容量复用重复字段，按摘要标题合并同源命中，按新到旧排序并截断。
link.go: 用独占临时目录原子维护数据目录里的 konseki 软链接；新建、原地不动、替换到别处、拒绝覆盖普通文件并暴露错误种类。
query_test.go: 用两天夹具断言四条命令的结构与值、空列表、只读指纹、用法错误、缺失目录、帮助语境、Unicode、DST、会话独占日期、半开时间上界、键鼠计数的缺失与零值，以及 Link 的元信息与覆盖边界。
search_test.go: 验证合并、排序、截断、遮蔽值内的命中丢弃和 flag 位置。
redact_test.go: 在五个命令的输出里验证密钥、邮箱、词和放行值，以及总开关关闭后的原文。
fixture_test.go: 用采集和摘要的写入接口造两天数据，固定 Env.Now。
example_test.go: KONSEKI_QUERY_EXAMPLE 指向文件时，把五条命令和输出写成示例。
perf_test.go: KONSEKI_QUERY_PERF=1 时用 7 天 × 2000 条快照测无命中和重复 OCR 命中的搜索中位数，门槛 2 秒。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
