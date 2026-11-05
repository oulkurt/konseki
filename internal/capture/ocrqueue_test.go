/*
 * [INPUT]: 依赖可控的识别引擎和临时事实存储。
 * [OUTPUT]: 验证队列溢出、超时和逐会话抽样。
 * [POS]: OCR 异步边界的独立测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
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
	q := newOCRQueue(engine, 30*time.Millisecond)
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
