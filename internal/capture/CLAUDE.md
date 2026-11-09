# capture/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本模块的成员与职责边界。
collector.go: 定义屏幕、OCR、系统观测、累计键鼠计数与时钟接口。
counts.go: 按隐私状态和完整区间计算快照与会话键鼠计数。
counts_test.go: 验证键鼠计数的隐私、读数和区间边界。
runner_counts_test.go: 验证采集器调用次数、会话计数和写盘回补。
dedup.go: 比较帧的亮度哈希、直方图和像素。
frame_other.go: 为非 macOS 平台提供帧生命周期桩。
dedup_test.go: 用合成画面验证去重阈值。
input_darwin_test.go: 验证 macOS 只读观测、累计键鼠计数和运行中应用列表，并测量窗口查询和累计读数耗时。
input_darwin.go: 读取输入间隔、累计键鼠计数、工作区前台、运行中应用、实时窗口所有者和锁屏状态；运行中应用只列有 Dock 图标的，供设置页加入敏感应用。
input_other.go: 为其他平台提供观测和授权后端桩。
contract_test.go: 验证存储兼容、过期清理和采集状态。
maintenance.go: 用可注入时钟调度维护；停止不等待，清空等待旧任务。
maintenance_test.go: 验证维护周期、退出与容量请求合并。
machine.go: 根据观测和实时敏感窗口决定会话、采集与探测动作。
live_test.go: 验证敏感窗口阻断抓屏和状态。
machine_test.go: 用人工时间验证采集决策。
macos.go: 绑定窗口、位图、JPEG 与 Vision 原语，不编排采集任务；文字识别先把图画进 IOSurface 像素缓冲再交给 Vision，Vision 直接收 CGImage 时同一张图每识别一次 RSS 涨一张图。
native_darwin_test.go: 用合成图和 Vision 样本验证原生管线与性能；KONSEKI_OCR_SOAK=1 时跑同一帧识别 300 次的 RSS 门槛。
testdata/timeline-empty-zh-Hans-light.png: Vision 中英文识别测试图。
ocrqueue.go: 限制 OCR 排队容量并按会话抽样。
ocrqueue_test.go: 验证 OCR 队列溢出、超时和抽样。
perms.go: 汇总平台权限和诊断信息。
pool_darwin.go: 将后台 Objective-C 调用绑定线程和自动释放池。
pool_test.go: 检查自动释放池的创建边界。
runner_behavior_test.go: 验证采集停止、清空残留、超额触发和 OCR 抽样。
runner_test.go: 用假依赖验证采集、识别和会话落盘。
runner.go: 协调采集生命周期、键鼠计数与异步抓帧；清空后的首次启动跳过恢复。
timeline.go: 按注入时钟读取快照并聚合时间轴。
screen_darwin.go: 提供受保护 Screen 的 macOS 后端，编排 macOS 帧、权限、图像编码与 OCR 生命周期。
screen_linux.go: 提供 Linux 平台采集适配。
screen_windows.go: 提供 Windows 平台采集适配。
sessions.go: 从最新快照向前扫描连续会话，只查之后的关闭记录。
sessions_test.go: 验证崩溃恢复、幂等性与异步启动。
recovery_performance_test.go: 验证恢复的分配量不随历史天数增长；耗时只记日志不断言，-race 加负载时耗时比值会越过阈值。
session.go: 定义会话关闭记录、可选键鼠计数与结束原因。
settings.go: 定义采集运行参数与键鼠计数开关，不依赖应用设置。
status.go: 定义采集状态和权限快照。
storage.go: 用量缓存、按天保留、超额淘汰、warm 1280×720 q40 与 cold 640×360 q30 重编码、清空；维护操作互斥，配额按磁盘实际总量删到上限的 90%，降级先读文件头只在需要时完整解码，只处理已落盘的 .jpg 和日期命名的 JSONL，不碰摘要与设置。
storage_test.go: 用临时目录和合成 JPEG 验证存储维护的磁盘行为，覆盖并发写入、删到一半中断、删除失败计数、降级幂等与解码次数；KONSEKI_STORAGE_PERF=1 时跑 300 张重编码的性能门槛。
store.go: 按天追加快照、OCR 与会话，区分键鼠计数缺失与零值；提供只读打开、快照与会话日期并集、快照读取、时间解析和截图路径。
capture_gate_e2e.go: 提供 e2e 许可策略；仅环境值 1 放行。
capture_gate_normal.go: 正式构建保持原有采集行为。
capture_gate_e2e_test.go: 无窗口验证默认阻断与显式授权。
capture_gate_normal_test.go: 验证正式构建不受环境开关影响。
capture_gate.go: 在生产 Screen 构造路径和公开授权入口统一检查许可；后端可替换。
capture_gate_test.go: 用假后端计数断言抓帧、权限参数与返回值。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
