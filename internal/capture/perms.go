/*
 * [INPUT]: 依赖屏幕采集接口的权限与诊断查询。
 * [OUTPUT]: 生成平台权限诊断报告。
 * [POS]: 采集模块的诊断入口，不参与后台调度。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"fmt"
	"runtime"
	"strings"
)

// printPermReportFor 生成人可读的权限检查结果。
func printPermReportFor(scr Screen) string {
	var b strings.Builder
	b.WriteString("权限检查 (" + runtime.GOOS + ")\n\n")

	st := scr.Permission(false)
	switch st {
	case PermGranted:
		b.WriteString("屏幕录制: 已授予\n")
	case PermDenied:
		b.WriteString("屏幕录制: 未授予\n")
		b.WriteString("  系统设置 → 隐私与安全性 → 屏幕录制 → 勾选本 App\n")
		b.WriteString("  如果已勾选仍失败，退出 App 重新打开（授权需重启生效）\n")
	default:
		b.WriteString("屏幕录制: 未知\n")
		if runtime.GOOS == "windows" {
			b.WriteString("  Windows 传统截屏不需要授权，可直接使用\n")
		}
		if runtime.GOOS == "linux" {
			b.WriteString("  Wayland 下需要 xdg-desktop-portal 的 ScreenCast 授权\n")
		}
	}

	f := scr.Grab()
	defer releaseFrame(f)
	switch {
	case f == nil:
		b.WriteString("抓帧能力: 失败（没拿到图像）\n")
	case f.Blank:
		b.WriteString("抓帧能力: 截到全黑图\n")
		b.WriteString("  常见原因：1) 未授权  2) 授权后未重启  3) 屏幕锁着/休眠\n")
	default:
		fmt.Fprintf(&b, "抓帧能力: 正常（%dx%d）\n", f.W, f.H)
	}

	app, title, pid := scr.FrontWindow()
	fmt.Fprintf(&b, "前台窗口: app=%q title=%q pid=%d\n", app, title, pid)
	if title == "" && app != "" {
		b.WriteString("  标题为空 —— 通常就是缺屏幕录制权限（App 名不需要权限）")
	}
	return b.String()
}
