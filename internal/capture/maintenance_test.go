/*
 * [INPUT]: 依赖可控时钟和可阻塞的存储维护操作。
 * [OUTPUT]: 验证维护周期、退出与超额请求合并。
 * [POS]: 后台维护调度的确定性测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"sync"
	"testing"
	"time"
)

type maintenanceCounts struct {
	mu                            sync.Mutex
	scan, prune, degrade, enforce int
}

func (c *maintenanceCounts) ops() maintenanceOps {
	return maintenanceOps{
		rescan:  func(*Storage) error { c.mu.Lock(); c.scan++; c.mu.Unlock(); return nil },
		prune:   func(*Storage, int, time.Time) (int, error) { c.mu.Lock(); c.prune++; c.mu.Unlock(); return 0, nil },
		degrade: func(*Storage, time.Time) (int, error) { c.mu.Lock(); c.degrade++; c.mu.Unlock(); return 0, nil },
		enforce: func(*Storage, int64) (int64, error) { c.mu.Lock(); c.enforce++; c.mu.Unlock(); return 0, nil },
	}
}
func (c *maintenanceCounts) get() (int, int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scan, c.prune, c.degrade, c.enforce
}
func waitAlarms(t *testing.T, c *fakeClock, n int) {
	t.Helper()
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.alarms) >= n })
}
func TestMaintenanceSchedule(t *testing.T) {
	d, c, _ := fakeDependencies()
	counts := &maintenanceCounts{}
	ops := counts.ops()
	d.maintenanceOps = &ops
	r, err := newRunner(Settings{Root: t.TempDir(), MaxStorage: 100}, d)
	if err != nil {
		t.Fatal(err)
	}
	stop, done, enforce := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	go r.maintenance(stop, done, enforce)
	defer func() { close(stop); <-done }()
	waitAlarms(t, c, 1)
	c.advance(59 * time.Second)
	if a, b, d, e := counts.get(); a+b+d+e != 0 {
		t.Fatal(a, b, d, e)
	}
	c.advance(time.Second)
	eventually(t, func() bool { a, b, d, e := counts.get(); return a == 1 && b == 1 && d == 1 && e == 1 })
	waitAlarms(t, c, 4)
	c.advance(5 * time.Minute)
	eventually(t, func() bool { _, _, _, e := counts.get(); return e == 2 })
	c.advance(25 * time.Minute)
	eventually(t, func() bool { a, b, d, e := counts.get(); return a == 2 && b == 1 && d == 2 && e == 3 })
	c.advance(5*time.Hour + 30*time.Minute)
	eventually(t, func() bool { a, b, d, e := counts.get(); return a == 3 && b == 2 && d == 3 && e == 4 })
}
func TestMaintenanceCoalescesEnforce(t *testing.T) {
	d, c, _ := fakeDependencies()
	started, release := make(chan int, 3), make(chan struct{})
	ops := defaultMaintenanceOps()
	n := 0
	ops.enforce = func(*Storage, int64) (int64, error) {
		n++
		started <- n
		if n == 1 {
			<-release
		}
		return 0, nil
	}
	d.maintenanceOps = &ops
	r, err := newRunner(Settings{Root: t.TempDir(), MaxStorage: 100}, d)
	if err != nil {
		t.Fatal(err)
	}
	stop, done := make(chan struct{}), make(chan struct{})
	r.enforce = make(chan struct{}, 1)
	go r.maintenance(stop, done, r.enforce)
	defer func() { close(stop); <-done }()
	waitAlarms(t, c, 1)
	r.mu.Lock()
	r.requestEnforce()
	r.mu.Unlock()
	if <-started != 1 {
		t.Fatal("首次调用")
	}
	r.mu.Lock()
	r.requestEnforce()
	r.requestEnforce()
	r.mu.Unlock()
	close(release)
	select {
	case got := <-started:
		if got != 2 {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("未补执行")
	}
	r.mu.Lock()
	pending := len(r.enforce)
	r.mu.Unlock()
	if pending != 0 {
		t.Fatal("重复请求未合并")
	}
}
