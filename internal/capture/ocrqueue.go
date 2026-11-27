/*
 * [INPUT]: 依赖原始帧、会话计数、识别引擎和快照存储。
 * [OUTPUT]: 提供有界异步 OCR 队列与识别结果。
 * [POS]: 采集写盘后的独立消费者，不阻塞抓帧。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"context"
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
	mu          sync.Mutex
	closed      bool
	tasks       []ocrTask
	dropped     int
	counts      map[string]int
	wake        chan struct{}
	stop        chan struct{}
	done        chan struct{}
	permit      chan struct{}
	engine      OCREngine
	timeout     time.Duration
	cancelGrace time.Duration
	onError     func(error)
}

func newOCRQueue(engine OCREngine, timeout time.Duration, onError func(error)) *ocrQueue {
	return newOCRQueueWithPermit(engine, timeout, nil, onError)
}

func newOCRQueueWithPermit(engine OCREngine, timeout time.Duration, permit chan struct{}, onError func(error)) *ocrQueue {
	grace := min(timeout, 2*time.Second)
	if grace <= 0 {
		grace = 50 * time.Millisecond
	}
	if permit == nil {
		permit = make(chan struct{}, 1)
		permit <- struct{}{}
	}
	q := &ocrQueue{counts: map[string]int{}, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), permit: permit, engine: engine, timeout: timeout, cancelGrace: grace, onError: onError}
	go q.run()
	return q
}
func (q *ocrQueue) submit(session string, cfg OCRSettings, task ocrTask) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || !cfg.Enabled {
		return
	}
	n := cfg.EveryN
	if n < 1 {
		n = 1
	}
	if session != "" {
		count := q.counts[session]
		q.counts[session] = count + 1
		if count%n != 0 {
			return
		}
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
		default:
		}
		select {
		case <-q.stop:
			return
		case <-q.wake:
		}
		for {
			select {
			case <-q.stop:
				return
			default:
			}
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
	select {
	case <-q.stop:
		releaseFrame(task.frame)
		return
	default:
	}
	select {
	case <-q.permit:
	case <-q.stop:
		releaseFrame(task.frame)
		return
	}
	select {
	case <-q.stop:
		q.permit <- struct{}{}
		releaseFrame(task.frame)
		return
	default:
	}
	type answer struct {
		text  string
		lines int
		err   error
	}
	ce, canCancel := q.engine.(cancelableOCR)
	recCtx, cancelRec := context.WithCancel(context.Background())
	defer cancelRec()
	ch := make(chan answer, 1)
	start := time.Now()
	go func() {
		defer func() {
			releaseFrame(task.frame)
			q.permit <- struct{}{}
		}()
		var (
			text  string
			lines int
			err   error
		)
		if canCancel {
			text, lines, err = ce.RecognizeContext(recCtx, task.frame, task.languages)
		} else {
			text, lines, err = q.engine.Recognize(task.frame, task.languages)
		}
		ch <- answer{text, lines, err}
	}()
	timer := time.NewTimer(q.timeout)
	defer timer.Stop()
	result := OCRResult{Snap: task.id}
	timedOut := false
	workerExited := false
	select {
	case <-q.stop:
		cancelRec()
		if canCancel {
			grace := time.NewTimer(q.cancelGrace)
			select {
			case <-ch:
			case <-grace.C:
			}
			grace.Stop()
		}
		return
	case a := <-ch:
		workerExited = true
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
	case <-timer.C:
		result.Status = "timeout"
		timedOut = true
		cancelRec()
		if canCancel {
			grace := time.NewTimer(q.cancelGrace)
			select {
			case <-ch:
				workerExited = true
			case <-grace.C:
			case <-q.stop:
				grace.Stop()
				return
			}
			grace.Stop()
		}
	}
	select {
	case <-q.stop:
		return
	default:
	}
	result.Ms = int(time.Since(start).Milliseconds())
	if err := task.store.AppendOCR(task.day, result); err != nil && q.onError != nil {
		q.onError(err)
	}
	if timedOut && !workerExited {
		select {
		case <-ch:
		case <-q.stop:
		}
	}
}
func (q *ocrQueue) close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		<-q.done
		return
	}
	q.closed = true
	q.mu.Unlock()
	close(q.stop)
	<-q.done
}
