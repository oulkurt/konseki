# 架构

应用使用 Go 与 mygo 构建原生界面。main 在创建窗口前分流 query，再组合 capture、config、summary 与模型服务。shell 编排时间轴和设置，小票组件呈现摘要。

界面跨帧保存模型数据、控件状态与 ui.Handle。ui.Context 和本轮 Element 不进入后台任务。后台任务通过 app.post 发布结果；该回调绑定 Window.Update，让界面线程接收状态更新。供应商探测和模型请求不阻塞界面构建。

capture.Machine 是纯状态机。它接收时间和观测，输出采集、探测及会话动作。Runner 读取系统观测，执行动作并反馈结果。Screen、OCREngine、观测源与时钟接口隔离平台能力，测试可注入假依赖。macOS 后端连接 CoreGraphics 与 Vision；其他平台的现有桩不构成全功能承诺。

抓帧任务先去重，再保存 JPEG 与快照。OCR 队列保留原分辨率帧，不从持久化 JPEG 回读识别。JPEG 编码单独缩放和压缩，维护任务继续降低旧截图质量。识别质量与磁盘成本因此各有边界。有界队列和会话抽样限制 OCR 积压。

Store 按天向快照、OCR 和会话 JSONL 追加记录。JPEG 独立存储，快照通过路径引用它。会话保留启动、暂停和结束边界。维护任务按保留天数与磁盘配额淘汰历史记录，不删除设置和摘要。

summary 聚合采集证据并调用 llm。脱敏总开关开启时，请求按所选规则遮蔽文字；关闭时发送原文。响应中的占位符在本机还原。供应商设置与密钥分开保存。截图保持可读图像，不执行图像脱敏。

query 通过 config.Read 读取不含密钥的设置。它只读打开采集与摘要存储，不创建数据目录，不启动采集或模型请求，也不扫描 CLI 凭据。命令输出 JSON，并按脱敏设置遮蔽文字。截图路径指向可读 JPEG。Agent 读取之后是否外传取决于该 Agent。

skills/konseki 只含技能说明。独立 skills CLI 安装到 canonical universal 目录，Agent 按需执行 query。应用不再写静态历史导出。internal/skill 按固定内容标记清理旧导出，保留新技能、用户文件和软链接。

e2e 构建的许可策略默认拒绝，仅环境值 1 放行。生产 Screen 构造器包装平台后端，在 Grab 与 Permission 前检查许可；公开授权入口同样先检查。live 启动命令清除继承许可，仅按 -recorder 写入 0 或 1。正式构建恒允许，不读取该变量。行为测试替换后端，放行测试也不调用原生 API。
