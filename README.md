# 痕跡 Konseki

本机优先的个人时间轴。记录你在电脑上做了什么，用你自己的模型把它压成人话，再让别的 Agent 读到它。

## 它解决什么

「我今天到底干了什么？」

这不是一个需要你手动打卡的工具。它截屏、OCR、聚合，然后让模型替你写下来。数据全在本机，只有摘要请求会出门 —— 而且出门前会脱敏。

## 三个组成部分

### 1. 采集

`internal/capture` 每 5 秒看一眼前台窗口，变了就抓一帧、OCR、落盘。z 磁盘上只存缩略图和文字，原图不保留。

三平台共用一套聚合逻辑，平台差异被收敛到 `Screen` / `OCREngine` 两个接口后面（macOS 用 purego 直调系统 API，Windows 用 BitBlt，Linux 用 PipeWire）。

### 2. 摘要

`internal/summary` 按整点切窗口，把窗口内的窗口标题 + OCR 交给模型，拿回结构化的结论：在做什么、进展到哪、什么没做完、做了什么决定。

模型由你自己配。`internal/providers` 的预设表和 [magpie](https://github.com/yetone/magpie) 对齐：**55 家**（Anthropic / OpenAI / Gemini / Vertex / DeepSeek / xAI / Kimi / Kimi Code / GLM / Z.ai / MiniMax / StepFun / 小米 MiMo / LongCat / 百度千帆 / 腾讯云 / 华为云 / 火山方舟 / Qwen / 百炼 / Mistral / Groq / Bedrock / Azure / OpenRouter / 硅基流动 / Ollama …），也可以接中转站或本地服务。

预设表是**从 magpie 的源码导出生成的**，不是手抄。这样 URL 和端点不会漂移 —— 手抄出来的表迟早会出现「测试连得上、真用 404」这种最难查的问题。

有区域/套餐档位的家（小米三个区域、火山方舟 Coding/Agent/按量、Bedrock 十个 region、Qwen Cloud 六个区域）会在面板里列成可选项，选了哪档就用哪档的地址和模型。

模型列表不用手动填：预设里没有的话，点「从接口拉取模型列表」真去问对方要。

也可以直接从本机已登录的 Claude Code / Codex 导入凭据，直接用你已有的订阅。

### 3. Skill

`internal/skill` 把痕迹导出成一个 Agent 能读的目录：

```
~/.claude/skills/konseki/
  SKILL.md              # 入口，Agent 读它决定要不要用
  KONSEKI.md            # 跨天画像：时间花在哪、反复出现的主题、悬着没做完的
  days/2026-11-04.md    # 一天的人话版
  days/2026-11-04.json  # 同一份数据的机器可读版
```

Claude Code、Codex、Cursor 都能读。装好之后，你不需要再向 Agent 解释「我昨天在干什么」。

## 脱敏

`internal/redact` 在把文本递给模型之前，把不该出门的东西换成占位符：

```
DB_PASSWORD=hunter2        →  DB_PASSWORD={{PASSWORD_k3v9x2mq}}
alice@example.com          →  {{EMAIL_7f2jd8sl}}
```

模型返回的文本再换回去，你看到的仍是原文。占位符对同一个值是稳定的（HMAC），所以模型看到的前后文是一致的，cache 不会失效。

能遮的：API key、私钥、密码、Bearer token、邮箱、手机号、身份证、银行卡，以及可选的 IP、家目录用户名、MAC。白名单和自定义词也在设置里。

**默认只遮四类核心个人数据 + 全部凭据。** IP 和家目录默认不遮 —— 开发者的时间轴里到处是 `127.0.0.1` 和 `/Users/xxx`，全遮掉会让摘要没法读。

## 隐私边界

- 原始截屏和 OCR 只存在 `~/Library/Application Support/Konseki/`
- 只有「摘要请求」会出门，出门前经过脱敏
- API key 单独存 `secrets.json`，权限 `0600`
- 有 denylist：某些 App、某些域名完全不采集
- 可以随时暂停采集

## 开发

```bash
go build ./...
go test ./...
go run .
```

`mygo` v0.7.4，native 模板（Go 自绘 UI，不用 WebView）。

```
main.go          界面与生命周期
timeline.go      时间轴渲染
ui_provider.go   供应商设置
ui_summary.go    摘要与 Skill 设置
internal/
  capture/       采集与聚合
  config/        设置存储
  llm/           模型调用（OpenAI 兼容 / Anthropic Messages）
  providers/     供应商目录（对齐 magpie）+ 本机凭据导入
  redact/        脱敏
  summary/       摘要生成
  skill/         导出给外部 Agent
```

## License

MIT
