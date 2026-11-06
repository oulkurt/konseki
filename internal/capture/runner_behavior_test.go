/*
 * [INPUT]: 依赖人工时钟、模拟画面和隔离的存储目录。
 * [OUTPUT]: 验证采集中停止、容量触发与 OCR 抽样。
 * [POS]: 采集外壳的并发与设置行为测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type blockingScreen struct {
	fakeScreen
	started chan struct{}
	release chan struct{}
}

func (s *blockingScreen) Grab() *Frame { close(s.started); <-s.release; return s.fakeScreen.Grab() }
func TestRunnerStopDuringCapture(t *testing.T) {
	d, c, sys := fakeDependencies()
	screen := &blockingScreen{started: make(chan struct{}), release: make(chan struct{})}
	d.screen = screen
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	<-screen.started
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	close(screen.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("采集结束后仍无法停止")
	}
	if r.Status().Phase != PhaseOff {
		t.Fatal(r.Status())
	}
	rows, err := r.Snapshots(c.Now().Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	count := len(rows)
	time.Sleep(20 * time.Millisecond)
	rows, _ = r.Snapshots(c.Now().Format("2006-01-02"))
	if len(rows) != count {
		t.Fatal("停止后仍写入")
	}
}
func TestRunnerOverQuotaRequestsEnforce(t *testing.T) {
	d, c, sys := fakeDependencies()
	enforced := make(chan int64, 1)
	ops := defaultMaintenanceOps()
	ops.enforce = func(_ *Storage, max int64) (int64, error) { enforced <- max; return 0, nil }
	d.maintenanceOps = &ops
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Hour, MaxStorage: 1}, d)
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
	select {
	case max := <-enforced:
		if max != 1 {
			t.Fatal(max)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超额未触发清理")
	}
}

type changingScreen struct {
	fakeScreen
	mu sync.Mutex
	n  byte
}

func (s *changingScreen) Grab() *Frame {
	s.mu.Lock()
	s.n++
	v := s.n
	s.mu.Unlock()
	f := s.fakeScreen.Grab()
	for i := 0; i < len(f.RGBA); i += 4 {
		f.RGBA[i] = v * 45
		f.RGBA[i+1] = v * 35
	}
	return f
}
func TestRunnerOCRSettingsSampling(t *testing.T) {
	d, c, sys := fakeDependencies()
	d.screen = &changingScreen{}
	cfg := Settings{Root: t.TempDir(), Heartbeat: time.Hour, OCR: OCRSettings{Enabled: false, EveryN: 2}}
	r, err := newRunner(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	day := c.Now().Format("2006-01-02")
	click := func(want int) {
		sys.mu.Lock()
		sys.click = c.Now()
		sys.mu.Unlock()
		c.advance(250 * time.Millisecond)
		eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == want })
	}
	click(1)
	r.mu.Lock()
	r.queue.mu.Lock()
	counts := len(r.queue.counts)
	r.queue.mu.Unlock()
	r.mu.Unlock()
	if counts != 0 {
		t.Fatal("关闭 OCR 时增加了计数")
	}
	cfg.OCR.Enabled = true
	r.Apply(cfg)
	click(2)
	click(3)
	click(4)
	eventually(t, func() bool { rows, _ := r.st.ReadOCR(day); return len(rows) == 2 })
	rows, _ := r.Snapshots(day)
	ocr, _ := r.st.ReadOCR(day)
	if ocr[0].Snap != rows[1].ID || ocr[1].Snap != rows[3].ID {
		t.Fatal(rows, ocr)
	}
}

type resizeDuplicateScreen struct {
	fakeScreen
	mu sync.Mutex
	n  int
}

func (s *resizeDuplicateScreen) Grab() *Frame {
	s.mu.Lock()
	s.n++
	n := s.n
	s.mu.Unlock()
	f := s.fakeScreen.Grab()
	if n == 1 {
		return f
	}
	wide := &Frame{W: f.W * 2, H: f.H * 2, RGBA: make([]byte, f.W*f.H*16)}
	for y := 0; y < wide.H; y++ {
		for x := 0; x < wide.W; x++ {
			src := ((y/2)*f.W + x/2) * 4
			dst := (y*wide.W + x) * 4
			copy(wide.RGBA[dst:dst+4], f.RGBA[src:src+4])
		}
	}
	return wide
}
func TestRunnerDuplicateUsesStoredDimensions(t *testing.T) {
	d, c, sys := fakeDependencies()
	d.screen = &resizeDuplicateScreen{}
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: time.Second}, d)
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
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return !r.machine.inFlight })
	c.advance(time.Second)
	eventually(t, func() bool { rows, _ := r.Snapshots(day); return len(rows) == 2 })
	rows, _ := r.Snapshots(day)
	if !rows[1].Dup || rows[1].W != rows[0].W || rows[1].H != rows[0].H {
		t.Fatal(rows)
	}
}

func TestRunnerSuspendPersistsPreviousTick(t *testing.T) {
	d, c, sys := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), Heartbeat: 24 * time.Hour, Idle: 24 * time.Hour}, d)
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
	before := c.Now()
	// 墙钟跨过睡眠，macOS 单调钟却不包含睡眠时长。
	c.advance(8 * time.Hour)
	eventually(t, func() bool { return r.Status().Session == nil })
	sessions := r.RecentSessions(1)
	if len(sessions) != 1 || sessions[0].EndReason != EndSuspend || !sessions[0].End.Equal(before) {
		t.Fatal(sessions)
	}
}

func TestClearAllDoesNotRecoverResidualSession(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要目录权限限制")
	}
	for _, running := range []bool{true, false} {
		t.Run(fmt.Sprintf("running=%v", running), func(t *testing.T) {
			d, c, _ := fakeDependencies()
			root := t.TempDir()
			r, err := newRunner(Settings{Root: root, Heartbeat: time.Hour}, d)
			if err != nil {
				t.Fatal(err)
			}
			if running {
				r.Start()
				c.advance(250 * time.Millisecond)
				eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
			}
			now := c.Now()
			if err := r.st.Append(Snapshot{TS: now.Format(time.RFC3339Nano), Session: "residual"}); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "sessions"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0555); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(root, 0755)
			cleared, err := r.ClearAll()
			if err != nil || cleared.Failed == 0 {
				t.Fatal(cleared, err)
			}
			if !running {
				r.Start()
			}
			c.advance(250 * time.Millisecond)
			eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.machine.initialized })
			r.Stop()
			rows, err := r.st.ReadSessions(now.Format("2006-01-02"))
			if err != nil || len(rows) != 0 {
				t.Fatalf("清空后恢复了残留会话: %v %v", rows, err)
			}
		})
	}
}
