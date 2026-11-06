/*
 * [INPUT]: 依赖采集状态、配置与语言目录。
 * [OUTPUT]: 提供记录器总开关、统计与保留期限界面。
 * [POS]: 记录设置的概览边界，采集参数由 settings_recorder.go 呈现。
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

func (a *app) renderCaptureSection(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			a.settingsRow(c, msg.RecorderTitle(), msg.RecorderEnableDescription(), pal, func() {
				enabled := a.cfgNow.Recorder.Enabled
				if renderPillSwitch(c, "sw-enable-context", &enabled, pal) && a.runner != nil {
					a.setRecorderEnabled(enabled)
				}
			})
			usage := a.status.Usage
			current := msg.RecorderIdleState()
			if a.status.Session != nil {
				current = msg.RecorderActive()
			}
			front := a.status.Front.App
			if front == "" {
				front = "—"
			}
			ui.Row(c).FillWidth().Padding(0, 18, 14, 18).Gap(12).Children(func() {
				for _, item := range [][2]string{{msg.RecorderSessions(), fmt.Sprint(usage.Sessions)}, {msg.RecorderSnapshots(), fmt.Sprint(usage.Snapshots)}, {msg.RecorderCurrentSession(), current}, {msg.RecorderFrontApp(), front}} {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, item[0]).FontSize(11).TextColor(pal.textSecondary)
						ui.Text(c, item[1]).FontSize(14).Bold().TextColor(pal.textPrimary).SingleLine()
					})
				}
			})
			limit := a.cfgNow.Recorder.MaxStorageMB
			percent := 0
			if limit > 0 {
				percent = min(100, int(usage.Bytes*100/(int64(limit)<<20)))
			}
			ui.Column(c).FillWidth().Padding(0, 18, 14, 18).Gap(6).Children(func() {
				ui.Text(c, msg.RecorderStorageUsed(fmt.Sprintf("%.1f", float64(usage.Bytes)/(1<<20)), limit, percent)).FontSize(11).TextColor(pal.textSecondary)
				ui.Box(c).FillWidth().Height(5).Radius(3).Background(pal.rowHoverBg).Draw(func(p *ui.Painter, r ui.Rect) {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(percent) / 100, H: r.H}, pal.switchOn, 3)
				})
			})
		})
	})
}

func (a *app) renderRecorderStorage(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			a.settingsRow(c, msg.RecorderStorageLimit(), msg.RecorderStorageHelp(), pal, func() {
				if !a.storageFocus {
					a.storageDraft = strconv.Itoa(a.cfgNow.Recorder.MaxStorageMB)
				}
				in := ui.TextInput(c.Key("recorder-storage"), &a.storageDraft).Width(110).Label(msg.RecorderStorageLimit())
				focused := in.Focused()
				if in.Submitted() || (a.storageFocus && !focused) {
					if n, err := strconv.Atoi(strings.TrimSpace(a.storageDraft)); err == nil {
						a.updateRecorder(func(r *config.Recorder) { r.MaxStorageMB = n })
					}
				}
				a.storageFocus = focused
			})
			a.settingsRow(c, msg.RecorderOutput(), a.cfgNow.Recorder.Dir(a.appDir), pal, func() {
				if ui.Button(c.Key("recorder-reveal"), msg.RecorderReveal()).Clicked() && a.showFolder != nil {
					a.showFolder(a.cfgNow.Recorder.Dir(a.appDir))
				}
			})
		})
	})
	a.renderRetention(c, pal)
}

func (a *app) renderRetention(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	label := func(days int) string {
		if days == 0 {
			return msg.RecorderForever()
		}
		return msg.RecorderDays(days)
	}
	a.settingsCard(c, pal, func() {
		a.settingsRow(c, msg.RecorderRetention(), msg.RecorderRetentionDescription(), pal, func() {
			menu := func(m *ui.Menu) {
				for _, days := range []int{7, 14, 30, 90, 0} {
					if m.Item(label(days)).Checked(a.cfgNow.Recorder.RetentionDays == days).Chosen() {
						if err := a.cfg.Update(func(cfg *config.Config) { cfg.Recorder.RetentionDays = days }); err != nil {
							a.err = err.Error()
							return
						}
						a.applyRecorder()
					}
				}
			}
			renderSelectPill(c, "btn-retention", label(a.cfgNow.Recorder.RetentionDays), 100, pal.textPrimary, pal, menu)
		})
	})
}
