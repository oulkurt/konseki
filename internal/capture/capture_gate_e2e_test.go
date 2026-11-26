//go:build e2e

/*
 * [INPUT]: 依赖构建期采集保护。
 * [OUTPUT]: 验证无窗口的授权边界。
 * [POS]: 采集入口回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"testing"
	"time"
)

func TestE2ECaptureGate(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", value)
			assertCaptureBackend(t, value == "1")
		})
	}
}

type e2eFrontSource struct {
	sys *fakeSystem
}

func (f e2eFrontSource) Front() Front { return f.sys.Front() }

func (f e2eFrontSource) LiveFront() Front { return (nativeObservations{}).LiveFront() }

func TestE2ERunnerStepBlocksLiveFrontAndScreen(t *testing.T) {
	oldScreen, oldLive := screenBackend, liveFrontBackend
	t.Cleanup(func() {
		screenBackend, liveFrontBackend = oldScreen, oldLive
	})
	gate := &gateScreen{frame: &Frame{W: 32, H: 32, RGBA: make([]byte, 32*32*4)}}
	liveCalls := 0
	screenBackend = func() Screen { return gate }
	liveFrontBackend = func() Front { liveCalls++; return Front{App: "Live", Bundle: "com.live.front", Title: "secret", PID: 12} }

	t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", "0")
	d, c, sys := fakeDependencies()
	d.screen = newScreen()
	d.front = e2eFrontSource{sys: sys}
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour, Idle: time.Minute}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Close()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.machine.initialized
	})
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.machine.Phase == PhaseBlocked && r.machine.Block == BlockNoScreenPermission
	})
	if liveCalls != 0 || gate.fronts != 0 || gate.grabs != 0 {
		t.Fatalf("E2E 禁止采集时仍调用原生路径: live=%d fronts=%d grabs=%d", liveCalls, gate.fronts, gate.grabs)
	}
}
