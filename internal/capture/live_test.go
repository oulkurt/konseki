/*
 * [INPUT]: 依赖假时钟、窗口观测和模拟屏幕。
 * [OUTPUT]: 验证敏感窗口阻断抓屏与状态。
 * [POS]: 采集循环的隐私回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"sync"
	"testing"
	"time"
)

type liveCountingScreen struct {
	fakeScreen
	mu    sync.Mutex
	grabs int
}

func (s *liveCountingScreen) Grab() *Frame {
	s.mu.Lock()
	s.grabs++
	s.mu.Unlock()
	return s.fakeScreen.Grab()
}
func (s *liveCountingScreen) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.grabs }

// liveGrabs 统计无输入的 20 拍（5 秒）里抓屏几次。liveSensitive 为真时，
// 实时窗口所有者是敏感应用而工作区前台不是，比如密码管理器的浮窗。
func liveGrabs(t *testing.T, liveSensitive bool) (int, Status) {
	d, c, sys := fakeDependencies()
	screen := &liveCountingScreen{}
	d.screen = screen
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Second, Debounce: 250 * time.Millisecond, Idle: time.Hour, SensitiveApps: []string{"secret.app"}}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	if liveSensitive {
		sys.mu.Lock()
		sys.live = Front{Bundle: "secret.app", PID: 77}
		sys.mu.Unlock()
	}
	reads := func() int { sys.mu.Lock(); defer sys.mu.Unlock(); return sys.frontReads }
	for i := 0; i < 20; i++ {
		n0 := reads()
		c.advance(250 * time.Millisecond)
		eventually(t, func() bool { return reads() > n0 })
		time.Sleep(2 * time.Millisecond)
		eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return !r.machine.inFlight && !r.machine.probing })
	}
	return screen.count(), r.Status()
}

func TestLiveSensitiveBlocksCapture(t *testing.T) {
	normal, _ := liveGrabs(t, false)
	live, st := liveGrabs(t, true)
	t.Logf("5 秒无输入：正常前台抓屏 %d 次；窗口所有者敏感时抓屏 %d 次；此时 Phase=%d Block=%d", normal, live, st.Phase, st.Block)
	if live > normal {
		t.Errorf("敏感窗口在前时心跳每次防抖都重抓整屏")
	}
	if st.Phase != PhaseBlocked || st.Block != BlockSensitiveApp {
		t.Errorf("状态没有显示已拦截")
	}
}

func TestLiveSensitiveLeavesAndCaptureResumes(t *testing.T) {
	d, c, sys := fakeDependencies()
	screen := &liveCountingScreen{}
	d.screen = screen
	sys.live = Front{Bundle: "secret.app", PID: 77}
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Second, Debounce: time.Millisecond, SensitiveApps: []string{"secret.app"}}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(time.Second)
	eventually(t, func() bool { return r.Status().Block == BlockSensitiveApp })
	if screen.count() != 0 {
		t.Fatal("敏感窗口存在时抓屏")
	}
	sys.mu.Lock()
	sys.live = Front{}
	sys.mu.Unlock()
	c.advance(time.Second)
	eventually(t, func() bool { return r.Status().Block == BlockNone && screen.count() > 0 })
}

type sensitiveDuringGrab struct {
	fakeScreen
	sys *fakeSystem
}

func (s sensitiveDuringGrab) Grab() *Frame {
	f := s.fakeScreen.Grab()
	s.sys.mu.Lock()
	s.sys.live = Front{Bundle: "secret.app", PID: 77}
	s.sys.mu.Unlock()
	return f
}

func TestLiveSensitiveAppearsDuringGrab(t *testing.T) {
	d, c, sys := fakeDependencies()
	d.screen = sensitiveDuringGrab{sys: sys}
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Second, Debounce: time.Millisecond, SensitiveApps: []string{"secret.app"}}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(time.Second)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	c.advance(time.Second)
	eventually(t, func() bool { return r.Status().Block == BlockSensitiveApp })
	r.mu.Lock()
	inFlight, pending := r.machine.inFlight, r.machine.pending
	r.mu.Unlock()
	if inFlight || pending != "" {
		t.Fatalf("敏感帧触发重试: inFlight=%v pending=%q", inFlight, pending)
	}
	rows, err := r.Snapshots(c.Now().Format("2006-01-02"))
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
