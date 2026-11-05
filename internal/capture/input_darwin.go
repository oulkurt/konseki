//go:build darwin

/*
 * [INPUT]: 依赖 CoreGraphics、NSWorkspace 与辅助功能权限查询。
 * [OUTPUT]: 提供输入间隔、前台应用、锁屏和权限读取。
 * [POS]: macOS 只读观测边界，不注册输入监听。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"github.com/ebitengine/purego"
	"sync"
	"time"
)

var platformInit sync.Once
var eventSeconds func(uint32, uint32) float64
var sessionDictionary func() uintptr
var cfRelease func(uintptr)
var accessibilityTrusted func() bool

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
func (nativeObservations) Front() Front {
	initPlatform()
	var front Front
	withPool(func() {
		app := O(O(cls("NSWorkspace"), "sharedWorkspace"), "frontmostApplication")
		front = Front{App: gostr(O(app, "localizedName")), Bundle: gostr(O(app, "bundleIdentifier")), PID: int(O(app, "processIdentifier"))}
	})
	return front
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
		locked = O(O(d, "objectForKey:", nsstr("CGSSessionScreenIsLocked")), "boolValue") != 0
	})
	return locked
}
func (nativeObservations) Accessibility() PermState {
	initPlatform()
	if accessibilityTrusted() {
		return PermGranted
	}
	return PermDenied
}
func RequestScreenPermission() PermState {
	initPlatform()
	if requestScreen == nil {
		return PermUnknown
	}
	if requestScreen() {
		return PermGranted
	}
	return PermDenied
}
