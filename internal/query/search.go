/*
 * [INPUT]: 依赖摘要和采集快照的只读接口、脱敏器。
 * [OUTPUT]: 实现 search 命令：在摘要字段、窗口标题和屏幕文字里查词。
 * [POS]: 查询命令的全文搜索。先找原文候选，再遮蔽整段字段并重新定位。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// hit 是一条命中的内部形状，summary 和 snapshot 共用排序与合并逻辑。
type hit struct {
	source   string
	day      string
	id       string
	app      string
	title    string
	field    string
	snippet  string
	shot     string
	headline string
	start    time.Time
	end      time.Time
	ts       time.Time
	count    int
}

func (h *hit) when() time.Time {
	if h.source == "snapshot" {
		return h.ts
	}
	return h.end
}

func (r *runner) search(w io.Writer, o options) error {
	if len(o.pos) < 2 || strings.TrimSpace(strings.Join(o.pos[1:], " ")) == "" {
		return usagef("missing search text")
	}
	query := strings.TrimSpace(strings.Join(o.pos[1:], " "))
	lower := strings.ToLower(query)
	days, err := intFlag(o, "days", 7, 365)
	if err != nil {
		return err
	}
	limit, err := intFlag(o, "limit", 30, 0)
	if err != nil {
		return err
	}

	now := r.now()
	from := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	to := now.Format("2006-01-02")

	hits := make([]*hit, 0)
	for _, day := range r.sums.Days() {
		if day < from || day > to {
			continue
		}
		entries, err := r.sums.Load(day)
		if err != nil {
			return err
		}
		for _, e := range entries {
			for _, field := range []struct{ name, text string }{
				{"headline", e.Headline},
				{"summary", e.Summary},
				{"topics", strings.Join(e.Topics, "\n")},
				{"open", strings.Join(e.Open, "\n")},
				{"decisions", strings.Join(e.Decisions, "\n")},
				{"files", strings.Join(e.Files, "\n")},
				{"people", strings.Join(e.People, "\n")},
				{"apps", strings.Join(e.Apps, "\n")},
			} {
				snippet, ok := r.matchField(field.text, lower)
				if !ok {
					continue
				}
				hits = append(hits, &hit{
					source: "summary", day: day, id: e.ID,
					field: field.name, snippet: snippet,
					headline: r.maskText(e.Headline),
					start:    e.Start, end: e.End, count: 1,
				})
				break
			}
		}
	}

	captureDays, err := r.cap.Days()
	if err != nil {
		return err
	}
	for _, day := range captureDays {
		if day < from || day > to {
			continue
		}
		snaps, err := r.cap.ReadDay(day)
		if err != nil {
			return err
		}
		for _, sn := range snaps {
			ts, err := sn.Time()
			if err != nil {
				continue
			}
			field, snippet := "", ""
			if s, ok := r.matchField(sn.Title, lower); ok {
				field, snippet = "title", s
			} else if s, ok := r.matchField(sn.OCR, lower); ok {
				field, snippet = "ocr", s
			} else {
				continue
			}
			h := &hit{
				source: "snapshot", day: day, id: sn.ID,
				app: r.maskText(sn.App), title: r.maskText(sn.Title),
				field: field, snippet: snippet, ts: ts, count: 1,
			}
			if path := r.cap.ShotOf(sn); path != "" {
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					h.shot = path
				}
			}
			hits = append(hits, h)
		}
	}

	// 先按时间新到旧排，再合并同一条命中。排序后第一次出现的即最新的一条，
	// 后来的只加计数，id、时间与截图保留最新的那份。
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].when().After(hits[j].when()) })
	type mergeKey struct {
		source, day, app, title, field, snippet string
	}
	merged := make([]*hit, 0, len(hits))
	index := map[mergeKey]int{}
	for _, h := range hits {
		key := mergeKey{h.source, h.day, h.app, h.title, h.field, h.snippet}
		if i, ok := index[key]; ok {
			merged[i].count++
			continue
		}
		index[key] = len(merged)
		merged = append(merged, h)
	}
	total := len(merged)
	if len(merged) > limit {
		merged = merged[:limit]
	}

	matches := make([]any, 0, len(merged))
	for _, h := range merged {
		if h.source == "summary" {
			matches = append(matches, struct {
				Source   string `json:"source"`
				Day      string `json:"day"`
				ID       string `json:"id"`
				Start    string `json:"start"`
				End      string `json:"end"`
				Headline string `json:"headline"`
				Field    string `json:"field"`
				Snippet  string `json:"snippet"`
				Count    int    `json:"count"`
			}{"summary", h.day, h.id, stamp(h.start), stamp(h.end), h.headline, h.field, h.snippet, h.count})
			continue
		}
		matches = append(matches, struct {
			Source  string `json:"source"`
			Day     string `json:"day"`
			ID      string `json:"id"`
			TS      string `json:"ts"`
			App     string `json:"app"`
			Title   string `json:"title"`
			Field   string `json:"field"`
			Snippet string `json:"snippet"`
			Shot    string `json:"shot,omitempty"`
			Count   int    `json:"count"`
		}{"snapshot", h.day, h.id, stamp(h.ts), h.app, h.title, h.field, h.snippet, h.shot, h.count})
	}

	out := struct {
		Query     string `json:"query"`
		From      string `json:"from"`
		To        string `json:"to"`
		Total     int    `json:"total"`
		Returned  int    `json:"returned"`
		Truncated bool   `json:"truncated"`
		Matches   []any  `json:"matches"`
	}{
		Query: query, From: from, To: to,
		Total: total, Returned: len(matches), Truncated: total > len(matches),
		Matches: matches,
	}
	return writeJSON(w, out)
}

// matchField 在原文里找候选，遮蔽整段字段，再在遮蔽后的文字里重新定位。
// 命中落在被遮的值里时返回 false。
func (r *runner) matchField(text, lower string) (string, bool) {
	if text == "" {
		return "", false
	}
	if !strings.Contains(strings.ToLower(text), lower) {
		return "", false
	}
	return snippet(r.maskText(text), lower)
}

// snippet 截取命中处前后各 60 个 rune，被截断的一侧加省略号。
// 连续空白和换行合并成一个空格。
func snippet(text, lower string) (string, bool) {
	hay := strings.ToLower(text)
	at := strings.Index(hay, lower)
	if at < 0 {
		return "", false
	}
	runes := []rune(text)
	start := utf8.RuneCountInString(text[:at])
	end := start + utf8.RuneCountInString(lower)
	lo, hi := start-60, end+60
	if lo < 0 {
		lo = 0
	}
	if hi > len(runes) {
		hi = len(runes)
	}
	if lo > len(runes) {
		lo = len(runes)
	}
	out := string(runes[lo:hi])
	if lo > 0 {
		out = "…" + out
	}
	if hi < len(runes) {
		out += "…"
	}
	return strings.Join(strings.Fields(out), " "), true
}
