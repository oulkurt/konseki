# live/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
client.go: 封装 mygo 自动化协议，校验回复编号和错误；idle 总带上限，因为插件把 0 当作无限等待。
client_test.go: 用 Unix 假服务端验证请求编码与回复匹配，并覆盖启动配置、真实子进程许可环境、套接字占用、种子摘要和 CPU 时间解析，不启动图形程序。
main.go: -recorder 显式授权 e2e 真实采集，默认关闭，子进程清除继承许可并仅传 0 或 1；launch 只接受空 home 和无人监听的套接字，旧版导出目录也指进 home（清除全部数据会删除里面的旧版导出文件），-seed 在启动前写入今天的三段摘要，seed 给运行中的 App 补同样的数据；tree、act、key 按控件键名或节点编号操作窗口；perf 的平均 CPU 取累计 CPU 时间增量，不平均 macOS 的衰减 %cpu。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
