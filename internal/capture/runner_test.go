/*
 * [INPUT]: 依赖临时目录和可控的屏幕、识别、观测与时钟。
 * [OUTPUT]: 验证采集外壳的快照、OCR 与会话写入。
 * [POS]: 采集外壳的端到端测试，不访问真实屏幕。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	ticks  chan time.Time
	alarms []*fakeAlarm
}

func (c *fakeClock) Now() time.Time                    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Ticks() (<-chan time.Time, func()) { return c.ticks, func() {} }

type fakeAlarm struct {
	next    time.Time
	period  time.Duration
	ch      chan time.Time
	stopped bool
}

func (c *fakeClock) schedule(d time.Duration, repeat bool) (<-chan time.Time, func()) {
	c.mu.Lock()
	a := &fakeAlarm{next: c.now.Add(d), ch: make(chan time.Time, 1)}
	if repeat {
		a.period = d
	}
	c.alarms = append(c.alarms, a)
	c.mu.Unlock()
	return a.ch, func() { c.mu.Lock(); a.stopped = true; c.mu.Unlock() }
}
func (c *fakeClock) Timer(d time.Duration) (<-chan time.Time, func())  { return c.schedule(d, false) }
func (c *fakeClock) Ticker(d time.Duration) (<-chan time.Time, func()) { return c.schedule(d, true) }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	for _, a := range c.alarms {
		if a.stopped || now.Before(a.next) {
			continue
		}
		select {
		case a.ch <- a.next:
		default:
		}
		if a.period == 0 {
			a.stopped = true
		} else {
			for !a.next.After(now) {
				a.next = a.next.Add(a.period)
			}
		}
	}
	c.mu.Unlock()
	c.ticks <- now
}

type fakeSystem struct {
	mu                    sync.Mutex
	key, click, scroll    time.Time
	clock                 *fakeClock
	front                 Front
	locked                bool
	frontReads, lockReads int
	live                  Front
}

func (s *fakeSystem) Intervals() (time.Duration, time.Duration, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	return now.Sub(s.key), now.Sub(s.click), now.Sub(s.scroll)
}
func (s *fakeSystem) Front() Front { s.mu.Lock(); defer s.mu.Unlock(); s.frontReads++; return s.front }
func (s *fakeSystem) LiveFront() Front {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live.Bundle != "" {
		return s.live
	}
	return s.front
}
func (s *fakeSystem) Locked() bool           { s.mu.Lock(); defer s.mu.Unlock(); s.lockReads++; return s.locked }
func (*fakeSystem) Accessibility() PermState { return PermGranted }

type fakeScreen struct{}

func (fakeScreen) FrontWindow() (string, string, int) { return "App", "Window", 1 }
func (fakeScreen) Permission(bool) PermState          { return PermGranted }
func (fakeScreen) Grab() *Frame {
	f := &Frame{W: 160, H: 90, RGBA: make([]byte, 160*90*4)}
	for i := 0; i < len(f.RGBA); i += 4 {
		f.RGBA[i] = byte(i % 251)
		f.RGBA[i+1] = 100
		f.RGBA[i+2] = 150
		f.RGBA[i+3] = 255
	}
	return f
}

type fakeOCR struct{}

func (fakeOCR) Recognize(*Frame, []string) (string, int, error) { return "识别文本", 1, nil }
func fakeDependencies() (dependencies, *fakeClock, *fakeSystem) {
	c := &fakeClock{now: time.Now(), ticks: make(chan time.Time, 100)}
	s := &fakeSystem{clock: c, key: c.now.Add(-time.Hour), click: c.now.Add(-time.Hour), scroll: c.now.Add(-time.Hour), front: Front{App: "App", Bundle: "a", PID: 1}}
	return dependencies{screen: fakeScreen{}, ocr: fakeOCR{}, input: s, front: s, lock: s, clock: c, perms: s, save: func(_ *Frame, path string, _ int) bool { return os.WriteFile(path, []byte("jpeg"), 0600) == nil }}, c, s
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待结果超时")
}
func TestRunnerScript(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), Idle: 2 * time.Second, TypingPause: 500 * time.Millisecond, Debounce: time.Millisecond, Heartbeat: time.Hour, JPEGQuality: 80, OCR: OCRSettings{Enabled: true, EveryN: 1}}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.key = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Session != nil })
	c.advance(500 * time.Millisecond)
	day := c.Now().Format("2006-01-02")
	eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == 1 })
	eventually(t, func() bool { rows, _ := r.st.ReadOCR(day); return len(rows) == 1 })
	c.advance(3 * time.Second)
	eventually(t, func() bool { return r.Status().Session == nil })
	rows, err := r.Snapshots(day)
	if err != nil || len(rows) != 1 || rows[0].Session == "" || rows[0].Trigger != TriggerTypingPause {
		t.Fatal(rows, err)
	}
	if _, err := os.Stat(r.st.ShotFile(rows[0].ID)); err != nil {
		t.Fatal(err)
	}
	sessions := r.RecentSessions(5)
	if len(sessions) != 1 || sessions[0].EndReason != EndIdle || sessions[0].Snapshots != 1 {
		t.Fatal(sessions)
	}
	sys.mu.Lock()
	sys.front = Front{App: "B", Bundle: "b", PID: 2}
	sys.mu.Unlock()
	c.advance(time.Second)
	eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == 2 })
	rows, _ = r.Snapshots(day)
	if rows[1].Trigger != TriggerAppFocus || rows[1].Bundle != "b" || rows[1].Dup {
		t.Fatal(rows[1])
	}
	eventually(t, func() bool { rows, _ := r.st.ReadOCR(day); return len(rows) == 2 })
	sys.mu.Lock()
	sys.locked = true
	sys.mu.Unlock()
	c.advance(time.Second)
	eventually(t, func() bool { return r.Status().Block == BlockScreenLocked })
	sessions = r.RecentSessions(5)
	if len(sessions) != 2 || sessions[0].EndReason != EndLock || sessions[0].Snapshots != 1 {
		t.Fatal(sessions)
	}

}

func TestRunnerHeartbeatDuplicate(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), Idle: time.Minute, Heartbeat: time.Second, Debounce: time.Millisecond, JPEGQuality: 75, OCR: OCRSettings{Enabled: true, EveryN: 2}}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	day := c.Now().Format("2006-01-02")
	eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == 1 })
	c.advance(time.Second)
	eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == 2 })
	rows, err := r.Snapshots(day)
	if err != nil || !rows[1].Dup || rows[1].Shot != rows[0].Shot || rows[1].Trigger != TriggerHeartbeat || rows[1].Bytes != 0 {
		t.Fatal(rows, err)
	}
	eventually(t, func() bool { ocr, _ := r.st.ReadOCR(day); return len(ocr) == 1 })
	r.mu.Lock()
	r.queue.mu.Lock()
	count := r.queue.counts[rows[0].Session]
	r.queue.mu.Unlock()
	r.mu.Unlock()
	if count != 1 {
		t.Fatalf("重复帧占用 OCR 抽样计数：%d", count)
	}
}
func TestRunnerRootChangesNextSession(t *testing.T) {
	d, c, sys := fakeDependencies()
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeTestFile(t, rootB, "shots/20261101-000000.jpg", []byte("existing"))
	cfg := Settings{Root: rootA, Idle: time.Second, Heartbeat: time.Hour, TypingPause: time.Second, JPEGQuality: 75}
	r, err := newRunner(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Session != nil })
	cfg.Root = rootB
	cfg.Idle = 10 * time.Second
	r.Apply(cfg)
	if r.Root() != rootA {
		t.Fatal(r.Root())
	}
	c.advance(2 * time.Second)
	eventually(t, func() bool { return r.Status().Session == nil })
	sys.mu.Lock()
	sys.scroll = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Root() == rootB && r.Status().Session != nil })
	eventually(t, func() bool { return r.Status().Usage.Shots == 1 })
	c.advance(2 * time.Second)
	eventually(t, func() bool { return r.Status().Session != nil })
}

func TestRunnerClearAllRestarts(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour, Idle: time.Minute}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.scroll = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Session != nil })
	r.setError(errors.New("清除前错误"))
	if _, err := r.ClearAll(); err != nil {
		t.Fatal(err)
	}
	if status := r.Status(); status.Phase != PhaseWaiting || status.Session != nil || status.LastErr != nil {
		t.Fatal(status)
	}
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.machine.initialized })
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	day := c.Now().Format("2006-01-02")
	eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == 1 })
	rows, err := r.Snapshots(day)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.st.ShotFile(rows[0].ID)); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCleansOldCaptureTemps(t *testing.T) {
	d, c, _ := fakeDependencies()
	root := t.TempDir()
	dir := filepath.Join(root, "shots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".capture-old.jpg", ".capture-new.jpg", "other.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := c.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(filepath.Join(dir, ".capture-old.jpg"), old, old); err != nil {
		t.Fatal(err)
	}
	r, err := newRunner(Settings{Root: root}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	if _, err := os.Stat(filepath.Join(dir, ".capture-old.jpg")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, name := range []string{".capture-new.jpg", "other.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

type blankScreen struct {
	mu    sync.Mutex
	grabs int
}

func (*blankScreen) FrontWindow() (string, string, int) { return "", "", 0 }
func (*blankScreen) Permission(bool) PermState          { return PermGranted }
func (s *blankScreen) Grab() *Frame {
	s.mu.Lock()
	s.grabs++
	s.mu.Unlock()
	return &Frame{Blank: true}
}
func (s *blankScreen) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.grabs }

func TestRunnerBlankBackoff(t *testing.T) {
	d, c, _ := fakeDependencies()
	screen := &blankScreen{}
	d.screen = screen
	r, err := newRunner(Settings{Root: t.TempDir(), ScreenPoll: time.Second, Heartbeat: time.Hour}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	for i, interval := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		c.advance(interval)
		eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.blankFrames == i+1 && !r.machine.probing })
	}
	if got := screen.count(); got != 3 {
		t.Fatalf("grabs=%d", got)
	}
	if err := r.Status().LastErr; err == nil || !strings.Contains(err.Error(), "截图全黑") || !strings.Contains(err.Error(), "重启 Konseki") {
		t.Fatal(err)
	}
	c.advance(time.Second)
	eventually(t, func() bool { return c.Now().Equal(r.deps.clock.Now()) })
	if got := screen.count(); got != 3 {
		t.Fatalf("退避失效: %d", got)
	}
}

func TestRunnerStopDoesNotWaitForMaintenance(t *testing.T) {
	d, _, _ := fakeDependencies()
	started, release := make(chan struct{}), make(chan struct{})
	ops := defaultMaintenanceOps()
	ops.enforce = func(*Storage, int64) (int64, error) { close(started); <-release; return 0, nil }
	d.maintenanceOps = &ops
	r, err := newRunner(Settings{Root: t.TempDir(), MaxStorage: 100}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	r.mu.Lock()
	r.requestEnforce()
	r.mu.Unlock()
	<-started
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("Stop 等待维护")
	}
	close(release)
}

func TestRunnerStatusDoesNotWaitForDisk(t *testing.T) {
	d, c, sys := fakeDependencies()
	started, release := make(chan struct{}), make(chan struct{})
	d.appendSnapshot = func(st *Store, snap Snapshot) error { close(started); <-release; return st.Append(snap) }
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	<-started
	done := make(chan struct{})
	go func() { _ = r.Status(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		close(release)
		t.Fatal("Status 等待磁盘")
	}
	close(release)
}

func TestRunnerObservationRates(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir()}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	for i := 1; i <= 5; i++ {
		c.advance(250 * time.Millisecond)
		want := i
		eventually(t, func() bool { sys.mu.Lock(); defer sys.mu.Unlock(); return sys.frontReads >= want })
	}
	// 每一拍先读前台再读锁屏。最后一拍前台读完时锁屏可能还没读，两样都等到再数。
	eventually(t, func() bool { sys.mu.Lock(); defer sys.mu.Unlock(); return sys.lockReads >= 2 })
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if sys.frontReads != 5 || sys.lockReads != 2 {
		t.Fatalf("前台 %d 次，锁屏 %d 次", sys.frontReads, sys.lockReads)
	}
}

func TestRunnerUsesObservationTimeAfterInput(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	c.mu.Lock()
	stale := c.now.Add(250 * time.Millisecond)
	c.now = c.now.Add(2 * time.Second)
	event := c.now.Add(-100 * time.Millisecond)
	c.mu.Unlock()
	sys.mu.Lock()
	sys.click = event
	sys.mu.Unlock()
	c.ticks <- stale
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.machine.lastClick.Equal(event) })
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return !r.machine.inFlight })
	r.mu.Lock()
	got := r.machine.lastClick
	r.mu.Unlock()
	if !got.Equal(event) {
		t.Fatalf("同一次点击被重复计算: %v", got)
	}
}

func TestRunnerRejectsLiveSensitiveWindow(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour, SensitiveApps: []string{"secret.app"}}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.live = Front{Bundle: "secret.app", PID: 77}
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.machine.initialized && !r.machine.inFlight && r.machine.Block == BlockSensitiveApp
	})
	rows, err := r.Snapshots(c.Now().Format("2006-01-02"))
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	entries, err := os.ReadDir(filepath.Join(r.Root(), "shots"))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestApplyStoragePolicyWhileStopped(t *testing.T) {
	d, _, _ := fakeDependencies()
	pruned := make(chan int, 1)
	enforced := make(chan int64, 1)
	ops := defaultMaintenanceOps()
	ops.prune = func(_ *Storage, days int, _ time.Time) (int, error) { pruned <- days; return 0, nil }
	ops.enforce = func(_ *Storage, max int64) (int64, error) { enforced <- max; return 0, nil }
	d.maintenanceOps = &ops
	cfg := Settings{Root: t.TempDir(), RetentionDays: 30, MaxStorage: 1000}
	r, err := newRunner(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RetentionDays = 7
	cfg.MaxStorage = 500
	r.Apply(cfg)
	select {
	case n := <-pruned:
		if n != 7 {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("未清理保留期")
	}
	select {
	case n := <-enforced:
		if n != 500 {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("未执行容量限制")
	}
}
