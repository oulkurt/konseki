/*
 * [INPUT]: 依赖采集配置、语言目录和 MyGo 输入控件。
 * [OUTPUT]: 提供表驱动的采集参数编辑界面。
 * [POS]: 记录设置的参数边界，将提交值交给配置归一化和采集器。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"strconv"
	"strings"

	"konseki/internal/config"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

type recorderField struct {
	key            string
	label          func(locales.Messages) string
	help           func(locales.Messages) string
	min, max, step int
	timed          bool
	get            func(config.Recorder) int
	set            func(*config.Recorder, int)
}

var recorderFields = []recorderField{
	{"debounce", locales.Messages.RecorderDebounce, locales.Messages.RecorderDebounceHelp, 3000, 30000, 100, true, func(r config.Recorder) int { return r.DebounceMs }, func(r *config.Recorder, n int) { r.DebounceMs = n }},
	{"heartbeat", locales.Messages.RecorderHeartbeat, locales.Messages.RecorderHeartbeatHelp, 60000, 300000, 1000, true, func(r config.Recorder) int { return r.HeartbeatMs }, func(r *config.Recorder, n int) { r.HeartbeatMs = n }},
	{"idle", locales.Messages.RecorderIdle, locales.Messages.RecorderIdleHelp, 10000, 600000, 5000, true, func(r config.Recorder) int { return r.IdleMs }, func(r *config.Recorder, n int) { r.IdleMs = n }},
	{"typing", locales.Messages.RecorderTyping, locales.Messages.RecorderTypingHelp, 800, 5000, 100, true, func(r config.Recorder) int { return r.TypingPauseMs }, func(r *config.Recorder, n int) { r.TypingPauseMs = n }},
	{"poll", locales.Messages.RecorderPoll, locales.Messages.RecorderPollHelp, 0, 30000, 500, true, func(r config.Recorder) int { return r.ScreenPollMs }, func(r *config.Recorder, n int) { r.ScreenPollMs = n }},
	{"quality", locales.Messages.RecorderQuality, locales.Messages.RecorderQualityHelp, 30, 95, 5, false, func(r config.Recorder) int { return r.JPEGQuality }, func(r *config.Recorder, n int) { r.JPEGQuality = n }},
}

func (a *app) stepRecorderField(i int, dir int) int {
	f := recorderFields[i]
	cur := f.get(a.cfgNow.Recorder)
	if n, err := strconv.Atoi(strings.TrimSpace(a.drafts.recorderDraft[i])); err == nil {
		cur = n
	}
	if f.key == "poll" {
		if dir < 0 && cur <= 10000 {
			return 0
		}
		if dir > 0 && cur == 0 {
			return 10000
		}
	}
	return cur + dir*f.step
}

func (a *app) commitRecorderField(i int, value int) {
	if a.clear.running() {
		return
	}
	f := recorderFields[i]
	value = max(f.min, min(f.max, value))
	if f.key == "quality" {
		value = (value + f.step/2) / f.step * f.step
	}
	if err := a.cfg.Update(func(c *config.Config) { f.set(&c.Recorder, value) }); err != nil {
		a.err = err
		return
	}
	a.applyRecorder()
	a.drafts.recorderDraft[i] = strconv.Itoa(f.get(a.cfgNow.Recorder))
}

func (a *app) renderRecorderFields(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			headingText(c, msg.RecorderCapture()).SingleLine().Padding(14, 18, 0, 18).TextColor(pal.textPrimary)
			for i, field := range recorderFields {
				unit := ""
				if field.timed {
					unit = msg.RecorderMilliseconds()
				}
				sub := field.help(msg) + "\n" + fmt.Sprintf("%d–%d · %d %s", field.min, field.max, field.step, unit)
				a.settingsRow(c, field.label(msg), sub, pal, func() {
					if !a.drafts.recorderFocus[i] {
						a.drafts.recorderDraft[i] = strconv.Itoa(field.get(a.cfgNow.Recorder))
					}
					input := ui.TextInput(c.Key("recorder-"+field.key), &a.drafts.recorderDraft[i]).Width(110).Disabled(a.clear.running()).Label(field.label(msg))
					focused := input.Focused()
					if input.Shortcut(0, ui.KeyUp) {
						a.commitRecorderField(i, a.stepRecorderField(i, 1))
					}
					if input.Shortcut(0, ui.KeyDown) {
						a.commitRecorderField(i, a.stepRecorderField(i, -1))
					}
					if input.Submitted() || (a.drafts.recorderFocus[i] && !focused) {
						if n, err := strconv.Atoi(strings.TrimSpace(a.drafts.recorderDraft[i])); err == nil {
							a.commitRecorderField(i, n)
						}
						a.drafts.recorderDraft[i] = strconv.Itoa(field.get(a.cfgNow.Recorder))
					}
					a.drafts.recorderFocus[i] = focused
				})
			}
		})
	})
}
