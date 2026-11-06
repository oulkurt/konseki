/*
 * [INPUT]: 依赖采集时间块、摘要和导出目录。
 * [OUTPUT]: 提供 DefaultDir 与 Export，写出入口、画像和日记。
 * [POS]: 将活动证据转换为外部 Agent 可读的文件，不修改采集数据。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Package skill 把 Konseki 的痕迹导出成别的 Agent 能读的东西。
//
// 想让 Claude Code / Codex / Cursor 这些工具了解「我在干什么」，
// 光给一份大 JSON 没用 —— 它们不会去读。真正能生效的是 skill：
// 一个带 frontmatter 的 SKILL.md，加上按天分文件的数据。
//
// 目录结构：
//
//	<exportDir>/
//	  SKILL.md              # 入口，告诉 Agent 有什么、怎么查
//	  KONSEKI.md            # 精简的长期画像（最近 N 天的主题、习惯）
//	  days/2026-11-04.md    # 一天的人话版
//	  days/2026-11-04.json  # 同样内容的机器可读版
//
// 为什么同时给 .md 和 .json：模型读 md 更省 token 也更准；
// 但脚本化的 Agent（比如自己写 grep 的）需要结构化数据。
package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"konseki/internal/capture"
	"konseki/internal/summary"
)

// Options 控制导出内容。
type Options struct {
	// Dir 是输出目录。空则用 DefaultDir()。
	Dir string
	// Days 是导出最近多少天。0 表示全部。
	Days int
	// IncludeOCR 决定要不要带原始 OCR 采样。默认关：
	// 那是最脏、最容易带出隐私的数据，摘要已经提炼过了。
	IncludeOCR bool
	// Owner 是用户的名字，写进 SKILL.md 让 Agent 知道在对谁说话。
	Owner string
}

// DefaultDir 返回默认导出位置。
//
// 挑 ~/.claude/skills 而不是 App 自己的数据目录，因为 skill 的消费者
// 是别的 CLI 工具，它们只会在自己的约定目录里找。用户在设置里可以把
// 它改到 ~/.codex/skills 或任何地方。
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "konseki-skill"
	}
	return filepath.Join(home, ".claude", "skills", "konseki")
}

// DayData 是导出某一天需要的全部原料。
type DayData struct {
	Day     string
	Blocks  []capture.Block
	Entries []summary.Entry
	// OCR 只在 IncludeOCR 时被读进来。
	OCR map[string]string
}

// Export 写出整个 skill 目录。返回写出的文件数。
func Export(opts Options, days []DayData) (int, error) {
	dir := opts.Dir
	if dir == "" {
		dir = DefaultDir()
	}
	if err := os.MkdirAll(filepath.Join(dir, "days"), 0o755); err != nil {
		return 0, err
	}

	sort.Slice(days, func(i, j int) bool { return days[i].Day > days[j].Day })
	if opts.Days > 0 && len(days) > opts.Days {
		days = days[:opts.Days]
	}

	n := 0
	if err := writeFile(filepath.Join(dir, "SKILL.md"), skillMD(opts, days)); err != nil {
		return n, err
	}
	n++
	if err := writeFile(filepath.Join(dir, "KONSEKI.md"), profileMD(opts, days)); err != nil {
		return n, err
	}
	n++

	for _, d := range days {
		if err := writeFile(filepath.Join(dir, "days", d.Day+".md"), dayMD(d)); err != nil {
			return n, err
		}
		n++
		b, err := json.MarshalIndent(dayJSON(d, opts.IncludeOCR), "", "  ")
		if err != nil {
			return n, err
		}
		if err := writeFile(filepath.Join(dir, "days", d.Day+".json"), b); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func writeFile(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// skillMD 是入口文件。
//
// frontmatter 里的 name/description 是 skill 规范要求的字段，
// description 尤其重要 —— 它是 Agent 决定「要不要用这个 skill」的
// 唯一线索，所以写清楚「什么时候该读」而不是「这里面有什么」。
func skillMD(opts Options, days []DayData) []byte {
	owner := opts.Owner
	if owner == "" {
		owner = "the user"
	}
	first, last := "", ""
	if len(days) > 0 {
		last = days[len(days)-1].Day
		first = days[0].Day
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: konseki\n")
	sb.WriteString("description: " + quoteYAML(fmt.Sprintf(
		"What %s actually did on their computer, reconstructed from local screen captures. Use this when you need context beyond the current conversation: what they worked on earlier today or this week, which files and apps they keep returning to, what is left unfinished, and what was decided. Read it before asking \"what are you working on?\" — the answer is probably already here.",
		owner)) + "\n")
	sb.WriteString("---\n\n")
	fmt.Fprintf(&sb, "# 痕跡 Konseki — %s 的本机工作痕迹\n\n", owner)
	fmt.Fprintf(&sb, "数据范围：%s ~ %s，共 %d 天。\n\n", last, first, len(days))
	sb.WriteString(`## 这套数据是什么

不是笔记，也不是你自己写的总结。是 Konseki 在这台机器上客观记录下来的：
前台窗口、窗口标题、屏幕文字（OCR），然后由模型压缩成「这段时间在做什么」。

所以它能回答一些聊天记录答不出的问题：

- 「我今天一直在纠结什么」—— 看反复回到同一个文件/同一个搜索的次数
- 「上次那个 bug 改了哪里」—— 找相关的文件路径和窗口标题
- 「我上周答应要做什么但没做」—— 看每天 ` + "`未完成`" + ` 那一栏
- 「现在这个对话和之前的工作有关系吗」—— 先读今天的文件

## 怎么读

| 想知道 | 读这个 |
| --- | --- |
| 今天的完整脉络 | ` + "`days/<今天>.md`" + ` |
| 最近都在做什么（跨天） | ` + "`KONSEKI.md`" + ` |
| 精确到时间点 | ` + "`days/<日期>.json`" + ` 的 ` + "`entries[].start/end`" + ` |
| 找某个项目/文件在哪天出现 | ` + "`grep -ri \"关键词\" days/`" + ` |

` + "`days/*.md`" + ` 里每天分两栏：

- **摘要**：按小时分段的结论，每段有标题、正文、未完成项、决定
- **原始时间轴**：哪个 App、什么窗口标题、持续多久

摘要比原始时间轴可信得多。时间轴只能说明「打开了 VS Code」，
摘要是「在改 capture/runner.go 的采样间隔」。

## 边界（重要）

- 这里有**脱敏**：API key、密码、邮箱、手机号、身份证号都被换成了 ` + "`{{KIND_xxxxxxxx}}`" + ` 这样的占位符。占位符是稳定的，同一个值每次一样，但你看不到原文。
- 屏幕文字是 **OCR** 出来的，会认错字。专有名词、代码标识符尤其容易错。如果一个名字看起来怪，去 ` + "`days/*.json`" + ` 里对照上下文，或者直接问用户。
- 用户可能**暂停过采集**。时间轴上的空白不等于那段时间在休息。
- 不要把这里的内容当成用户的原话引用。要引用就说是「Konseki 记录的」。

## 更新

Konseki 每次生成新摘要后会重写这个目录。
`)
	return []byte(sb.String())
}

// profileMD 是跨天的画像。
//
// 只做统计，不做推断。推断（「他最近压力大」）留给读它的 Agent ——
// 我们给的是「哪些 App 出现最多」「哪些主题反复出现」这种可验证的东西。
func profileMD(opts Options, days []DayData) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Konseki 工作画像\n\n")
	fmt.Fprintf(&sb, "统计区间：最近 %d 天。\n\n", len(days))

	appDur := map[string]time.Duration{}
	appCount := map[string]int{}
	topicCount := map[string]int{}
	openCount := map[string]int{}
	decisions := map[string]bool{}
	total := time.Duration(0)

	for _, d := range days {
		for _, b := range d.Blocks {
			appDur[b.App] += b.Duration()
			appCount[b.App]++
			total += b.Duration()
		}
		for _, e := range d.Entries {
			for _, t := range e.Topics {
				topicCount[t]++
			}
			for _, o := range e.Open {
				openCount[o]++
			}
			for _, dec := range e.Decisions {
				decisions[dec] = true
			}
		}
	}

	sb.WriteString("## 时间花在哪\n\n")
	sb.WriteString("| App | 累计时长 | 出现次数 |\n| --- | --- | --- |\n")
	for _, kv := range topN(appDur, 12) {
		fmt.Fprintf(&sb, "| %s | %s | %d |\n", kv.key, fmtDur(kv.dur), appCount[kv.key])
	}
	if total > 0 {
		fmt.Fprintf(&sb, "\n合计有记录的时间：%s\n", fmtDur(total))
	}

	if len(topicCount) > 0 {
		sb.WriteString("\n## 反复出现的主题\n\n")
		type ic struct {
			k string
			n int
		}
		var tcs []ic
		for k, n := range topicCount {
			tcs = append(tcs, ic{k, n})
		}
		sort.Slice(tcs, func(i, j int) bool { return tcs[i].n > tcs[j].n })
		for i, t := range tcs {
			if i >= 15 {
				break
			}
			fmt.Fprintf(&sb, "- %s（%d 次）\n", t.k, t.n)
		}
	}

	if len(openCount) > 0 {
		sb.WriteString("\n## 悬着没做完的\n\n")
		type ic struct {
			k string
			n int
		}
		var ocs []ic
		for k, n := range openCount {
			ocs = append(ocs, ic{k, n})
		}
		sort.Slice(ocs, func(i, j int) bool { return ocs[i].n > ocs[j].n })
		for i, o := range ocs {
			if i >= 15 {
				break
			}
			if o.n > 1 {
				fmt.Fprintf(&sb, "- %s（连续 %d 天提到）\n", o.k, o.n)
			} else {
				fmt.Fprintf(&sb, "- %s\n", o.k)
			}
		}
	}

	if len(decisions) > 0 {
		sb.WriteString("\n## 做过的决定\n\n")
		var ds []string
		for d := range decisions {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		// 决定会越来越多，只留最新的 20 条。
		if len(ds) > 20 {
			ds = ds[len(ds)-20:]
		}
		for _, d := range ds {
			fmt.Fprintf(&sb, "- %s\n", d)
		}
	}
	return []byte(sb.String())
}

const (
	daySummaryHeading  = "## 摘要\n\n"
	dayTimelineHeading = "## 原始时间轴\n\n"
	dayEmpty           = "这一天没有记录。\n"
)

// dayMD 是一天的人话版。
func dayMD(d DayData) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", d.Day)

	if len(d.Entries) == 0 && len(d.Blocks) == 0 {
		sb.WriteString(dayEmpty)
		return []byte(sb.String())
	}

	if len(d.Entries) > 0 {
		sb.WriteString(daySummaryHeading)
		for _, e := range d.Entries {
			fmt.Fprintf(&sb, "### %s–%s · %s\n\n", e.Start.Format("15:04"), e.End.Format("15:04"), e.Headline)
			if e.Summary != "" {
				sb.WriteString(e.Summary)
				sb.WriteString("\n\n")
			}
			if len(e.Apps) > 0 {
				fmt.Fprintf(&sb, "- **App**：%s\n", strings.Join(e.Apps, ", "))
			}
			if len(e.Topics) > 0 {
				fmt.Fprintf(&sb, "- **主题**：%s\n", strings.Join(e.Topics, "、"))
			}
			if len(e.Open) > 0 {
				fmt.Fprintf(&sb, "- **未完成**：%s\n", strings.Join(e.Open, "；"))
			}
			if len(e.Decisions) > 0 {
				fmt.Fprintf(&sb, "- **决定**：%s\n", strings.Join(e.Decisions, "；"))
			}
			if len(e.People) > 0 {
				fmt.Fprintf(&sb, "- **人**：%s\n", strings.Join(e.People, ", "))
			}
			if len(e.Files) > 0 {
				fmt.Fprintf(&sb, "- **文件**：%s\n", strings.Join(e.Files, ", "))
			}
			sb.WriteString("\n")
		}
	}

	if len(d.Blocks) > 0 {
		sb.WriteString(dayTimelineHeading)
		sb.WriteString("| 时间 | App | 窗口标题 | 时长 |\n| --- | --- | --- | --- |\n")
		for _, b := range d.Blocks {
			title := ""
			if len(b.Titles) > 0 {
				title = b.Titles[0]
			}
			fmt.Fprintf(&sb, "| %s–%s | %s | %s | %s |\n",
				b.Start.Format("15:04"), b.End.Format("15:04"),
				esc(b.App), esc(title), fmtDur(b.Duration()))
		}
	}
	return []byte(sb.String())
}

// dayJSON 是机器可读版。
func dayJSON(d DayData, includeOCR bool) map[string]any {
	type block struct {
		Start   string   `json:"start"`
		End     string   `json:"end"`
		App     string   `json:"app"`
		Titles  []string `json:"titles,omitempty"`
		Samples int      `json:"samples"`
		Minutes int      `json:"minutes"`
		OCR     string   `json:"ocr,omitempty"`
	}
	blocks := make([]block, 0, len(d.Blocks))
	for _, b := range d.Blocks {
		bb := block{
			Start: b.Start.Format(time.RFC3339), End: b.End.Format(time.RFC3339),
			App: b.App, Titles: b.Titles, Samples: b.Samples,
			Minutes: int(b.Duration().Minutes()),
		}
		if includeOCR && len(d.OCR) > 0 && len(b.Titles) > 0 {
			bb.OCR = d.OCR[b.Titles[0]]
		}
		blocks = append(blocks, bb)
	}
	return map[string]any{
		"day":     d.Day,
		"entries": d.Entries,
		"blocks":  blocks,
	}
}

// ---------- 小工具 ----------

type durKey struct {
	key string
	dur time.Duration
}

func topN(m map[string]time.Duration, n int) []durKey {
	out := make([]durKey, 0, len(m))
	for k, v := range m {
		out = append(out, durKey{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dur > out[j].dur })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func fmtDur(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// esc 让 markdown 表格不被标题里的竖线撑破。
func esc(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func quoteYAML(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
