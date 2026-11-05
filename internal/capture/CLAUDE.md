# capture/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本模块的成员与职责边界。
collector.go: 定义屏幕与 OCR 接口，隔离平台实现。
contract_test.go: 验证存储兼容、过期清理和采集状态。
macos.go: 连接 macOS 原生屏幕与文字识别能力。
perms.go: 汇总平台权限和诊断信息。
runner.go: 协调采集循环与时间轴聚合。
screen_darwin.go: 提供 macOS 平台采集适配。
screen_linux.go: 提供 Linux 平台采集适配。
screen_windows.go: 提供 Windows 平台采集适配。
session.go: 定义会话关闭记录与结束原因。
settings.go: 定义采集运行参数，不依赖应用设置。
status.go: 定义采集状态和权限快照。
storage.go: 按日期清理采集数据并声明存储接口。
store.go: 快照、OCR、会话各写一份按天分文件的 JSONL，只追加；截图路径由快照 id 推出，读快照时按 id 回填 OCR，旧格式照常可读。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
