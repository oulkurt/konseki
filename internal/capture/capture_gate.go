/*
 * [INPUT]: 依赖构建期许可策略与平台屏幕、授权后端。
 * [OUTPUT]: 提供受保护的 Screen 和公开授权入口。
 * [POS]: Runner 与平台后端之间的唯一采集许可边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

var (
	screenBackend               = newPlatformScreen
	requestScreenBackend        = platformRequestScreenPermission
	requestAccessibilityBackend = platformRequestAccessibility
)

type guardedScreen struct{ Screen }

func newScreen() Screen { return guardedScreen{screenBackend()} }

func (s guardedScreen) Grab() *Frame {
	if !realCaptureAllowed() {
		return nil
	}
	return s.Screen.Grab()
}

func (s guardedScreen) Permission(ask bool) PermState {
	if !realCaptureAllowed() {
		return PermDenied
	}
	return s.Screen.Permission(ask)
}

func RequestScreenPermission() PermState {
	if !realCaptureAllowed() {
		return PermDenied
	}
	return requestScreenBackend()
}

// RequestAccessibility 仅由用户操作请求授权。
func RequestAccessibility() PermState {
	if !realCaptureAllowed() {
		return PermDenied
	}
	return requestAccessibilityBackend()
}
