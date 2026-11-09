# live/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
client.go: 封装 mygo 自动化协议，校验回复编号和错误；idle 总带上限，因为插件把 0 当作无限等待。
client_test.go: 用 Unix 假服务端验证协议编码与回复。验证启动配置、子进程许可环境和套接字占用。验证种子摘要与 CPU 时间解析。不启动图形程序。
main.go: -recorder 显式授权 e2e 真实采集，默认关闭。子进程清除继承许可，仅传 0 或 1。launch 要求空 home 和无人监听的套接字。旧版导出目录指进 home，清空时会删除旧文件。-seed 在启动前写入今天的三段摘要。seed 为运行中的 App 写入相同数据。tree、act、key 按控件键名或节点编号操作窗口。perf 用累计 CPU 时间增量计算平均值。不平均 macOS 衰减后的 %cpu。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
