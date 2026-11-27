/*
 * [INPUT]: 依赖生产 Screen 构造器与公开授权入口。
 * [OUTPUT]: 用假后端计数验证阻断与放行，不调用原生 API。
 * [POS]: 普通与 e2e 构建共用的许可行为断言。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"reflect"
	"testing"
)

type gateScreen struct {
	fronts int
	grabs  int
	asks   []bool
	frame  *Frame
}

func (s *gateScreen) FrontWindow() (string, string, int) { s.fronts++; return "fake", "window", 7 }
func (s *gateScreen) Grab() *Frame                       { s.grabs++; return s.frame }
func (s *gateScreen) Permission(ask bool) PermState {
	s.asks = append(s.asks, ask)
	if ask {
		return PermGranted
	}
	return PermUnknown
}

func assertCaptureBackend(t *testing.T, allowed bool) {
	t.Helper()
	oldScreen, oldLive, oldRequest, oldAX := screenBackend, liveFrontBackend, requestScreenBackend, requestAccessibilityBackend
	t.Cleanup(func() {
		screenBackend, liveFrontBackend, requestScreenBackend, requestAccessibilityBackend = oldScreen, oldLive, oldRequest, oldAX
	})
	fake := &gateScreen{frame: &Frame{W: 31, H: 17}}
	screenRequests, axRequests, liveCalls := 0, 0, 0
	screenBackend = func() Screen { return fake }
	liveFrontBackend = func() Front { liveCalls++; return Front{App: "live", Title: "secret", Bundle: "com.live.app", PID: 9} }
	requestScreenBackend = func() PermState { screenRequests++; return PermGranted }
	requestAccessibilityBackend = func() PermState { axRequests++; return PermUnknown }
	screen := newScreen()
	app, title, pid := screen.FrontWindow()
	live := nativeObservations{}.LiveFront()
	frame := screen.Grab()
	passive, active := screen.Permission(false), screen.Permission(true)
	request, ax := RequestScreenPermission(), RequestAccessibility()
	if allowed {
		if app != "fake" || title != "window" || pid != 7 || live.Title != "secret" || live.Bundle != "com.live.app" || frame != fake.frame || passive != PermUnknown || active != PermGranted || request != PermGranted || ax != PermUnknown {
			t.Fatalf("后端返回值未保留: %q %q %d %+v %p %v %v %v %v", app, title, pid, live, frame, passive, active, request, ax)
		}
		if fake.fronts != 1 || liveCalls != 1 || fake.grabs != 1 || !reflect.DeepEqual(fake.asks, []bool{false, true}) || screenRequests != 1 || axRequests != 1 {
			t.Fatalf("后端调用不符: fronts=%d live=%d grabs=%d asks=%v screen=%d ax=%d", fake.fronts, liveCalls, fake.grabs, fake.asks, screenRequests, axRequests)
		}
	} else {
		if app != "" || title != "" || pid != 0 || live != (Front{}) || frame != nil || passive != PermDenied || active != PermDenied || request != PermDenied || ax != PermDenied {
			t.Fatal("阻断返回值不符")
		}
		if fake.fronts != 0 || liveCalls != 0 || fake.grabs != 0 || len(fake.asks) != 0 || screenRequests != 0 || axRequests != 0 {
			t.Fatalf("调用越过保护: fronts=%d live=%d grabs=%d asks=%v screen=%d ax=%d", fake.fronts, liveCalls, fake.grabs, fake.asks, screenRequests, axRequests)
		}
	}
}
