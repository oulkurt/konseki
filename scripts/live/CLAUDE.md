# live/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
client.go: 封装 mygo 自动化协议，串行化请求并在流损坏后快速失败，为读写设置截止时间，校验回复编号、空结果与错误；idle 拒绝非正数超时并将正的亚毫秒超时钳位到至少 1ms，防止插件当作无限等待。
client_test.go: 用原生 TestMain 子进程与 Unix 假服务端验证协议编码、亚毫秒 idle、空结果错误、并发串行化、超时失活与回复。验证 act 校验、空窗口报错、home 规范化、提前退出错误输出、超时回收、启动失败回滚、子进程许可环境和套接字占用。验证种子摘要与 CPU 时间解析。不依赖外部 shell 且不启动图形程序。
proc_unix_test.go: Unix 平台下通过信号探测检测已超时子进程是否回收。
proc_windows_test.go: Windows 平台下通过进程句柄与退出码检测已超时子进程是否回收。
proc_other_test.go: 非 Unix 非 Windows 平台下进程存活检测桩实现。
main.go: -recorder 显式授权 e2e 真实采集，默认关闭。子进程清除继承许可，仅传 0 或 1，将子进程 stderr 重定向到隔离目录的 stderr.log 并在退出或超时失败时读取有界尾部日志；超时后杀死并回收子进程，启动失败时回滚新写的 config.json、summaries 与 stderr.log。规范化 -home 路径。严格校验 act 参数与 tree/shot 非空窗口。旧版导出目录指进 home，清空时会删除旧文件。-seed 在启动前写入今天的三段摘要。seed 为运行中的 App 写入相同数据。perf 用累计 CPU 时间增量计算平均值。不平均 macOS 衰减后的 %cpu。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
