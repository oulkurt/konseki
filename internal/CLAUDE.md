# internal/
> L2 | 父级: ../CLAUDE.md
capture/: 定义采集契约，保存快照、会话与 OCR；提供只读打开、日期列表与截图路径。
config/: 迁移和归一化设置，分离保存密钥；提供不写文件的只读读取。
llm/: 调用模型服务。
providers/: 定义模型供应商预设。
query/: 给 Agent 的只读命令，输出 JSON 并遮蔽文字字段。
redact/: 遮蔽与还原敏感文本。
skill/: 识别并删除旧版技能导出的文件，不写新文件。
summary/: 基于采集证据生成摘要。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
