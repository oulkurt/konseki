/*
 * [INPUT]: 依赖人工时钟、模拟画面和隔离的存储目录。
 * [OUTPUT]: 验证采集中停止、容量触发与 OCR 抽样。
 * [POS]: 采集外壳的并发与设置行为测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
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
