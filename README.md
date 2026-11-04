# 痕跡 Konseki

本机优先的个人时间轴。记录你这一天在哪些 App 里做了什么，**数据全部留在本机，不上传任何东西。**

## 当前状态：v0.1.0 可运行

能跑，能真采集，界面能用。还是个骨架，缺摘要和理解层。

```
build/darwin-arm64/Konseki.app     9.2 MB
build/darwin-arm64/*.dmg           3.0 MB
```

## 跑起来

```sh
go tool mygo dev      # 开发（热重载）
go test               # 跑测试（界面测试不需要开窗口）
go tool mygo build    # 打包 App + dmg
```

## 项目结构

```
konseki/
├── main.go                    界面 + 生命周期（约 200 行）
├── internal/capture/          采集层，平台无关
│   ├── collector.go             接口定义：Screen / OCREngine / Frame
│   ├── runner.go                采样循环 + 时间轴聚合
│   ├── store.go                 JSONL 存储
│   ├── perms.go                 权限报告
│   ├── macos.go                 macOS: CGDisplayCreateImage + Vision OCR
│   ├── screen_darwin.go         macOS 平台实现
│   ├── screen_windows.go        Windows 骨架（TODO 标注了要调的 API）
│   └── screen_linux.go          Linux 骨架（TODO 标注了 portal/PipeWire）
├── docs/
│   ├── README.md               文档索引
│   ├── architecture.md         分层与数据流
│   ├── decisions.md            技术选型记录
│   └── mygo/                   mygo v0.7.4 文档（107 篇，离线可查）
└── mygo.json                   打包配置
```

## 设计要点

### 1. OCR 用全分辨率，磁盘只存缩略图

这是整个设计里唯一真正重要的决定。

```
CGDisplayCreateImage → 全分辨率 CGImage（内存，不落盘）
                       ├→ Vision OCR（全分辨率，识别准）
                       └→ 缩到 1200px → JPEG → 存盘（~42KB）
```

实测：中文界面缩到 1400px 再 OCR 就废了（40 行 → 11 行）。
但 OCR 不需要落盘那张图，两个目标就不冲突。

体积：42KB × 每天约 500 张 ≈ **20MB/天**，对比全存原图 547MB/天，省 27 倍。

### 2. 不是定时盲截，是「有信息量才截」

- 前台 App 或窗口标题变了 → 立刻截
- 没变 → 每 2 分钟补一张（保证长任务有时间轴）
- 闲置超过 5 分钟 → 不截
- 采到黑帧（锁屏/休眠）→ 跳过，不写记录

轮询 5 秒查一次窗口信息，**不截屏**，CPU 几乎为零。

### 3. 平台抽象

`Frame` 用统一的 RGBA 表示，各平台自己转。代价是一次内存拷贝（1-2ms），
换来 OCR / 缩略图 / 存储三套逻辑跨平台共用。

三平台都能编译：Windows 3.81 MB / Linux 3.80 MB / macOS 4.29 MB。

## 已知问题

- **首次授权要重启 App**。macOS 在「已授权但进程是授权前启动的」情况下，
  截出来的仍是全黑图。系统会弹框提示「Quit & Reopen」，按它做即可。
  这个问题在 `docs/` 里有详细记录。
- Windows / Linux 只有骨架，`Grab()` 返回 nil，需要按 TODO 填充。
- 还没有摘要层（Aside 那种 Memory summary），只有原始时间轴。
- ad-hoc 签名，分发前需要 Developer ID + 公证。

## 相关文档

- `docs/README.md` —— 文档索引，mygo 的 107 篇文档在 `docs/mygo/`
- `../aside-lite/README.md` —— 立项时的调研：Aside 内部结构、踩坑、全平台分析
