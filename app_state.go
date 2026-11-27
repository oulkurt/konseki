/*
 * [INPUT]: 依赖采集状态、摘要条目、供应商配置与后台上下文。
 * [OUTPUT]: 提供应用显式状态机、任务作用域与动画阶段转换方法。
 * [POS]: 根包的显式状态核心，替代散落布尔与哨兵字段。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"sync"
	"time"

	"konseki/internal/providers"
	"konseki/internal/summary"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

type appTasks struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

func (t *appTasks) ensure() context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx == nil && !t.closed {
		t.ctx, t.cancel = context.WithCancel(context.Background())
	}
	if t.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return t.ctx
}

func (t *appTasks) goTask(fn func(context.Context)) bool {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return false
	}
	if t.ctx == nil {
		t.ctx, t.cancel = context.WithCancel(context.Background())
	}
	ctx := t.ctx
	t.wg.Add(1)
	t.mu.Unlock()
	go func() {
		defer t.wg.Done()
		fn(ctx)
	}()
	return true
}

func (t *appTasks) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

func (t *appTasks) close() {
	t.mu.Lock()
	t.closed = true
	cancel := t.cancel
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	t.wg.Wait()
}

type summaryPhase uint8

const (
	summaryIdle summaryPhase = iota
	summaryRunning
	summaryFailed
)

type summaryJobState struct {
	phase    summaryPhase
	epoch    uint64
	dayKey   string
	cancel   context.CancelFunc
	done     chan struct{}
	inFlight []chan struct{}
	err      error
}

func (s *summaryJobState) running() bool {
	return s.phase == summaryRunning
}

func (s *summaryJobState) pruneInFlight() {
	kept := s.inFlight[:0]
	for _, ch := range s.inFlight {
		if ch == nil {
			continue
		}
		select {
		case <-ch:
		default:
			kept = append(kept, ch)
		}
	}
	s.inFlight = kept
}

func (s *summaryJobState) cancelActive() {
	s.epoch++
	if s.cancel != nil {
		s.cancel()
	}
	if s.phase == summaryRunning {
		s.phase = summaryIdle
	}
	s.cancel = nil
	s.done = nil
	s.pruneInFlight()
}

func (s *summaryJobState) start(parent context.Context, dayKey string, timeout time.Duration) (uint64, context.Context, context.CancelFunc, chan struct{}) {
	s.cancelActive()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	done := make(chan struct{})
	s.phase = summaryRunning
	s.dayKey = dayKey
	s.cancel = cancel
	s.done = done
	s.inFlight = append(s.inFlight, done)
	s.err = nil
	return s.epoch, ctx, cancel, done
}

func (s *summaryJobState) finish(epoch uint64, err error) bool {
	s.pruneInFlight()
	if s.epoch != epoch {
		return false
	}
	s.cancel = nil
	s.done = nil
	s.err = err
	if err != nil {
		s.phase = summaryFailed
	} else {
		s.phase = summaryIdle
	}
	return true
}

func (s *summaryJobState) waitAllChannel() <-chan struct{} {
	s.pruneInFlight()
	waitList := make([]chan struct{}, 0, len(s.inFlight)+1)
	seen := make(map[chan struct{}]bool, len(s.inFlight)+1)
	for _, ch := range s.inFlight {
		if ch != nil && !seen[ch] {
			seen[ch] = true
			waitList = append(waitList, ch)
		}
	}
	if s.done != nil && !seen[s.done] {
		select {
		case <-s.done:
		default:
			waitList = append(waitList, s.done)
		}
	}
	switch len(waitList) {
	case 0:
		return nil
	case 1:
		return waitList[0]
	default:
		all := make(chan struct{})
		go func(chs []chan struct{}) {
			for _, ch := range chs {
				<-ch
			}
			close(all)
		}(waitList)
		return all
	}
}

func (s *summaryJobState) errorText(lang string) string {
	if s.err == nil {
		return ""
	}
	return errorText(lang, s.err)
}

type clearPhase uint8

const (
	clearIdle clearPhase = iota
	clearRunning
	clearFailed
)

type clearSaga struct {
	phase   clearPhase
	confirm bool
	err     error
	receipt ReceiptDay
}

func (s *clearSaga) running() bool {
	return s.phase == clearRunning
}

type providerTestPhase uint8

const (
	testIdle providerTestPhase = iota
	testRunning
	testSucceeded
	testFailed
)

type providerTestState struct {
	phase      providerTestPhase
	providerID string
	epoch      uint64
	cancel     context.CancelFunc
	model      string
	latency    int
	err        error
}

func (s *providerTestState) start(parent context.Context, pid string, timeout time.Duration) (uint64, context.Context, context.CancelFunc) {
	if s.cancel != nil {
		s.cancel()
	}
	s.epoch++
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	s.phase = testRunning
	s.providerID = pid
	s.cancel = cancel
	s.model = ""
	s.latency = 0
	s.err = nil
	return s.epoch, ctx, cancel
}

func (s *providerTestState) finish(epoch uint64, pid, model string, latency int, err error) bool {
	if s.epoch != epoch || s.providerID != pid {
		return false
	}
	s.cancel = nil
	s.model = model
	s.latency = latency
	s.err = err
	if err != nil {
		s.phase = testFailed
	} else if model != "" {
		s.phase = testSucceeded
	} else {
		s.phase = testIdle
	}
	return true
}

func (s *providerTestState) resetForProvider(pid string) {
	if s.providerID != "" && s.providerID != pid {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.epoch++
	s.cancel = nil
	s.phase = testIdle
	s.providerID = ""
	s.model = ""
	s.latency = 0
	s.err = nil
}

func (s *providerTestState) clear() {
	if s.cancel != nil {
		s.cancel()
	}
	s.epoch++
	s.cancel = nil
	s.phase = testIdle
	s.providerID = ""
	s.model = ""
	s.latency = 0
	s.err = nil
}

type fetchModelsPhase uint8

const (
	fetchIdle fetchModelsPhase = iota
	fetchRunning
	fetchSucceeded
	fetchEmpty
	fetchFailed
)

type fetchModelsState struct {
	phase      fetchModelsPhase
	providerID string
	epoch      uint64
	cancel     context.CancelFunc
	count      int
	err        error
}

func (s *fetchModelsState) start(parent context.Context, pid string, timeout time.Duration) (uint64, context.Context, context.CancelFunc) {
	if s.cancel != nil {
		s.cancel()
	}
	s.epoch++
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	s.phase = fetchRunning
	s.providerID = pid
	s.cancel = cancel
	s.count = 0
	s.err = nil
	return s.epoch, ctx, cancel
}

func (s *fetchModelsState) finish(epoch uint64, pid string, count int, err error) bool {
	if s.epoch != epoch || s.providerID != pid {
		return false
	}
	s.cancel = nil
	s.count = count
	s.err = err
	switch {
	case err != nil:
		s.phase = fetchFailed
	case count == 0:
		s.phase = fetchEmpty
	default:
		s.phase = fetchSucceeded
	}
	return true
}

func (s *fetchModelsState) clear() {
	if s.cancel != nil {
		s.cancel()
	}
	s.epoch++
	s.cancel = nil
	s.phase = fetchIdle
	s.providerID = ""
	s.count = 0
	s.err = nil
}

func (s *fetchModelsState) message(lang string) string {
	msg := locales.In(lang)
	switch s.phase {
	case fetchFailed:
		if s.err == nil {
			return ""
		}
		return msg.ModelsFetchFailed() + errorText(lang, s.err)
	case fetchEmpty:
		return msg.ModelsFetchEmpty()
	case fetchSucceeded:
		return msg.ModelsFetchSuccess(s.count)
	default:
		return ""
	}
}

type scanPhase uint8

const (
	scanIdle scanPhase = iota
	scanRunning
	scanDone
)

type credScanState struct {
	phase    scanPhase
	epoch    uint64
	detected []providers.Credential
}

type legacyPhase uint8

const (
	legacyUnchecked legacyPhase = iota
	legacyReady
	legacyFailed
)

type legacyState struct {
	phase legacyPhase
	files []string
	err   error
}

type animPhase uint8

const (
	animIdle animPhase = iota
	animMeasuring
	animRunning
)

type paperFeedState struct {
	phase    animPhase
	day      string
	start    time.Time
	distance float32
	handle   ui.Handle
}

func (s *paperFeedState) resetDay() {
	s.phase = animIdle
	s.day = ""
	s.start = time.Time{}
}

type printRevealState struct {
	phase    animPhase
	revision int
	start    time.Time
	height   float32
	handle   ui.Handle
}

func (s *printRevealState) syncTo(rev int) {
	s.phase = animIdle
	s.revision = rev
	s.start = time.Time{}
	s.height = 0
}

type shredPhase uint8

const (
	shredIdle shredPhase = iota
	shredArmed
	shredRetracting
	shredFalling
)

type shredState struct {
	phase   shredPhase
	start   time.Time
	height  float32
	mask    *ui.Canvas
	handle  ui.Handle
	physics shredSimulation
	receipt ReceiptDay
}

func (s *shredState) active() bool {
	return s.phase != shredIdle
}

func (s *shredState) arm(day ReceiptDay) {
	s.phase = shredArmed
	s.start = time.Time{}
	s.height = 0
	s.physics = shredSimulation{}
	s.receipt = day
	s.mask = nil
}

func (s *shredState) stop() {
	s.phase = shredIdle
	s.start = time.Time{}
	s.height = 0
	s.physics = shredSimulation{}
	s.receipt = ReceiptDay{}
	s.mask = nil
}

type editDrafts struct {
	recorderDraft     [6]string
	recorderFocus     [6]bool
	ocrLanguagesDraft string
	ocrLanguagesFocus bool
	ocrEveryDraft     string
	ocrEveryFocus     bool
	sensitiveDraft    string
	sensitiveFocus    bool
	storageDraft      string
	storageFocus      bool
}

func (d *editDrafts) clearFocus() {
	d.recorderFocus = [6]bool{}
	d.ocrLanguagesFocus = false
	d.ocrEveryFocus = false
	d.sensitiveFocus = false
	d.storageFocus = false
}

func (a *app) postUI(fn func()) {
	if a.tasks.isClosed() {
		return
	}
	if a.post != nil {
		a.post(fn)
	}
}

func (a *app) shutdown() {
	a.summary.cancelActive()
	a.providerTest.clear()
	a.fetchState.clear()
	a.tasks.close()
	if a.runner != nil {
		a.runner.Close()
	}
}

func (a *app) bannerError() error {
	if a.err != nil {
		return a.err
	}
	return a.readErr
}

func (a *app) syncSummaryState(entries []summary.Entry) {
	a.sumEntries = entries
	if a.sums != nil {
		a.hasSummaries = len(a.sums.Days()) > 0
	}
}
