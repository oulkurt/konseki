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
	grabs int
	asks  []bool
	frame *Frame
}

func (s *gateScreen) FrontWindow() (string, string, int) { return "fake", "window", 7 }
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
	oldScreen, oldRequest, oldAX := screenBackend, requestScreenBackend, requestAccessibilityBackend
	t.Cleanup(func() {
		screenBackend, requestScreenBackend, requestAccessibilityBackend = oldScreen, oldRequest, oldAX
	})
	fake := &gateScreen{frame: &Frame{W: 31, H: 17}}
	screenRequests, axRequests := 0, 0
	screenBackend = func() Screen { return fake }
	requestScreenBackend = func() PermState { screenRequests++; return PermGranted }
	requestAccessibilityBackend = func() PermState { axRequests++; return PermUnknown }
	screen := newScreen()
	frame := screen.Grab()
	passive, active := screen.Permission(false), screen.Permission(true)
	request, ax := RequestScreenPermission(), RequestAccessibility()
	if allowed {
		if frame != fake.frame || passive != PermUnknown || active != PermGranted || request != PermGranted || ax != PermUnknown {
			t.Fatalf("后端返回值未保留: %p %v %v %v %v", frame, passive, active, request, ax)
		}
		if fake.grabs != 1 || !reflect.DeepEqual(fake.asks, []bool{false, true}) || screenRequests != 1 || axRequests != 1 {
			t.Fatalf("后端调用不符: grabs=%d asks=%v screen=%d ax=%d", fake.grabs, fake.asks, screenRequests, axRequests)
		}
	} else {
		if frame != nil || passive != PermDenied || active != PermDenied || request != PermDenied || ax != PermDenied {
			t.Fatal("阻断返回值不符")
		}
		if fake.grabs != 0 || len(fake.asks) != 0 || screenRequests != 0 || axRequests != 0 {
			t.Fatalf("调用越过保护: grabs=%d asks=%v screen=%d ax=%d", fake.grabs, fake.asks, screenRequests, axRequests)
		}
	}
}
