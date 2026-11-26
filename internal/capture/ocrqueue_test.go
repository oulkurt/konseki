/*
 * [INPUT]: 依赖可控的识别引擎和临时事实存储。
 * [OUTPUT]: 验证队列溢出、超时和逐会话抽样。
 * [POS]: OCR 异步边界的独立测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingOCR struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingOCR) Recognize(*Frame, []string) (string, int, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	return "ok", 1, nil
}
func TestOCRQueueDropOldestAndTimeout(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := &blockingOCR{started: make(chan struct{}, 1), release: make(chan struct{})}
	q := newOCRQueue(engine, 30*time.Millisecond, nil)
	defer q.close()
	task := func(id string) ocrTask { return ocrTask{id: id, day: "2026-01-01", store: st, frame: &Frame{}} }
	cfg := OCRSettings{Enabled: true, EveryN: 1}
	q.submit("a", cfg, task("1"))
	<-engine.started
	q.submit("a", cfg, task("2"))
	q.submit("a", cfg, task("3"))
	q.submit("a", cfg, task("4"))
	q.mu.Lock()
	if q.dropped != 1 || len(q.tasks) != 2 || q.tasks[0].id != "3" {
		t.Fatalf("丢弃 %d 队列 %+v", q.dropped, q.tasks)
	}
	q.mu.Unlock()
	eventually(t, func() bool { rows, _ := st.ReadOCR("2026-01-01"); return len(rows) > 0 && rows[0].Status == "timeout" })
	close(engine.release)
}
func TestOCRQueueSessionSampling(t *testing.T) {
	q := &ocrQueue{counts: map[string]int{}, wake: make(chan struct{}, 1)}
	task := ocrTask{}
	off := OCRSettings{EveryN: 2}
	q.submit("a", off, task)
	if len(q.counts) != 0 {
		t.Fatal(q.counts)
	}
	on := OCRSettings{Enabled: true, EveryN: 2}
	q.submit("a", on, task)
	q.submit("a", on, task)
	q.submit("b", on, task)
	if q.counts["a"] != 2 || q.counts["b"] != 1 || len(q.tasks) != 2 {
		t.Fatal(q.counts, q.tasks)
	}
	q.submit("a", on, task)
	if q.dropped != 1 {
		t.Fatal(q.dropped)
	}
}

func TestOCRQueueClosedSubmitAndEmptySession(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := &blockingOCR{started: make(chan struct{}, 1), release: make(chan struct{})}
	q := newOCRQueue(engine, 5*time.Second, nil)
	cfg := OCRSettings{Enabled: true, EveryN: 1}
	q.submit("", cfg, ocrTask{id: "empty-sess", day: "2026-01-01", store: st, frame: &Frame{}})
	<-engine.started
	q.mu.Lock()
	_, hasEmptyKey := q.counts[""]
	q.mu.Unlock()
	if hasEmptyKey {
		close(engine.release)
		q.close()
		t.Fatal("空会话写入了抽样计数")
	}
	done := make(chan struct{})
	go func() {
		q.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(engine.release)
		t.Fatal("阻塞中的 OCR 队列关闭超时")
	}
	close(engine.release)
	q.submit("a", cfg, ocrTask{id: "after-close", day: "2026-01-01", store: st, frame: &Frame{}})
	q.mu.Lock()
	pending := len(q.tasks)
	q.mu.Unlock()
	if pending != 0 {
		t.Fatalf("关闭后仍入队任务: %d", pending)
	}
	rows, err := st.ReadOCR("2026-01-01")
	if err != nil || len(rows) != 0 {
		t.Fatalf("停止后仍写出 OCR 结果: %+v %v", rows, err)
	}
}

type cancelableBlockingOCR struct {
	mu           sync.Mutex
	calls        int
	firstStarted chan struct{}
	closeExited  chan struct{}
}

func (c *cancelableBlockingOCR) Recognize(f *Frame, langs []string) (string, int, error) {
	return c.RecognizeContext(context.Background(), f, langs)
}

func (c *cancelableBlockingOCR) RecognizeContext(ctx context.Context, _ *Frame, _ []string) (string, int, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()
	if n == 1 {
		close(c.firstStarted)
		<-ctx.Done()
		return "", 0, ctx.Err()
	}
	if c.closeExited != nil && n == 3 {
		<-ctx.Done()
		time.Sleep(15 * time.Millisecond)
		close(c.closeExited)
		return "", 0, ctx.Err()
	}
	return "recovered", 1, nil
}

func TestOCRQueueCancelableTimeoutAndCloseDrain(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	eng := &cancelableBlockingOCR{
		firstStarted: make(chan struct{}),
		closeExited:  make(chan struct{}),
	}
	q := newOCRQueue(eng, 25*time.Millisecond, nil)
	cfg := OCRSettings{Enabled: true, EveryN: 1}

	q.submit("s", cfg, ocrTask{id: "hang-1", day: "2026-01-02", store: st, frame: &Frame{}})
	<-eng.firstStarted
	q.submit("s", cfg, ocrTask{id: "next-2", day: "2026-01-02", store: st, frame: &Frame{}})

	eventually(t, func() bool {
		rows, err := st.ReadOCR("2026-01-02")
		if err != nil || len(rows) < 2 {
			return false
		}
		return rows[0].Status == "timeout" && rows[1].Status == "ok" && rows[1].Text == "recovered"
	})

	eng.closeExited = make(chan struct{})
	eng.mu.Lock()
	eng.calls = 2
	eng.mu.Unlock()
	q.submit("s", cfg, ocrTask{id: "close-3", day: "2026-01-02", store: st, frame: &Frame{}})
	eventually(t, func() bool {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		return eng.calls == 3
	})
	q.close()
	select {
	case <-eng.closeExited:
	default:
		t.Fatal("q.close() returned before cancelable worker exited")
	}
}

type stubbornCancelableOCR struct {
	mu        sync.Mutex
	calls     int
	canceled1 chan struct{}
	release1  chan struct{}
}

func (s *stubbornCancelableOCR) Recognize(f *Frame, langs []string) (string, int, error) {
	return s.RecognizeContext(context.Background(), f, langs)
}

func (s *stubbornCancelableOCR) RecognizeContext(ctx context.Context, _ *Frame, _ []string) (string, int, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == 1 {
		<-ctx.Done()
		close(s.canceled1)
		<-s.release1
		return "", 0, ctx.Err()
	}
	return "second", 1, nil
}

func TestOCRQueueUnresponsiveCancelableOCRBoundedTimeoutAndClose(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	eng := &stubbornCancelableOCR{
		canceled1: make(chan struct{}),
		release1:  make(chan struct{}),
	}
	t.Cleanup(func() { close(eng.release1) })
	q := newOCRQueue(eng, 25*time.Millisecond, nil)
	cfg := OCRSettings{Enabled: true, EveryN: 1}
	q.submit("s", cfg, ocrTask{id: "stuck-1", day: "2026-01-03", store: st, frame: &Frame{}})
	q.submit("s", cfg, ocrTask{id: "queued-2", day: "2026-01-03", store: st, frame: &Frame{}})

	select {
	case <-eng.canceled1:
	case <-time.After(time.Second):
		t.Fatal("timeout did not cancel stuck OCR worker")
	}
	eventually(t, func() bool {
		rows, err := st.ReadOCR("2026-01-03")
		return err == nil && len(rows) == 1 && rows[0].Status == "timeout"
	})

	eng.mu.Lock()
	callsBeforeClose := eng.calls
	eng.mu.Unlock()
	if callsBeforeClose != 1 {
		t.Fatalf("started second worker while stuck worker still active: calls=%d", callsBeforeClose)
	}

	closed := make(chan struct{})
	go func() {
		q.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("q.close() hung waiting for unresponsive cancelable worker")
	}
}

type restartOverlapOCR struct {
	mu            sync.Mutex
	calls         int
	active        int
	maxConcurrent int
	started1      chan struct{}
	release1      chan struct{}
}

func (r *restartOverlapOCR) Recognize(f *Frame, langs []string) (string, int, error) {
	return r.RecognizeContext(context.Background(), f, langs)
}

func (r *restartOverlapOCR) RecognizeContext(ctx context.Context, _ *Frame, _ []string) (string, int, error) {
	r.mu.Lock()
	r.calls++
	n := r.calls
	r.active++
	if r.active > r.maxConcurrent {
		r.maxConcurrent = r.active
	}
	started1 := r.started1
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()
	if n == 1 {
		close(started1)
		<-ctx.Done()
		<-r.release1
		return "", 0, ctx.Err()
	}
	return "second", 1, nil
}

func TestOCRQueueSharedPermitPreventsOverlapAcrossRestarts(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	eng := &restartOverlapOCR{
		started1: make(chan struct{}),
		release1: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(eng.release1) }) }
	t.Cleanup(release)

	permit := make(chan struct{}, 1)
	permit <- struct{}{}
	cfg := OCRSettings{Enabled: true, EveryN: 1}

	q1 := newOCRQueueWithPermit(eng, 5*time.Second, permit, nil)
	q1.submit("s1", cfg, ocrTask{id: "f1", day: "2026-01-04", store: st, frame: &Frame{}})
	<-eng.started1

	done1 := make(chan struct{})
	go func() {
		q1.close()
		close(done1)
	}()
	select {
	case <-done1:
	case <-time.After(4 * time.Second):
		t.Fatal("q1.close() exceeded bounded grace")
	}

	q2 := newOCRQueueWithPermit(eng, 5*time.Second, permit, nil)
	q2.submit("s2", cfg, ocrTask{id: "f2", day: "2026-01-04", store: st, frame: &Frame{}})
	time.Sleep(80 * time.Millisecond)
	eng.mu.Lock()
	callsDuringBlocked := eng.calls
	maxConcurrent := eng.maxConcurrent
	eng.mu.Unlock()
	if callsDuringBlocked != 1 || maxConcurrent != 1 {
		t.Fatalf("new queue overlapped stuck OCR worker: calls=%d maxConcurrent=%d", callsDuringBlocked, maxConcurrent)
	}

	done2 := make(chan struct{})
	go func() {
		q2.close()
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("q2.close() hung waiting on permit held by stuck worker")
	}

	q3 := newOCRQueueWithPermit(eng, 5*time.Second, permit, nil)
	defer q3.close()
	q3.submit("s3", cfg, ocrTask{id: "f3", day: "2026-01-04", store: st, frame: &Frame{}})
	time.Sleep(40 * time.Millisecond)
	release()

	eventually(t, func() bool {
		rows, err := st.ReadOCR("2026-01-04")
		return err == nil && len(rows) == 1 && rows[0].Status == "ok" && rows[0].Text == "second"
	})
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.calls != 2 || eng.maxConcurrent != 1 {
		t.Fatalf("unexpected execution counts after permit handoff: calls=%d maxConcurrent=%d", eng.calls, eng.maxConcurrent)
	}
}

func TestRunnerRestartDoesNotOverlapStuckOCR(t *testing.T) {
	eng := &restartOverlapOCR{
		started1: make(chan struct{}),
		release1: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(eng.release1) }) }

	d, clk, sys := fakeDependencies()
	d.ocr = eng
	r, err := newRunner(Settings{
		Root:        t.TempDir(),
		Idle:        2 * time.Second,
		TypingPause: 500 * time.Millisecond,
		Debounce:    time.Millisecond,
		Heartbeat:   time.Hour,
		JPEGQuality: 80,
		OCR:         OCRSettings{Enabled: true, EveryN: 1},
	}, d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		release()
		r.Close()
	})

	triggerShot := func(wantShots int) {
		clk.advance(250 * time.Millisecond)
		eventually(t, func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.machine.initialized
		})
		sys.mu.Lock()
		sys.key = clk.Now()
		sys.mu.Unlock()
		clk.advance(250 * time.Millisecond)
		eventually(t, func() bool { return r.Status().Session != nil })
		clk.advance(500 * time.Millisecond)
		day := clk.Now().Format("2006-01-02")
		eventually(t, func() bool {
			rows, _ := r.Snapshots(day)
			return len(rows) == wantShots
		})
	}

	r.Start()
	triggerShot(1)
	<-eng.started1

	stopped := make(chan struct{})
	go func() {
		r.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Runner.Stop hung beyond bounded cancelGrace")
	}

	r.Start()
	triggerShot(2)
	time.Sleep(80 * time.Millisecond)
	eng.mu.Lock()
	callsBeforeRelease := eng.calls
	maxBeforeRelease := eng.maxConcurrent
	eng.mu.Unlock()
	if callsBeforeRelease != 1 || maxBeforeRelease != 1 {
		t.Fatalf("Runner.Start overlapped still-stuck OCR worker: calls=%d maxConcurrent=%d", callsBeforeRelease, maxBeforeRelease)
	}

	release()
	day := clk.Now().Format("2006-01-02")
	eventually(t, func() bool {
		rows, _ := r.st.ReadOCR(day)
		return len(rows) == 1 && rows[0].Status == "ok" && rows[0].Text == "second"
	})
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.calls != 2 || eng.maxConcurrent != 1 {
		t.Fatalf("unexpected Runner OCR counts: calls=%d maxConcurrent=%d", eng.calls, eng.maxConcurrent)
	}
}
