/*
 * [INPUT]: 依赖采集器权限状态和可注入的系统权限操作。
 * [OUTPUT]: 提供屏幕录制和辅助功能权限卡片。
 * [POS]: 记录设置的系统权限边界，只在用户操作时请求授权。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"runtime"

	"konseki/internal/capture"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) renderRecorderPermissions(c *ui.Context, pal palette) {
	if runtime.GOOS != "darwin" {
		return
	}
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
			headingText(c, msg.RecorderPermissions()).SingleLine().TextColor(pal.textPrimary)
			for _, item := range []struct {
				key, label, url string
				state           capture.PermState
				request         func() capture.PermState
			}{
				{"screen", msg.RecorderScreenPermission(), capture.ScreenPermissionSettingsURL, a.status.Perms.Screen, a.requestScreen},
				{"accessibility", msg.RecorderAccessibility(), capture.AccessibilityPermissionSettingsURL, a.status.Perms.Accessibility, a.requestAccessibility},
			} {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, item.label).Grow(1).FontSize(12).TextColor(pal.textPrimary)
					state := msg.RecorderPermissionMissing()
					if item.state == capture.PermGranted {
						state = msg.RecorderPermissionGranted()
					}
					ui.Text(c, state).FontSize(11).TextColor(pal.textSecondary)
					if item.state != capture.PermGranted {
						if ui.Button(c.Key("request-"+item.key), msg.RecorderPermissionRequest()).Clicked() && item.request != nil {
							state := item.request()
							if item.key == "screen" {
								a.status.Perms.Screen = state
							} else {
								a.status.Perms.Accessibility = state
							}
						}
						if ui.Button(c.Key("settings-"+item.key), msg.RecorderPermissionSettings()).Clicked() && a.openExternal != nil {
							if err := a.openExternal(item.url); err != nil {
								a.err = err
							}
						}
					}
				})
			}
			// 系统设置里授权以后，要重启 Konseki 才生效，这期间状态仍是未授权。生效以后不再提示。
			if a.status.Perms.Screen != capture.PermGranted {
				ui.Text(c, msg.RecorderPermissionRestart()).FontSize(11).TextColor(pal.textSecondary)
			}
		})
	})
}
