# Konseki - 本机活动记录器
Go 1.27.3 + mygo v0.7.4
<directory>
./ - package main 负责界面与生命周期；main.go 组合服务，e2e.go 在测试构建中注册窗口自动化服务，shell.go 编排时间轴和三类设置导航，widgets.go 提供共用控件，settings_capture.go、settings_recorder.go、settings_privacy.go、settings_permissions.go、settings_sessions.go、settings_models.go、settings_general.go 和 settings_language.go 呈现设置，ui_provider.go 与 provider_requests.go 负责供应商配置和请求，ui_summary.go 与 generator.go 负责导出、导航和摘要生成器，timeline_model.go 聚合小票数据，receipt.go 编排打印机与送纸，receipt_parts.go 绘制纸面，receipt_shred.go 与 receipt_shred_physics.go 绘制碎纸并模拟绳索，settings_shots_test.go 输出设置和动画截图，icons.go 提供控件图标，recorder_settings.go 连接采集设置，settings_danger.go 串联安全清除。
internal/ - 采集、配置、模型、摘要及导出对账
locales/ - 简体中文与英文消息目录和生成的类型化访问器
scripts/ - 开发脚本；scripts/live/ 驱动测试构建的原生窗口
docs/ - 架构与决策文档
resources/ - 应用资源
</directory>
<config>
go.mod - Go 模块与依赖版本
mygo.json - 原生应用构建和语言目录配置
</config>
