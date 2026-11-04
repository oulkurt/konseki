# 技术选型记录

## 为什么用 mygo 而不是 Electron / Tauri

| | mygo | Electron | Tauri |
|---|---|---|---|
| 二进制 | 9.2 MB | ~150 MB | ~10 MB |
| 运行时 | 系统 WebView / 自绘 | 捆 Chromium | 系统 WebView |
| Go 直调 macOS API | 原生支持（purego） | 要走 native addon | 要走 Rust FFI |
| 跨平台 | Win/Linux/macOS 齐全 | 齐全 | 齐全 |

决定性因素是**第三行**：这个 App 的核心是采集（截屏 + OCR + 窗口枚举），
全都在系统层。mygo 用 `purego` 直调 Objective-C runtime，
采集逻辑可以完全写在 Go 里，不需要 Swift 桥或 Rust FFI。

Electron 要多一层 native addon，Tauri 要多写 Rust。

## 为什么用 native 模板而不是 web 模板

mygo 提供两种：
- `web`：TypeScript 前端 + Vite，WebView 渲染
- `native`：Go 自绘 UI，不依赖 WebView

选 native 的理由：
1. 时间轴是**列表**，不是复杂交互页面，自绘足够
2. 少一个 Node/Bun 工具链，构建更快、依赖更少
3. `ui.NewTester` 让界面能在测试里构建，不用起窗口

如果以后要做富文本摘要、Markdown 渲染、复杂动画，可以迁到 web 模板。
mygo 有 `migrate-ui` 命令支持这个迁移。

## 为什么 OCR 用全分辨率但存缩略图

见 `../../aside-lite/docs/findings.md` 第 5 节，那里有完整的实测数据。

一句话：中文界面缩到 1400px 再 OCR 会崩（40 行 → 11 行），
但 OCR 不需要落盘那张图。拆开就没矛盾了，省 27 倍空间。

## 为什么用 JSONL 而不是 SQLite

初期阶段，理由是：
- 只追加写入，崩溃安全
- 零依赖，人可读
- 一天一个文件，删除某天很直接

**这是有意的技术债。** 数据超过几个月后需要加 SQLite 索引层，
但那是产品真正被用起来之后才需要解决的问题，现在引入只是过早优化。

## 为什么平台抽象用 RGBA 而不是原生句柄

代价：每次抓帧多一次内存拷贝（实测 1-2ms，占一轮的 3%）。

收益：OCR、缩略图、存储、聚合四套逻辑三个平台共用一份代码。

如果不抽象，每个平台都要重写一遍这些逻辑，而且测试也要重写。
3% 的性能换这个，很划算。

## 为什么采集不显示权限引导页（当前版本）

v0.1.0 依赖 macOS 系统自己弹的授权框（「Konseki may not be able to record...」）。
够用，但不友好。

正式分发前必须自己做一个引导页，因为：
- 用户点过「拒绝」后，系统不会再弹框
- 那种情况下只能引导去系统设置手动勾选

这件事在 `../../aside-lite/docs/shipping.md` 里有详细方案。
