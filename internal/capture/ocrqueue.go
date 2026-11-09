/*
 * [INPUT]: 依赖原始帧、会话计数、识别引擎和快照存储。
 * [OUTPUT]: 提供有界异步 OCR 队列与识别结果。
 * [POS]: 采集写盘后的独立消费者，不阻塞抓帧。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"sync"
	"time"
)

type ocrTask struct {
	frame     *Frame
	id, day   string
	languages []string
	store     *Store
}
type ocrQueue struct {
	mu      sync.Mutex
	tasks   []ocrTask
	dropped int
	counts  map[string]int
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	engine  OCREngine
	timeout time.Duration
	onError func(error)
}

func newOCRQueue(engine OCREngine, timeout time.Duration, onError func(error)) *ocrQueue {
	q := &ocrQueue{counts: map[string]int{}, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), engine: engine, timeout: timeout, onError: onError}
	go q.run()
	return q
}
func (q *ocrQueue) submit(session string, cfg OCRSettings, task ocrTask) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !cfg.Enabled {
		return
	}
	n := cfg.EveryN
	if n < 1 {
		n = 1
	}
	count := q.counts[session]
	q.counts[session] = count + 1
	if count%n != 0 {
		return
	}
	task.languages = append([]string(nil), cfg.Languages...)
	if len(q.tasks) == 2 {
		releaseFrame(q.tasks[0].frame)
		q.tasks[0] = ocrTask{}
		q.tasks = q.tasks[1:]
		q.dropped++
	}
	task.frame = retainFrame(task.frame)
	q.tasks = append(q.tasks, task)
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *ocrQueue) run() {
	defer close(q.done)
	defer func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		for i := range q.tasks {
			releaseFrame(q.tasks[i].frame)
			q.tasks[i] = ocrTask{}
		}
		q.tasks = nil
	}()
	for {
		select {
		case <-q.stop:
			return
		case <-q.wake:
		}
		for {
			q.mu.Lock()
			if len(q.tasks) == 0 {
				q.mu.Unlock()
				break
			}
			task := q.tasks[0]
			q.tasks[0] = ocrTask{}
			q.tasks = q.tasks[1:]
			q.mu.Unlock()
			q.recognize(task)
			select {
			case <-q.stop:
				return
			default:
			}
		}
	}
}
func (q *ocrQueue) recognize(task ocrTask) {
	type answer struct {
		text  string
		lines int
		err   error
	}
	ch := make(chan answer, 1)
	start := time.Now()
	go func() {
		defer releaseFrame(task.frame)
		text, lines, err := q.engine.Recognize(task.frame, task.languages)
		ch <- answer{text, lines, err}
	}()
	result := OCRResult{Snap: task.id}
	select {
	case a := <-ch:
		result.Text = a.text
		result.Lines = a.lines
		switch {
		case a.err != nil:
			result.Status = "error"
		case a.text == "":
			result.Status = "empty"
		default:
			result.Status = "ok"
		}
	case <-time.After(q.timeout):
		result.Status = "timeout"
	case <-q.stop:
		return
	}
	result.Ms = int(time.Since(start).Milliseconds())
	if err := task.store.AppendOCR(task.day, result); err != nil && q.onError != nil {
		q.onError(err)
	}
	if result.Status == "timeout" {
		select {
		case <-ch:
		case <-q.stop:
		}
	}
}
func (q *ocrQueue) close() { close(q.stop); <-q.done }
