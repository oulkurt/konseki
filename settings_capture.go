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
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) renderCaptureSection(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	phaseColor, phaseLabel := ui.Hex("#71717A"), msg.ReceiptOff()
	switch a.status.Phase {
	case capture.PhaseActive:
		phaseColor, phaseLabel = ui.Hex("#22C55E"), msg.ReceiptActive()
	case capture.PhaseWaiting:
		phaseColor, phaseLabel = ui.Hex("#22C55E"), msg.ReceiptWaiting()
	case capture.PhasePaused:
		phaseColor, phaseLabel = ui.Hex("#F59E0B"), msg.ReceiptPaused()
	case capture.PhaseBlocked:
		phaseColor, phaseLabel = ui.Hex("#EF4444"), msg.ReceiptBlocked()
	}
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).FillWidth().Padding(14, 18).AlignItems(ui.Center).Gap(16).Children(func() {
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, msg.RecorderTitle()).FontSize(13.5).Bold().TextColor(pal.textPrimary)
						ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 8).Radius(999).Background(pal.rowHoverBg).Border(1, pal.cardBorder).Children(func() {
							ui.Box(c).Size(6, 6).Radius(3).Background(phaseColor)
							ui.Text(c, phaseLabel).FontSize(11).Bold().TextColor(pal.textSecondary)
						})
					})
					ui.Text(c, msg.RecorderEnableDescription()).FontSize(12.2).TextColor(pal.textSecondary).LineHeight(1.38)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Disabled(a.clearRunning).Children(func() {
					if a.cfgNow.Recorder.Enabled {
						renderSelectPill(c, "recorder-pause", msg.RecorderPause(), 100, pal.textPrimary, pal, func(m *ui.Menu) {
							if m.Item(msg.RecorderResume()).Chosen() {
								a.pauseRecorder(time.Time{})
							}
							for _, minutes := range []int{15, 60, 240} {
								if m.Item(msg.RecorderPauseMinutes(minutes)).Chosen() {
									a.pauseRecorder(a.currentTime().Add(time.Duration(minutes) * time.Minute))
								}
							}
						})
					}
					enabled := a.cfgNow.Recorder.Enabled
					if renderPillSwitch(c, "sw-enable-context", &enabled, pal) && a.runner != nil {
						a.setRecorderEnabled(enabled)
					}
				})
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
			ui.Row(c).FillWidth().Padding(0, 18, 14, 18).Gap(10).Children(func() {
				for _, item := range [][2]string{{msg.RecorderSessions(), fmt.Sprint(usage.Sessions)}, {msg.RecorderSnapshots(), fmt.Sprint(usage.Snapshots)}, {msg.RecorderCurrentSession(), current}, {msg.RecorderFrontApp(), front}} {
					ui.Column(c).Grow(1).Basis(0).Padding(10, 12).Gap(4).Radius(10).Background(pal.rowExpandedBg).Border(1, pal.rowDivider).Children(func() {
						ui.Text(c, item[0]).FontSize(11).TextColor(pal.textSecondary).SingleLine()
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

func (a *app) pauseRecorder(until time.Time) {
	if a.clearRunning || a.runner == nil {
		return
	}
	if err := a.cfg.Update(func(cfg *config.Config) { cfg.Recorder.PausedUntil = until }); err != nil {
		a.err = err.Error()
		return
	}
	a.cfgNow = a.cfg.Get()
	if until.IsZero() {
		a.runner.Resume()
	} else {
		a.runner.Pause(until)
	}
	a.status = a.runner.Status()
}

func (a *app) renderRecorderStorage(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	label := func(days int) string {
		if days == 0 {
			return msg.RecorderForever()
		}
		return msg.RecorderDays(days)
	}
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).FillWidth().Padding(14, 18, 0, 18).Children(func() {
				ui.Text(c, msg.RecorderStorage()).Bold().TextColor(pal.textPrimary)
			})
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
	})
}
