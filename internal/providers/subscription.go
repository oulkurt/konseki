package providers

// 订阅额度。
//
// 现实：绝大多数供应商不提供公开的余额/额度 API。Claude 的用量只在
// 客户端本地日志里，OpenAI 的要 dashboard，Kimi/GLM 只在控制台。
// 所以这里做两件事：
//
//  1. 能算的本地算 —— 读 Claude Code 自己的 usage 记录
//  2. 算不了的如实说 —— 给出窗口长度和「去控制台看」的链接，
//     而不是编一个假数字
//
// 宁可显示「未知」，也不要显示一个错的百分比。用户会依赖它做决定。

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Quota 是一个订阅额度的估算。
type Quota struct {
	// PresetID 是 providers 里的 id。
	PresetID string
	// Window 是限额窗口长度。0 表示这家没有明确的窗口制。
	Window time.Duration
	// Used / Limit 是窗口内已用和上限。Limit 为 0 表示拿不到上限。
	Used  int
	Limit int
	// ResetAt 是窗口重置的时刻，零值表示未知。
	ResetAt time.Time
	// Confidence 说这个数字有多可信：
	//   "measured" —— 本地日志里数出来的
	//   "declared" —— 只有窗口长度，没用量
	//   "unknown"  —— 只能给个链接
	Confidence string
	// Notes 是给用户看的一句话。
	Notes string
	// ConsoleURL 是去看真实额度的地方。
	ConsoleURL string
}

// LocalUsage 从本机已登录的 CLI 里数出今天的用量。
//
// 目前只支持 Claude Code：它的 ~/.claude/projects/**/*.jsonl 每条记录
// 都带 message.usage，是唯一一个能在本地精确统计的。
func LocalUsage(presetID string) (Quota, bool) {
	switch presetID {
	case "anthropic", "claude-code":
		return claudeUsage()
	}
	return Quota{}, false
}

func claudeUsage() (Quota, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Quota{}, false
	}
	root := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(root); err != nil {
		return Quota{}, false
	}

	// Claude 的窗口是 5 小时滚动。要算「当前窗口内用了多少」，
	// 得找到最近一条记录，再从它往前推 5 小时。
	type rec struct {
		ts    time.Time
		input int
		out   int
	}
	var recs []rec

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		// 只看最近改过的文件。一个装了很久的机器上可能有几百个
		// session 文件，全读一遍要好几秒，而当前窗口的数据
		// 一定在最近修改的文件里。
		if time.Since(info.ModTime()) > 24*time.Hour {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			var line struct {
				Timestamp string `json:"timestamp"`
				Message   struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal(sc.Bytes(), &line) != nil || line.Timestamp == "" {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, line.Timestamp)
			if err != nil {
				if ts, err = time.Parse(time.RFC3339, line.Timestamp); err != nil {
					continue
				}
			}
			if line.Message.Usage.InputTokens == 0 && line.Message.Usage.OutputTokens == 0 {
				continue
			}
			recs = append(recs, rec{ts, line.Message.Usage.InputTokens, line.Message.Usage.OutputTokens})
		}
		return nil
	})

	if len(recs) == 0 {
		return Quota{}, false
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ts.Before(recs[j].ts) })

	// 5 小时窗口：以最后一条记录为终点往前推。不用「当前时刻」
	// 作终点，因为用户可能已经离开电脑几小时了，那时真实窗口
	// 早就重置，报一个空的窗口没有意义。
	last := recs[len(recs)-1].ts
	start := last.Add(-5 * time.Hour)
	used := 0
	for _, r := range recs {
		if r.ts.After(start) || r.ts.Equal(start) {
			used += r.input + r.out
		}
	}
	return Quota{
		PresetID:   "claude-code",
		Window:     5 * time.Hour,
		Used:       used,
		ResetAt:    last.Add(5 * time.Hour),
		Confidence: "measured",
		Notes:      "从本机 Claude Code 的会话记录里数出来的 token 数。上限不公开，只能看趋势。",
		ConsoleURL: "https://claude.ai/settings/usage",
	}, true
}
