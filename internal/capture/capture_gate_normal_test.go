//go:build !e2e

/*
 * [INPUT]: 依赖正式构建许可策略与假后端。
 * [OUTPUT]: 验证环境值不改变正式构建的后端调用。
 * [POS]: 正式构建的许可行为回归测试，不调用原生 API。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "testing"

func TestNormalCaptureGate(t *testing.T) {
	for _, value := range []string{"", "0", "1"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", value)
			assertCaptureBackend(t, true)
		})
	}
}
