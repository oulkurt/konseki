/*
 * [INPUT]: 依赖采集设置与界面组件。
 * [OUTPUT]: 提供采集设置界面。
 * [POS]: 设置页的采集区块。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"

	"konseki/internal/config"

	"github.com/egoist/mygo/ui"
)

func (a *app) renderCaptureSection(c *ui.Context, pal palette) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "Capture").
			FontSize(13).
			Bold().
			TextColor(pal.textSecondary).
			Margin(0, 0, 0, 10)

		// Card 1A: Enable Context Awareness
		a.settingsCard(c, pal, func() {
			// Row 1: Enable Context Awareness
			a.settingsRow(c,
				"Enable Context Awareness",
				"Capture your activity on this device.",
				pal,
				func() {
					enabled := a.cfgNow.Recorder.Enabled
					if renderPillSwitch(c, "sw-enable-context", &enabled, pal) && a.runner != nil {
						a.setRecorderEnabled(enabled)
					}
				},
			)

		})

		// Card 1B: Retention
		a.settingsCard(c, pal, func() {
			// Row 1: Retention
			// 采集到的原始截屏是最占空间也最敏感的东西，
			// 让用户能自己设一个上限，比事后清理体验好得多。
			retentionSub := "原始截图和屏幕文字保留的天数。摘要不会过期。"
			retentionLabel := fmt.Sprintf("%d days", a.cfgNow.Recorder.RetentionDays)
			if a.cfgNow.Recorder.RetentionDays <= 0 {
				retentionLabel = "Forever"
			}
			a.settingsRow(c, "Retention", retentionSub, pal, func() {
				menu := func(m *ui.Menu) {
					for _, days := range []int{7, 14, 30, 90, 0} {
						label := fmt.Sprintf("%d days", days)
						if days == 0 {
							label = "Forever"
						}
						if m.Item(label).Checked(a.cfgNow.Recorder.RetentionDays == days).Chosen() {
							a.cfg.Update(func(c *config.Config) { c.Recorder.RetentionDays = days })
							a.applyRecorder()
						}
					}
				}
				renderSelectPill(c, "btn-retention", retentionLabel, 100, pal.textPrimary, pal, menu)
			})

		})
	})
}
