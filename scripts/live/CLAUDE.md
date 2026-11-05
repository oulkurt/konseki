# live/
> L2 | 父级: ../CLAUDE.md
client.go: 封装 mygo 自动化协议，校验回复编号和错误；idle 总带上限，因为插件把 0 当作无限等待。
client_test.go: 用 Unix 假服务端验证请求编码与回复匹配，并覆盖启动配置、套接字占用和 CPU 时间解析，不启动图形程序。
main.go: launch 只接受空 home 和无人监听的套接字；perf 的平均 CPU 取累计 CPU 时间增量，不平均 macOS 的衰减 %cpu。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
