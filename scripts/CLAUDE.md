# scripts/
> L2 | 父级: ../CLAUDE.md
sync-presets.py: 同步供应商预设，生成带契约及标准生成标记的 Go 数据并运行 gofmt。
live/: 驱动测试构建的原生窗口，使用 Unix 套接字。
render-icon.sh: 用无头 Chrome 把 docs/icon.svg 渲染成 resources/icon.png，把 docs/icon.svg#dev 渲染成 resources/icon-dev.png，都是 1024 透明底；先校验 XML、Chrome 路径、尺寸和两张图不同，全部通过才替换。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
