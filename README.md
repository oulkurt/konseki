# 痕跡 Konseki

macOS 本机活动时间轴。Go + mygo 原生界面。

记录器总开关控制采集。采集根据前台窗口、输入、闲置、锁屏和敏感应用决定时机；抓帧去重后保存 JPEG 与 JSONL，OCR 在有界队列异步执行。会话记录启动、暂停及结束边界。保留天数与磁盘配额约束历史数据，维护任务降质和淘汰旧截图。

时间轴聚合应用时间块。模型根据采集证据生成摘要，小票界面呈现摘要并支持碎纸交互。设置提供采集、权限、隐私、会话、模型、Agent 和简体中文／英文选项。

摘要请求把脱敏文字发送给所选模型服务。本机记录包含敏感内容；截图可直接读取且不脱敏。Agent 读取后的传输取决于该 Agent 自身行为。

## Agent 安装

先启动应用建立查询命令链接。使用独立 skills CLI 安装说明：

```sh
npx skills add /path/to/konseki --skill konseki -g -y
```

将占位路径替换为此仓库检出。发布到 main 后才可使用远程仓库地址替换本地路径。canonical 技能位于 `~/.agents/skills/konseki`；工具可通过链接或 universal 路径发现它，不要求每个 Agent 目录都有副本。

```sh
export KONSEKI_HOME="/path/to/data" # 仅自定义目录时需要
"$KONSEKI_HOME/konseki" query status
```

默认目录为 `~/Library/Application Support/Konseki`。`query status/days/day/snapshots/search` 输出只读 JSON；文本按隐私设置脱敏，截图路径指向原始可读 JPEG。技能只安装说明，不静态导出历史记录。

## 开发

```sh
CGO_ENABLED=0 go test ./...
go build ./...
go test -tags e2e ./...
go build -tags e2e ./...
```

e2e 构建默认阻断真实抓屏与权限请求。只有 `KONSEKI_E2E_ALLOW_CAPTURE=1` 才放行；scripts/live 的 `-recorder` 显式授权，默认关闭。正式构建不受此测试开关影响。平台适配存在不代表各平台全功能可用。
