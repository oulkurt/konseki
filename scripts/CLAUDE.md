# scripts/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
sync-presets.py: 同步供应商预设，生成带契约及标准生成标记的 Go 数据并运行 gofmt。
live/: 驱动测试构建的原生窗口，使用 Unix 套接字。
render-icon.sh: 用无头 Chrome 渲染两张 1024 透明图。docs/icon.svg 生成 resources/icon.png。docs/icon.svg#dev 生成 resources/icon-dev.png。脚本先校验 XML、Chrome 路径、尺寸和图像差异。全部通过后才替换目标文件。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
