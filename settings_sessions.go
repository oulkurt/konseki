/*
 * [INPUT]: 依赖采集会话、摘要缓存和小票跳转。
 * [OUTPUT]: 提供最近会话缓存与可点击列表。
 * [POS]: 记录设置连接会话存储和时间轴，不修改会话。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"strings"
	"time"

	"konseki/internal/capture"
	"konseki/internal/summary"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

type recentSession struct {
	session capture.Session
	title   string
}

func sessionTitle(session capture.Session, entries []summary.Entry) string {
	for _, entry := range entries {
		if entry.Headline != "" && entry.Start.Before(session.End) && session.Start.Before(entry.End) {
			return entry.Headline
		}
	}
	return strings.Join(session.Apps, ", ")
}

func (a *app) loadRecentSessions() {
	if a.runner == nil || a.sums == nil || !a.cfgNow.Recorder.Enabled {
		return
	}
	if time.Since(a.lastSessions) < 10*time.Second {
		return
	}
	a.lastSessions = time.Now()
	entriesByDay := make(map[string][]summary.Entry)
	a.recentSessions = nil
	for _, session := range a.runner.RecentSessions(8) {
		day := session.Start.In(time.Local).Format("2006-01-02")
		entries, ok := entriesByDay[day]
		if !ok {
			entries, _ = a.sums.Load(day)
			entriesByDay[day] = entries
		}
		a.recentSessions = append(a.recentSessions, recentSession{session: session, title: sessionTitle(session, entries)})
	}
}

func (a *app) renderRecentSessions(c *ui.Context, pal palette) {
	if !a.cfgNow.Recorder.Enabled || len(a.recentSessions) == 0 {
		return
	}
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
			headingText(c, msg.RecorderRecent()).SingleLine().TextColor(pal.textPrimary)
			for _, item := range a.recentSessions {
				session := item.session
				title := item.title
				if title == "" {
					title = msg.RecorderSessionFallback()
				}
				button := ui.ButtonBase(c.Key("recent-"+session.ID)).FillWidth().Padding(6, 0).Label(title)
				button.Children(func() {
					ui.Column(c).FillWidth().Gap(3).Children(func() {
						headingText(c, title).FontSize(12).SingleLine().Tooltip(title).TextColor(pal.textPrimary)
						detail := msg.RecorderSessionDetails(int(session.End.Sub(session.Start).Minutes()), session.Snapshots)
						numText(c, session.Start.In(time.Local).Format("2006-01-02 15:04")+" · "+detail).FontSize(11).TextColor(pal.textSecondary)
					})
				})
				if button.Clicked() {
					a.showReceiptAt(session.Start, session.Start)
				}
			}
		})
	})
}
