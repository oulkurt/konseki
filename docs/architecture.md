# 架构

应用层组合 capture、config、summary 和 query。main 在创建窗口前分流 query；查询不会启动采集器。shell 编排时间轴和设置，小票组件呈现摘要。

capture.Runner 用可替换依赖读取系统观测。状态机决定会话与采集动作，异步抓帧先去重再落盘；OCR 使用有界队列并按会话抽样。Store 按天保存快照、OCR、会话 JSONL 与 JPEG。维护任务执行保留、降质和配额淘汰，不删除设置和摘要。

summary 聚合采集证据，经 redact 遮蔽文字后调用 llm。供应商设置与密钥分开保存。时间轴从本机摘要和采集记录构建应用时间块、小票及会话。

query 只读打开设置和记录，输出 JSON；每次命令独立建立脱敏映射。截图不脱敏，Agent 可按路径读取。Agent 读取之后是否外传由其自身行为决定。

skills/konseki 仅含技能说明。独立 skills CLI 安装到 canonical universal 目录；应用只清理旧导出，不再输出静态技能数据。中英设置使用 locales 类型化消息目录。

e2e 构建在平台抓帧及权限入口检查显式环境许可；录制 UI 开关无法绕过。正式构建保持原行为。
