# Konseki 文档

## mygo 框架文档

**拷贝在 `docs/mygo/`（107 篇），对应 mygo v0.7.4。**
这是从模块缓存复制进来的，和锁定版本严格一致。

在线版：https://mygo.egoist.dev/

### 常用入口

| 主题 | 路径 |
|---|---|
| 快速开始 | `docs/mygo/getting-started.md` |
| 原生 UI 总览 | `docs/mygo/native.md` |
| 界面组件 | `docs/mygo/ui/`（40+ 篇，每组件一篇） |
| Go ↔ 前端绑定 | `docs/mygo/bindings.md` |
| 打包分发 | `docs/mygo/distribution.md` |
| 菜单与托盘 | `docs/mygo/menus.md` |
| 自动更新 | `docs/mygo/updates.md` |
| 配置（mygo.json） | `docs/mygo/configuration.md` |
| 架构说明 | `docs/mygo/architecture.md` |

### 本项目会用到的组件

```
docs/mygo/ui/views.md         列/行/滚动容器
docs/mygo/ui/list.md          虚拟化列表（时间轴用它）
docs/mygo/ui/text.md          文本样式
docs/mygo/ui/button.md        按钮
docs/mygo/ui/outline.md       侧边栏大纲
docs/mygo/ui/markdown.md      摘要渲染
docs/mygo/ui/overlays.md      弹层
docs/mygo/ui/transitions.md   动画
docs/mygo/ui/accessibility.md 无障碍
```

**注意**：文档有滞后。实测 `ui.Fill` 常量不存在，是 `Fill()` 方法和 `FillWidth()`。
遇到 API 对不上时，以 `$(go env GOMODCACHE)/github.com/egoist/mygo@v0.7.4/ui/api_gen.go` 为准。

## 本项目自己的文档

| 文件 | 内容 |
|---|---|
| `../README.md` | 项目总览、当前进度、如何跑起来 |
| `architecture.md` | 代码分层与数据流 |
| `decisions.md` | 技术选型记录 |
