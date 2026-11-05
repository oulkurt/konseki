/*
 * [INPUT]: 依赖采集时间块与界面文本组件。
 * [OUTPUT]: 提供时间轴数据聚合与富文本渲染。
 * [POS]: 为时间轴视图转换采集数据。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"strings"
	"time"

	"konseki/internal/capture"

	"github.com/egoist/mygo/ui"
)

// MemoryGroup represents a top-level bullet category under "Memory summary"
// with nested detail bullets.
type MemoryGroup struct {
	Category string
	Items    []string
}

// RecordingSegment represents a chronological micro-slice under "Recording summary".
type RecordingSegment struct {
	TimeRange string
	AppsLabel string
	Detail    string
}

// TimelineSlot represents one time-window row in the History timeline
// (e.g. "10:00 AM", "9:40 AM", "9:30 AM").
type TimelineSlot struct {
	ID           string
	TimeLabel    string
	Title        string
	Summary      string
	Apps         []string
	Memory       []MemoryGroup
	PriorContext string
	UserContext  []string
	Recordings   []RecordingSegment
}

// buildSlotsFromBlocks converts live capture.Block records into TimelineSlot items
// (newest first, matching the History timeline order).
func buildSlotsFromBlocks(blocks []capture.Block) []TimelineSlot {
	if len(blocks) == 0 {
		return nil
	}

	// Group blocks that fall within the same 10-minute window, or keep distinct blocks
	// when they are in different windows.
	type bucket struct {
		slotTime time.Time
		blocks   []capture.Block
	}
	var buckets []bucket
	for _, b := range blocks {
		slotMin := (b.End.Minute() / 10) * 10
		slotT := time.Date(b.End.Year(), b.End.Month(), b.End.Day(), b.End.Hour(), slotMin, 0, 0, b.End.Location())
		if len(buckets) > 0 && buckets[len(buckets)-1].slotTime.Equal(slotT) && len(buckets[len(buckets)-1].blocks) < 4 {
			buckets[len(buckets)-1].blocks = append(buckets[len(buckets)-1].blocks, b)
		} else {
			buckets = append(buckets, bucket{
				slotTime: b.Start,
				blocks:   []capture.Block{b},
			})
		}
	}

	out := make([]TimelineSlot, 0, len(buckets))
	// Iterate in reverse so newest slots appear at the top of the timeline.
	for i := len(buckets) - 1; i >= 0; i-- {
		bkt := buckets[i]
		first := bkt.blocks[0]
		last := bkt.blocks[len(bkt.blocks)-1]

		var apps []string
		seenApp := map[string]bool{}
		var allTitles []string
		var timeTokens []string
		var memGroups []MemoryGroup
		var recordings []RecordingSegment
		var contextTokens []string

		for _, blk := range bkt.blocks {
			if blk.App != "" && !seenApp[blk.App] {
				seenApp[blk.App] = true
				apps = append(apps, blk.App)
			}
			tStr := blk.Start.Format("15:04")
			timeTokens = append(timeTokens, tStr)

			var cleanTitles []string
			for _, t := range blk.Titles {
				t = strings.TrimSpace(t)
				if t != "" {
					cleanTitles = append(cleanTitles, t)
					allTitles = append(allTitles, t)
				}
			}

			var items []string
			if len(cleanTitles) > 0 {
				for _, ct := range cleanTitles {
					items = append(items, fmt.Sprintf("Focused on `%s` in %s (%d samples, %s).", ct, blk.App, max(1, blk.Samples), fmtDur(blk.Duration())))
				}
				contextTokens = append(contextTokens, cleanTitles[0])
			} else {
				items = append(items, fmt.Sprintf("Active session in `%s` (%d samples captured).", blk.App, max(1, blk.Samples)))
			}
			memGroups = append(memGroups, MemoryGroup{
				Category: fmt.Sprintf("%s (%s):", blk.App, tStr),
				Items:    items,
			})

			endStr := blk.End.Format("15:04")
			detail := fmt.Sprintf("Worked in %s", blk.App)
			if len(cleanTitles) > 0 {
				detail = fmt.Sprintf("Worked in %s viewing `%s` (%d snapshots recorded).", blk.App, strings.Join(cleanTitles, "`, `"), max(1, blk.Samples))
			}
			recordings = append(recordings, RecordingSegment{
				TimeRange: fmt.Sprintf("%s–%s", tStr, endStr),
				AppsLabel: blk.App,
				Detail:    detail,
			})
		}

		headlineApp := strings.Join(apps, ", ")
		headline := fmt.Sprintf("Worked in %s", headlineApp)
		if len(allTitles) > 0 {
			headline = fmt.Sprintf("%s — %s", headlineApp, allTitles[0])
		}

		summary := fmt.Sprintf("Recorded activity at %s across %s.", strings.Join(timeTokens, ", "), headlineApp)
		if len(allTitles) > 0 {
			summary = fmt.Sprintf("Active at %s in %s: %s.", strings.Join(timeTokens, ", "), headlineApp, strings.Join(allTitles, " · "))
		}

		prior := fmt.Sprintf("Captured by Konseki local timeline engine between `%s` and `%s`.", first.Start.Format("15:04"), last.End.Format("15:04"))
		if i > 0 {
			prevApp := buckets[i-1].blocks[0].App
			prior = fmt.Sprintf("Continues from earlier session in `%s` at `%s`.", prevApp, buckets[i-1].slotTime.Format("15:04"))
		}

		if len(contextTokens) == 0 {
			contextTokens = append(contextTokens, apps...)
		}

		out = append(out, TimelineSlot{
			ID:           fmt.Sprintf("live-%d-%s", i, first.Start.Format("150405")),
			TimeLabel:    last.End.Format("3:04 PM"),
			Title:        headline,
			Summary:      summary,
			Apps:         apps,
			Memory:       memGroups,
			PriorContext: prior,
			UserContext:  contextTokens,
			Recordings:   recordings,
		})
	}
	return out
}

func renderRichTextWithCodePills(c *ui.Context, text string, fontSize float32, textColor, codeFg, codeBg ui.Color) {
	if !strings.Contains(text, "`") {
		ui.Text(c, text).FontSize(fontSize).TextColor(textColor).LineHeight(1.48)
		return
	}
	parts := strings.Split(text, "`")
	spans := make([]ui.Span, 0, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i%2 == 1 {
			spans = append(spans, ui.Span{
				Text:       " " + part + " ",
				Font:       "monospace",
				Size:       fontSize - 1.0,
				Color:      codeFg,
				Background: codeBg,
				Radius:     4,
			})
		} else {
			spans = append(spans, ui.Span{
				Text:  part,
				Size:  fontSize,
				Color: textColor,
			})
		}
	}
	ui.RichText(c, spans...).FontSize(fontSize).TextColor(textColor).LineHeight(1.55)
}
