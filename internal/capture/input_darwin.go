//go:build darwin

/*
 * [INPUT]: 依赖 CoreGraphics、NSWorkspace、窗口所有者与辅助功能权限。
 * [OUTPUT]: 提供系统观测、运行中应用列表、权限读取和辅助功能授权请求。
 * [POS]: macOS 只读观测边界，不注册输入监听。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"github.com/ebitengine/purego"
	"slices"
	"strings"
	"sync"
	"time"
)

var platformInit sync.Once
var eventSeconds func(int32, uint32) float64
var sessionDictionary func() uintptr
var cfRelease func(uintptr)
var accessibilityTrusted func() uint8
var accessibilityWithOptions func(uintptr) uint8
var accessibilityPrompt uintptr

func initPlatform() {
	platformInit.Do(func() {
		withPool(func() {
			initRuntime()
			initScreenPerm()
			cg, _ := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW)
			mustReg(cg, "CGEventSourceSecondsSinceLastEventType", &eventSeconds)
			mustReg(cg, "CGSessionCopyCurrentDictionary", &sessionDictionary)
			cf, _ := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW)
			mustReg(cf, "CFRelease", &cfRelease)
			ax, _ := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW)
			mustReg(ax, "AXIsProcessTrusted", &accessibilityTrusted)
			mustReg(ax, "AXIsProcessTrustedWithOptions", &accessibilityWithOptions)
			accessibilityPrompt = nativeGlobal(ax, "kAXTrustedCheckOptionPrompt")
		})
	})
}

type nativeObservations struct{}

func (nativeObservations) Intervals() (time.Duration, time.Duration, time.Duration) {
	initPlatform()
	seconds := func(event uint32) time.Duration {
		v := eventSeconds(0, event)
		if v < 0 {
			return 0
		}
		if v > float64((24*time.Hour)/time.Second)*365 {
			return 365 * 24 * time.Hour
		}
		return time.Duration(v * float64(time.Second))
	}
	a, b := seconds(1), seconds(3)
	if b < a {
		a = b
	}
	return seconds(10), a, seconds(22)
}

// RunningApps 列出有 Dock 图标的运行中应用，按名字排序，供设置页加入敏感应用。
// 后台助手和菜单栏工具不会成为前台应用，所以不列出。
func RunningApps() []Front {
	initPlatform()
	var out []Front
	withPool(func() {
		apps := O(O(cls("NSWorkspace"), "sharedWorkspace"), "runningApplications")
		for i, n := uintptr(0), O(apps, "count"); i < n; i++ {
			app := O(apps, "objectAtIndex:", i)
			// NSApplicationActivationPolicyRegular 是 0。
			if O(app, "activationPolicy") != 0 {
				continue
			}
			if bundle := gostr(O(app, "bundleIdentifier")); bundle != "" {
				out = append(out, Front{App: gostr(O(app, "localizedName")), Bundle: bundle, PID: int(O(app, "processIdentifier"))})
			}
		}
	})
	slices.SortFunc(out, func(a, b Front) int { return strings.Compare(strings.ToLower(a.App), strings.ToLower(b.App)) })
	return out
}

func (nativeObservations) Front() Front {
	initPlatform()
	var front Front
	withPool(func() {
		app := O(O(cls("NSWorkspace"), "sharedWorkspace"), "frontmostApplication")
		front = Front{App: gostr(O(app, "localizedName")), Bundle: gostr(O(app, "bundleIdentifier")), PID: int(O(app, "processIdentifier"))}
	})
	return front
}

// LiveFront 从窗口所有者读取进程信息，避开主线程滞后的工作区缓存。
func (nativeObservations) LiveFront() (front Front) {
	initPlatform()
	withPool(func() {
		front.App, front.Title, front.PID = frontWindow()
		if front.PID != 0 {
			app := O(cls("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", uintptr(front.PID))
			front.Bundle = gostr(O(app, "bundleIdentifier"))
		}
	})
	return
}
func (nativeObservations) Locked() bool {
	initPlatform()
	var locked bool
	withPool(func() {
		d := sessionDictionary()
		if d == 0 {
			return
		}
		defer cfRelease(d)
		locked = objcBool(O(O(d, "objectForKey:", nsstr("CGSSessionScreenIsLocked")), "boolValue"))
	})
	return locked
}
func (nativeObservations) Accessibility() PermState {
	initPlatform()
	if accessibilityTrusted() != 0 {
		return PermGranted
	}
	return PermDenied
}
func RequestScreenPermission() PermState {
	initPlatform()
	if requestScreen == nil {
		return PermUnknown
	}
	if requestScreen() != 0 {
		return PermGranted
	}
	return PermDenied
}

// RequestAccessibility 请求辅助功能权限，应由用户操作触发。
func RequestAccessibility() (state PermState) {
	initPlatform()
	withPool(func() {
		options := O(cls("NSDictionary"), "dictionaryWithObject:forKey:", O(cls("NSNumber"), "numberWithBool:", 1), accessibilityPrompt)
		state = PermDenied
		if accessibilityWithOptions(options) != 0 {
			state = PermGranted
		}
	})
	return
}
