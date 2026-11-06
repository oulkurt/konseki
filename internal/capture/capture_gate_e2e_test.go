//go:build e2e

/*
 * [INPUT]: 依赖构建期采集保护。
 * [OUTPUT]: 验证无窗口的授权边界。
 * [POS]: 采集入口回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "testing"

func TestE2ECaptureGate(t *testing.T) {
	for _, value := range []string{"", "0", "true"} {
		t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", value)
		if realCaptureAllowed() {
			t.Fatal("implicit authorization")
		}
		if newScreen().Grab() != nil {
			t.Fatal("capture escaped gate")
		}
		if RequestScreenPermission() != PermDenied || RequestAccessibility() != PermDenied {
			t.Fatal("permission escaped gate")
		}
	}
	t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", "1")
	if !realCaptureAllowed() {
		t.Fatal("explicit authorization denied")
	}
}
