---
name: konseki
description: "What tester actually did on their computer, reconstructed from local screen captures. Use this when you need context beyond the current conversation: what they worked on earlier today or this week, which files and apps they keep returning to, what is left unfinished, and what was decided. Read it before asking \"what are you working on?\" — the answer is probably already here."
---

# 痕跡 Konseki — tester 的本机工作痕迹

数据范围：2026-11-04 ~ 2026-11-04，共 1 天。

## 这套数据是什么

不是笔记，也不是你自己写的总结。是 Konseki 在这台机器上客观记录下来的：
前台窗口、窗口标题、屏幕文字（OCR），然后由模型压缩成「这段时间在做什么」。

所以它能回答一些聊天记录答不出的问题：

- 「我今天一直在纠结什么」—— 看反复回到同一个文件/同一个搜索的次数
- 「上次那个 bug 改了哪里」—— 找相关的文件路径和窗口标题
- 「我上周答应要做什么但没做」—— 看每天 `未完成` 那一栏
- 「现在这个对话和之前的工作有关系吗」—— 先读今天的文件

## 怎么读

| 想知道 | 读这个 |
| --- | --- |
| 今天的完整脉络 | `days/<今天>.md` |
| 最近都在做什么（跨天） | `KONSEKI.md` |
| 精确到时间点 | `days/<日期>.json` 的 `entries[].start/end` |
| 找某个项目/文件在哪天出现 | `grep -ri "关键词" days/` |

`days/*.md` 里每天分两栏：

- **摘要**：按小时分段的结论，每段有标题、正文、未完成项、决定
- **原始时间轴**：哪个 App、什么窗口标题、持续多久

摘要比原始时间轴可信得多。时间轴只能说明「打开了 VS Code」，
摘要是「在改 capture/runner.go 的采样间隔」。

## 边界（重要）

- 这里有**脱敏**：API key、密码、邮箱、手机号、身份证号都被换成了 `{{KIND_xxxxxxxx}}` 这样的占位符。占位符是稳定的，同一个值每次一样，但你看不到原文。
- 屏幕文字是 **OCR** 出来的，会认错字。专有名词、代码标识符尤其容易错。如果一个名字看起来怪，去 `days/*.json` 里对照上下文，或者直接问用户。
- 用户可能**暂停过采集**。时间轴上的空白不等于那段时间在休息。
- 不要把这里的内容当成用户的原话引用。要引用就说是「Konseki 记录的」。

## 更新

Konseki 每次生成新摘要后会重写这个目录。
