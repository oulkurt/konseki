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

// showcaseSlots returns the curated timeline entries modeled after the design spec.
func showcaseSlots() []TimelineSlot {
	return []TimelineSlot{
		{
			ID:        "slot-1440",
			TimeLabel: "2:40 PM",
			Title:     "Verified Konseki screen permissions and drafted MyGo native UI prompt in Alma",
			Summary:   "You tested macOS screen capture permissions for Konseki, inspected the initial native timeline build, and prepared the UI/UX design references in Alma.",
			Apps:      []string{"Konseki", "Alma", "Dia", "Finder"},
			Memory: []MemoryGroup{
				{
					Category: "Konseki Native UI (Alma, Konseki):",
					Items: []string{
						"Verified macOS Screen Recording permission flow and initial JSONL capture in `~/Downloads/02-Projects/konseki`.",
						"Shared UI/UX design screenshots for the `Capture`, `Summaries`, and `History` sections in Alma.",
					},
				},
			},
			PriorContext: "Follows the initial scaffolding of `konseki` on `mygo v0.7.4` native template.",
			UserContext: []string{
				"konseki",
				"mygo v0.7.4",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "14:29–14:35",
					AppsLabel: "Konseki and Dia",
					Detail:    "Launched `Konseki.app`, granted screen recording access, and verified live OCR snapshots.",
				},
				{
					TimeRange: "14:35–14:40",
					AppsLabel: "Alma",
					Detail:    "Drafted the UI/UX specification for `Capture`, `Summaries`, and expandable `History` timeline.",
				},
			},
		},
		{
			ID:        "slot-1000",
			TimeLabel: "10:00 AM",
			Title:     "Checked MyGo subagent CI status in T3 Code and chatted with David Lee",
			Summary:   "You messaged with David Lee about industry news and your subway commute times. You checked T3 Code for subagent CI test results and branch diffs on the MyGo refactoring thread, and briefly viewed Alma.",
			Apps:      []string{"Messages", "Dia", "T3 Code", "Alma", "Finder", "WeChat"},
			Memory: []MemoryGroup{
				{
					Category: "MyGo Refactor (T3 Code):",
					Items: []string{
						"Checked the `Refactor MyGo with Plugin System` thread on branch `t3/refactor-mygo-plugins-alma-ui`.",
						"Subagent 3E-2 round 3 isolated validation passed (Bun 473 passed) and was pushed in commit `dfd3c49`.",
						"Subagent 3E-3 round 2 (`a3a0614`) passed CI on both macOS and Ubuntu after a re-run of two transient sidecar test timeouts (cataloged as gotcha 33).",
						"Both 3E-2 round 3 CI and subagent 3G-2 round 2 (attachments implementation) remained in progress.",
						"Inspected the diff against `origin/phase2b-contract-infrastructure`, reviewing changes in `cmd/xtwork`, `docs/architecture/phase-3.md`, and `internal/app/attachments*.go`.",
					},
				},
				{
					Category: "Messaging:",
					Items: []string{
						"Exchanged messages with David Lee in Messages discussing Dongfeng Nissan, office relocations, and commute durations (5 minutes by electric scooter to the station, followed by 25–28 minutes on the train).",
					},
				},
			},
			PriorContext: "Continues from the earlier window tracking the MyGo plugin system refactoring in T3 Code (workspace `xiaotong_work_mygo`), where subagents 3E-2 and 3E-3 were implementing settings and sidecar changes.",
			UserContext: []string{
				"t3/refactor-mygo-plugins-alma-ui",
				"xiaotong_work_mygo",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "09:54–09:55",
					AppsLabel: "Messages and Dia",
					Detail:    "Exchanged iMessages with David Lee discussing work relocations and whether they were taking compensatory leave. Switched briefly to Dia to view Twitter notifications and user UFO2002616's page.",
				},
				{
					TimeRange: "09:55–09:57",
					AppsLabel: "T3 Code (Nightly)",
					Detail:    "Reviewed progress updates from assistant \"薯条\" in the thread `Refactor MyGo with Plugin System` within `xiaotong_work_mygo`. Read that subagent 3E-3 round 2 (`a3a0614`) cleared CI after re-running a macOS job that had timed out on sidecar tests. Observed that subagent 3E-2 round 3 was committed (`dfd3c49`) after passing isolation tests, while subagent 3G-2 round 2 continued working on attachments. Opened and inspected the file diff view against `origin/phase2b-contract-infrastructure`.",
				},
				{
					TimeRange: "09:57–09:57",
					AppsLabel: "Alma",
					Detail:    "Switched to Alma and viewed the primary prompt workspace and project directory listing (including `agent_workspace`, `Default`, `文件工作`, and `维护`).",
				},
				{
					TimeRange: "09:57–10:00",
					AppsLabel: "Messages, Finder, and WeChat",
					Detail:    "Returned to Messages to continue a conversation with David Lee, detailing commute timing (5 minutes to the station by scooter, 25 to 28 minutes on the metro, plus walking) and discussing news regarding Dongfeng Nissan. Concluded by switching briefly to Finder and opening the WeChat media picker.",
				},
			},
		},
		{
			ID:        "slot-0940",
			TimeLabel: "9:40 AM",
			Title:     "Updated Mole, reviewed MyGo 3E-3 subagent progress, and inspected Cicada MCP tools",
			Summary:   "You initiated an update for Mole, reviewed subagent progress and test results for MyGo's native settings in T3 Code, and studied Cicada's MCP exploration tools on GitHub.",
			Apps:      []string{"Dia", "Mole", "T3 Code"},
			Memory: []MemoryGroup{
				{
					Category: "MyGo Native Settings (T3 Code):",
					Items: []string{
						"Reviewed subagent 3E-3 progress on native settings bindings and sidecar test results in `xiaotong_work_mygo`.",
						"Verified test runs for `internal/settings` and inspected macOS CI runner output.",
					},
				},
				{
					Category: "Tooling & MCP Research (Mole, Dia):",
					Items: []string{
						"Initiated an update for `Mole` and verified its background daemon status.",
						"Studied `wende/cicada` MCP exploration tools and symbol graph indexing on GitHub.",
					},
				},
			},
			PriorContext: "Follows the 09:30 monitoring session on `t3/refactor-mygo-plugins-alma-ui` and GitHub exploration of `wende/cicada`.",
			UserContext: []string{
				"wende/cicada",
				"xiaotong_work_mygo",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "09:31–09:34",
					AppsLabel: "Mole",
					Detail:    "Initiated an in-place update for Mole and confirmed version status after restart.",
				},
				{
					TimeRange: "09:34–09:38",
					AppsLabel: "T3 Code (Nightly)",
					Detail:    "Reviewed subagent 3E-3 progress and native settings test results in `xiaotong_work_mygo`.",
				},
				{
					TimeRange: "09:38–09:40",
					AppsLabel: "Dia",
					Detail:    "Inspected `wende/cicada` MCP exploration tools and documentation on GitHub.",
				},
			},
		},
		{
			ID:        "slot-0930",
			TimeLabel: "9:30 AM",
			Title:     "Monitored MyGo refactoring in T3 Code and researched wende/cicada on GitHub",
			Summary:   "You monitored the progress of subagent refactoring tasks in T3 Code (Nightly). You browsed LINUX DO discussions on model performance, updated Alma to version 0.4.415, and researched the wende/cicada code exploration tool on GitHub.",
			Apps:      []string{"T3 Code", "Dia", "Alma"},
			Memory: []MemoryGroup{
				{
					Category: "Subagent Orchestration (T3 Code):",
					Items: []string{
						"Monitored subagent refactoring tasks on `t3/refactor-mygo-plugins-alma-ui` in T3 Code (Nightly).",
					},
				},
				{
					Category: "Alma & GitHub Research:",
					Items: []string{
						"Updated Alma to version `0.4.415` and browsed LINUX DO discussions on model performance.",
						"Researched `wende/cicada` code exploration tool on GitHub in Dia.",
					},
				},
			},
			PriorContext: "Continues from the 09:00–09:20 MyGo refactoring thread and Alma update sequence.",
			UserContext: []string{
				"Alma 0.4.415",
				"wende/cicada",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "09:21–09:25",
					AppsLabel: "T3 Code (Nightly)",
					Detail:    "Monitored active subagent refactoring tasks in `xiaotong_work_mygo`.",
				},
				{
					TimeRange: "09:25–09:28",
					AppsLabel: "Dia",
					Detail:    "Read LINUX DO model benchmark threads and opened `wende/cicada` on GitHub.",
				},
				{
					TimeRange: "09:28–09:30",
					AppsLabel: "Alma",
					Detail:    "Installed Alma `0.4.415` update and verified active workspace threads.",
				},
			},
		},
		{
			ID:        "slot-0920",
			TimeLabel: "9:20 AM",
			Title:     "Reviewed T3 Code usage metrics and ran Speedometer 3.1 in Yab",
			Summary:   "You reviewed model rate limits and 30-day API spending in T3 Code (Nightly). You started a Speedometer 3.1 benchmark run in Yab before quitting it, and sent messages in WeChat and managed contacts in Telegram.",
			Apps:      []string{"WeChat", "T3 Code", "Yab", "Telegram"},
			Memory: []MemoryGroup{
				{
					Category: "Metrics & Benchmarks (T3 Code, Yab):",
					Items: []string{
						"Reviewed model rate limits and 30-day API spending in `T3 Code (Nightly)`.",
						"Started a `Speedometer 3.1` benchmark run in Yab before quitting.",
					},
				},
				{
					Category: "Messaging (WeChat, Telegram):",
					Items: []string{
						"Sent messages in WeChat and managed contacts in Telegram.",
					},
				},
			},
			PriorContext: "Follows Yab settings inspection and T3 Code release notes review at 09:10 AM.",
			UserContext: []string{
				"Speedometer 3.1",
				"T3 Code (Nightly)",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "09:11–09:14",
					AppsLabel: "T3 Code (Nightly)",
					Detail:    "Checked model rate limits and 30-day API spending breakdown.",
				},
				{
					TimeRange: "09:14–09:17",
					AppsLabel: "Yab",
					Detail:    "Started `Speedometer 3.1` benchmark run and quit the browser mid-test.",
				},
				{
					TimeRange: "09:17–09:20",
					AppsLabel: "WeChat and Telegram",
					Detail:    "Replied to chats in WeChat and organized contacts in Telegram.",
				},
			},
		},
		{
			ID:        "slot-0910",
			TimeLabel: "9:10 AM",
			Title:     "Checked Yab settings, tracked T3 Code update, and messaged David Lee",
			Summary:   "You checked Small Window and Passwords settings in Yab and inspected update release notes in T3 Code (Nightly). You checked back on the MyGo Phase 1 inspection thread in T3 Code, browsed Telegram channels, and sent messages to David Lee in Messages.",
			Apps:      []string{"WeChat", "Yab", "T3 Code", "Telegram", "Messages"},
			Memory: []MemoryGroup{
				{
					Category: "Yab & T3 Code:",
					Items: []string{
						"Checked `Small Window` and `Passwords` settings in Yab.",
						"Inspected update release notes and the `MyGo Phase 1` inspection thread in T3 Code (Nightly).",
					},
				},
				{
					Category: "Messaging:",
					Items: []string{
						"Browsed Telegram channels and sent messages to David Lee in Messages.",
					},
				},
			},
			PriorContext: "Continues morning workflow after updating Alma to `0.4.414`.",
			UserContext: []string{
				"MyGo Phase 1",
				"Yab Small Window",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "09:01–09:04",
					AppsLabel: "Yab",
					Detail:    "Checked Small Window and Passwords settings.",
				},
				{
					TimeRange: "09:04–09:07",
					AppsLabel: "T3 Code (Nightly)",
					Detail:    "Inspected update release notes and checked the `MyGo Phase 1` inspection thread.",
				},
				{
					TimeRange: "09:07–09:10",
					AppsLabel: "Telegram, WeChat, and Messages",
					Detail:    "Browsed Telegram channels and sent messages to David Lee in Messages.",
				},
			},
		},
		{
			ID:        "slot-0900",
			TimeLabel: "9:00 AM",
			Title:     "Updated Alma to 0.4.414 and checked rate-limited MyGo refactoring thread in T3 Code",
			Summary:   "You updated Alma to version 0.4.414, verified its configuration, and returned to T3 Code to inspect the rate-limited MyGo refactoring thread.",
			Apps:      []string{"Alma", "T3 Code", "Dia"},
			Memory: []MemoryGroup{
				{
					Category: "Alma & MyGo Refactor:",
					Items: []string{
						"Updated Alma to version `0.4.414` and verified runtime configuration.",
						"Returned to T3 Code to check the rate-limited `MyGo refactoring` thread.",
					},
				},
			},
			PriorContext: "Initial morning checkpoint for the MyGo plugin refactoring sprint.",
			UserContext: []string{
				"Alma 0.4.414",
				"t3/refactor-mygo-plugins-alma-ui",
			},
			Recordings: []RecordingSegment{
				{
					TimeRange: "08:52–08:56",
					AppsLabel: "Alma",
					Detail:    "Updated Alma to `0.4.414` and verified configuration.",
				},
				{
					TimeRange: "08:56–09:00",
					AppsLabel: "T3 Code (Nightly)",
					Detail:    "Checked rate-limited MyGo refactoring thread on `t3/refactor-mygo-plugins-alma-ui`.",
				},
			},
		},
	}
}

// renderRichTextWithCodePills renders text where backtick-delimited segments (`code`)
// are styled as inline rounded monospace pills, matching the reference screenshot.
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
