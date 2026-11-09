# Konseki - 本机活动记录器
Go 1.27.3 + mygo v0.7.4
<directory>
./ - package main 组合服务与界面。main.go 分流查询命令并装配应用。cli.go 提供命令分流，并在启动时维护命令链接。ui_errors.go 按错误种类给出当前语言的错误文案。shell.go 编排页面导航。widgets.go 和 icons.go 提供共用控件与图标。设置页面：记录由 settings_capture.go、settings_recorder.go、settings_privacy.go、settings_permissions.go、settings_sessions.go、settings_danger.go 呈现；模型由 settings_models.go 呈现；通用由 settings_language.go、settings_agent.go 呈现。ui_provider.go 和 provider_requests.go 编排供应商配置与请求。generator.go 装配摘要生成器。recorder_settings.go 连接采集设置。timeline_model.go 聚合小票数据。receipt.go 编排打印机与送纸。receipt_parts.go 绘制纸面。receipt_shred.go 与 receipt_shred_physics.go 实现碎纸动画。e2e.go 注册测试构建的自动化服务。e2e_frames.go 记录测试构建的帧间隔。正式构建使用 e2e_frames_off.go 的空实现。
根目录测试：应用与入口由 background_test.go、cli_test.go、main_test.go、render_test.go 验证；语言由 lang_test.go、lang_states_test.go、ui_errors_test.go 验证；小票由 receipt_animation_test.go、receipt_shred_test.go、receipt_test.go 验证；设置由 settings_agent_test.go、settings_bench_test.go、settings_danger_test.go、settings_recorder_test.go 验证；时间轴由 timeline_bench_test.go、timeline_model_test.go 验证；视觉产物由 shots_test.go、settings_shots_test.go 生成；repo_gate_test.go 检查仓库门禁。
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
