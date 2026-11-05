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
storage.go: 用量缓存、按天保留、超额淘汰、warm 1280×720 q40 与 cold 640×360 q30 重编码、清空；维护操作互斥，配额按磁盘实际总量删到上限的 90%，降级先读文件头只在需要时完整解码，只处理已落盘的 .jpg 和日期命名的 JSONL，不碰摘要与设置。
storage_test.go: 用临时目录和合成 JPEG 验证存储维护的磁盘行为，覆盖并发写入、删到一半中断、删除失败计数、降级幂等与解码次数；KONSEKI_STORAGE_PERF=1 时跑 300 张重编码的性能门槛。
store.go: 快照、OCR、会话各写一份按天分文件的 JSONL，只追加；截图路径由快照 id 推出，读快照时按 id 回填 OCR，旧格式照常可读。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
