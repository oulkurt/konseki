//go:build !e2e

/*
 * [INPUT]: 依赖构建期采集保护。
 * [OUTPUT]: 验证无窗口的授权边界。
 * [POS]: 采集入口回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "testing"

func TestNormalCaptureGate(t *testing.T) {
	t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", "")
	if !realCaptureAllowed() {
		t.Fatal("production capture disabled")
	}
}
