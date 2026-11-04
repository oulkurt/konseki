# 架构

## 分层

```
┌─────────────────────────────────────┐
│ main.go                             │  界面 + 生命周期
│   app.view(c *ui.Context)           │  mygo 的 immediate-mode UI
└──────────────┬──────────────────────┘
               │ 只依赖 capture 的公开 API
┌──────────────▼──────────────────────┐
│ internal/capture                    │
│   Runner  ── 采样循环（goroutine）   │
│   Store   ── JSONL 读写             │
│   Block   ── 时间块聚合             │
└──────────────┬──────────────────────┘
               │ Screen / OCREngine 接口
    ┌──────────┼──────────┐
    ▼          ▼          ▼
 darwin    windows     linux
```

**关键约束：`main.go` 不认识任何平台 API。** 它只用 `capture.Runner`。
平台差异全部收敛在 `screen_*.go` 里的两个函数：`Grab()` 和 `Recognize()`。

## 数据流

```
每 5 秒轮询
  └→ FrontWindow()  取 App 名 + 窗口标题（很便宜，不截屏）
      └→ 判断值不值得截（标题变了？心跳到了？闲置了？）
          └→ Grab()  抓全分辨率帧到内存
              ├→ Blank?  → 跳过，不写记录
              ├→ Recognize()  全分辨率 OCR  → text
              └→ platformSaveThumb()  缩到 1200px → 存盘
                  └→ Append()  写一行 JSONL
```

## 存储格式

一天一个文件：`~/Library/Application Support/Konseki/2026-11-04.jsonl`

```json
{"ts":"2026-11-04T14:29:45+08:00",
 "app":"Konseki","title":"痕跡 Konseki","pid":45977,
 "ocr":"...识别出的文字...","ocrLines":18,
 "shot":"shots/20261104-142945.jpg","imgKB":42,"ocrMs":148}
```

选 JSONL 的理由：
- 只追加，崩溃最多丢最后一行
- 人可读，出问题能直接看
- 不需要数据库依赖

**但数据到几个月后会需要 SQLite 索引**（跨天查询、全文搜索）。
那时候的做法是：JSONL 继续写入，定期导入 SQLite 供查询。

## 时间块聚合

`Aggregate()` 是纯函数，不碰 IO，所以能完全离线测试。

规则：同一 App 且与上一块的间隔 < 10 分钟 → 并入同一块。

这样一次连续的工作不会因为中间心跳几次就被切成碎片。
标题变化会记录到 `Block.Titles`，但不会切块。

## 界面

mygo 是 immediate mode：`view(c)` 每帧重画。

- 状态放在 `app` struct 里（跨帧存活）
- 每 2 秒从磁盘重读一次（`refresh()` 里节流）
- 时间轴用 `ui.List` 虚拟化，几千行也不卡

**测试不开窗口**：`ui.NewTester(a.view, 640, 560)` 能直接构建视图并查询文本。
所以界面逻辑是纯查可测的，不用起 GUI。

## 生命周期

```go
mygo.App.WhenReady(func() {
    runner.Start()      // 装上就开始记录
    mygo.NewWindow(...)
})
mygo.App.Run()
```

采集在 `Start()` 时起一个 goroutine，`Stop()` 时关 channel 并等它退出。
重复调用 `Start()` 无副作用。
