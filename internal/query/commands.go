/*
 * [INPUT]: 依赖采集快照、会话和摘要的只读接口。
 * [OUTPUT]: 实现 status、days、day、snapshots 四条命令的输出。
 * [POS]: 查询命令的数据视图，所有文字字段在编码前单独遮蔽。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"konseki/internal/capture"
)

type statusOutput struct {
	DataDir    string `json:"dataDir"`
	CaptureDir string `json:"captureDir"`
	Recorder   struct {
		Enabled     bool   `json:"enabled"`
		PausedUntil string `json:"pausedUntil,omitempty"`
	} `json:"recorder"`
	Language  string `json:"language"`
	Redaction struct {
		Enabled  bool `json:"enabled"`
		Secrets  bool `json:"secrets"`
		Personal bool `json:"personal"`
	} `json:"redaction"`
	Days struct {
		Count int    `json:"count"`
		First string `json:"first"`
		Last  string `json:"last"`
	} `json:"days"`
	Today struct {
		Day       string `json:"day"`
		Snapshots int    `json:"snapshots"`
		Summaries int    `json:"summaries"`
	} `json:"today"`
}

func (r *runner) status(w io.Writer) error {
	days, err := r.dayList()
	if err != nil {
		return err
	}
	var out statusOutput
	out.DataDir = r.env.Dir
	out.CaptureDir = r.cfg.Recorder.Dir(r.env.Dir)
	out.Recorder.Enabled = r.cfg.Recorder.Enabled
	if until := r.cfg.Recorder.PausedUntil; until.After(r.now()) {
		out.Recorder.PausedUntil = stamp(until)
	}
	out.Language = r.cfg.Language
	out.Redaction.Enabled = r.cfg.Redact.Enabled
	out.Redaction.Secrets = r.cfg.Redact.Secrets
	out.Redaction.Personal = r.cfg.Redact.Personal
	out.Days.Count = len(days)
	if len(days) > 0 {
		out.Days.Last = days[0]
		out.Days.First = days[len(days)-1]
	}
	out.Today.Day = r.today()
	snaps, err := r.cap.ReadSnapshots(out.Today.Day)
	if err != nil {
		return err
	}
	entries, err := r.sums.Load(out.Today.Day)
	if err != nil {
		return err
	}
	out.Today.Snapshots = len(snaps)
	out.Today.Summaries = len(entries)
	return writeJSON(w, out)
}

type dayCount struct {
	Day       string `json:"day"`
	Snapshots int    `json:"snapshots"`
	Summaries int    `json:"summaries"`
	Sessions  int    `json:"sessions"`
}

func (r *runner) days(w io.Writer, o options) error {
	limit, err := intFlag(o, "limit", 30, 0)
	if err != nil {
		return err
	}
	all, err := r.dayList()
	if err != nil {
		return err
	}
	if len(all) > limit {
		all = all[:limit]
	}
	out := struct {
		Days []dayCount `json:"days"`
	}{Days: make([]dayCount, 0, len(all))}
	for _, day := range all {
		snaps, err := r.cap.ReadSnapshots(day)
		if err != nil {
			return err
		}
		entries, err := r.sums.Load(day)
		if err != nil {
			return err
		}
		sessions, err := r.cap.ReadSessions(day)
		if err != nil {
			return err
		}
		out.Days = append(out.Days, dayCount{
			Day: day, Snapshots: len(snaps), Summaries: len(entries), Sessions: len(sessions),
		})
	}
	return writeJSON(w, out)
}

type entryOutput struct {
	ID        string   `json:"id"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	Headline  string   `json:"headline"`
	Summary   string   `json:"summary"`
	Apps      []string `json:"apps"`
	Topics    []string `json:"topics"`
	Open      []string `json:"open"`
	Decisions []string `json:"decisions"`
	People    []string `json:"people"`
	Files     []string `json:"files"`
}

type blockOutput struct {
	Start   string   `json:"start"`
	End     string   `json:"end"`
	Minutes int      `json:"minutes"`
	App     string   `json:"app"`
	Titles  []string `json:"titles"`
	Samples int      `json:"samples"`
}

// sessionOutput 没有键鼠计数时略去 keys、clicks：输出 0 会被读成「没打字」。
type sessionOutput struct {
	ID        string   `json:"id"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	EndReason string   `json:"endReason"`
	Apps      []string `json:"apps"`
	Snapshots int      `json:"snapshots"`
	Keys      int      `json:"keys,omitempty"`
	Clicks    int      `json:"clicks,omitempty"`
}

func (r *runner) day(w io.Writer, o options) error {
	day, err := r.dateFlag(o)
	if err != nil {
		return err
	}
	snaps, err := r.cap.ReadSnapshots(day)
	if err != nil {
		return err
	}
	entries, err := r.sums.Load(day)
	if err != nil {
		return err
	}
	sessions, err := r.cap.ReadSessions(day)
	if err != nil {
		return err
	}

	out := struct {
		Day       string          `json:"day"`
		Summaries []entryOutput   `json:"summaries"`
		Blocks    []blockOutput   `json:"blocks"`
		Sessions  []sessionOutput `json:"sessions"`
	}{
		Day:       day,
		Summaries: make([]entryOutput, 0, len(entries)),
		Blocks:    make([]blockOutput, 0),
		Sessions:  make([]sessionOutput, 0, len(sessions)),
	}
	for _, e := range entries {
		out.Summaries = append(out.Summaries, entryOutput{
			ID:        e.ID,
			Start:     stamp(e.Start),
			End:       stamp(e.End),
			Headline:  r.maskText(e.Headline),
			Summary:   r.maskText(e.Summary),
			Apps:      r.maskList(e.Apps),
			Topics:    r.maskList(e.Topics),
			Open:      r.maskList(e.Open),
			Decisions: r.maskList(e.Decisions),
			People:    r.maskList(e.People),
			Files:     r.maskList(e.Files),
		})
	}
	for _, b := range capture.Aggregate(snaps) {
		out.Blocks = append(out.Blocks, blockOutput{
			Start:   stamp(b.Start),
			End:     stamp(b.End),
			Minutes: int(b.Duration().Minutes()),
			App:     r.maskText(b.App),
			Titles:  r.maskList(b.Titles),
			Samples: b.Samples,
		})
	}
	for _, s := range sessions {
		out.Sessions = append(out.Sessions, sessionOutput{
			ID:        s.ID,
			Start:     stamp(s.Start),
			End:       stamp(s.End),
			EndReason: string(s.EndReason),
			Apps:      r.maskList(s.Apps),
			Snapshots: s.Snapshots,
			Keys:      s.Keys,
			Clicks:    s.Clicks,
		})
	}
	return writeJSON(w, out)
}

func (r *runner) maskList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, r.maskText(item))
	}
	return out
}

type snapshotOutput struct {
	ID        string `json:"id"`
	TS        string `json:"ts"`
	App       string `json:"app"`
	Bundle    string `json:"bundle"`
	Title     string `json:"title"`
	Trigger   string `json:"trigger"`
	Dup       bool   `json:"dup,omitempty"`
	Keys      int    `json:"keys,omitempty"`
	Clicks    int    `json:"clicks,omitempty"`
	Shot      string `json:"shot,omitempty"`
	OCR       string `json:"ocr,omitempty"`
	OCRRepeat bool   `json:"ocrRepeat,omitempty"`
}

func (r *runner) snapshots(w io.Writer, o options) error {
	day, err := r.dateFlag(o)
	if err != nil {
		return err
	}
	start, end, err := dayBounds(day, o.values["from"], o.values["to"], o.set["from"], o.set["to"])
	if err != nil {
		return err
	}
	limit, err := intFlag(o, "limit", 30, 1000)
	if err != nil {
		return err
	}

	var snaps []capture.Snapshot
	if o.flags["ocr"] {
		snaps, err = r.cap.ReadDay(day)
	} else {
		snaps, err = r.cap.ReadSnapshots(day)
	}
	if err != nil {
		return err
	}

	appFilter := strings.ToLower(strings.TrimSpace(o.values["app"]))
	filtered := make([]capture.Snapshot, 0, len(snaps))
	for _, sn := range snaps {
		t, err := sn.Time()
		if err != nil {
			continue
		}
		if t.Before(start) || !t.Before(end) {
			continue
		}
		if appFilter != "" &&
			!strings.Contains(strings.ToLower(sn.App), appFilter) &&
			!strings.Contains(strings.ToLower(sn.Bundle), appFilter) {
			continue
		}
		filtered = append(filtered, sn)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, _ := filtered[i].Time()
		b, _ := filtered[j].Time()
		return a.Before(b)
	})

	total := len(filtered)
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := struct {
		Day       string           `json:"day"`
		From      string           `json:"from"`
		To        string           `json:"to"`
		Total     int              `json:"total"`
		Returned  int              `json:"returned"`
		Truncated bool             `json:"truncated"`
		Snapshots []snapshotOutput `json:"snapshots"`
	}{
		Day: day, From: o.values["from"], To: o.values["to"],
		Total: total, Returned: len(filtered), Truncated: total > len(filtered),
		Snapshots: make([]snapshotOutput, 0, len(filtered)),
	}
	prevOCR := ""
	havePrev := false
	for _, sn := range filtered {
		t, _ := sn.Time()
		item := snapshotOutput{
			ID:      sn.ID,
			TS:      stamp(t),
			App:     r.maskText(sn.App),
			Bundle:  r.maskText(sn.Bundle),
			Title:   r.maskText(sn.Title),
			Trigger: string(sn.Trigger),
			Dup:     sn.Dup,
			Keys:    sn.Keys,
			Clicks:  sn.Clicks,
		}
		if path := r.cap.ShotOf(sn); path != "" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				item.Shot = path
			}
		}
		if o.flags["ocr"] && sn.OCR != "" {
			if havePrev && sn.OCR == prevOCR {
				item.OCRRepeat = true
			} else {
				item.OCR = r.maskText(sn.OCR)
			}
			prevOCR, havePrev = sn.OCR, true
		}
		out.Snapshots = append(out.Snapshots, item)
	}
	return writeJSON(w, out)
}

// dayBounds 把日期和当天的 HH:MM 换成半开区间 [start, end)。
func dayBounds(day, from, to string, hasFrom, hasTo bool) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, end := d, d.AddDate(0, 0, 1)
	parse := func(name, value string) (time.Time, error) {
		hm, err := time.ParseInLocation("15:04", value, time.Local)
		if err != nil {
			return time.Time{}, usagef("invalid --%s %q", name, value)
		}
		return time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, time.Local), nil
	}
	if hasFrom {
		if start, err = parse("from", from); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if hasTo {
		if end, err = parse("to", to); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, usagef("--from must precede --to")
	}
	return start, end, nil
}
