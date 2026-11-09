# Konseki - 本机活动记录器
Go 1.27.3 + mygo v0.7.4
<directory>
./ - package main 负责界面与生命周期；main.go 先分流查询命令再组合服务，cli.go 实现命令分流和软链接，e2e.go 在测试构建中注册窗口自动化服务，e2e_frames.go 在测试构建中记录粉碎动画的帧间隔（正式构建用 e2e_frames_off.go 的空实现），shell.go 编排时间轴、设置导航和分页，widgets.go 提供共用控件，settings_capture.go、settings_recorder.go、settings_privacy.go、settings_permissions.go、settings_sessions.go、settings_models.go、settings_language.go、settings_agent.go 和 settings_danger.go 呈现设置，ui_provider.go 与 provider_requests.go 负责供应商配置和请求，generator.go 负责摘要生成器，timeline_model.go 聚合小票数据，receipt.go 编排打印机与送纸，receipt_parts.go 绘制纸面，receipt_shred.go 与 receipt_shred_physics.go 绘制碎纸并模拟绳索，settings_shots_test.go 输出设置和动画截图，icons.go 提供控件图标，recorder_settings.go 连接采集设置。
根目录测试：repo_gate_test.go 检查仓库门禁；background_test.go、cli_test.go、main_test.go、render_test.go 验证应用与入口；lang_test.go 验证双语；receipt_animation_test.go、receipt_shred_test.go、receipt_test.go 验证小票；settings_agent_test.go、settings_bench_test.go、settings_danger_test.go、settings_recorder_test.go 验证设置；timeline_bench_test.go、timeline_model_test.go 验证时间轴；shots_test.go 保存视觉基线。
internal/ - 采集、配置、模型、摘要、脱敏、只读查询及旧导出清理
skills/ - 安装到 Agent 的 Konseki 技能说明
locales/ - 简体中文与英文消息目录和生成的类型化访问器
scripts/ - 开发脚本；scripts/live/ 驱动测试构建的原生窗口，render-icon.sh 渲染应用图标和开发版图标
docs/ - 架构与决策文档，以及应用图标母版 icon.svg
resources/ - 应用资源。mygo build 把本目录原样复制进应用包，所以这里不放 CLAUDE.md；icon.png 是应用图标，icon-dev.png 只给 mygo dev 用，不进应用包
</directory>
<config>
go.mod - Go 模块与依赖版本
mygo.json - 原生应用构建和语言目录配置
</config>
