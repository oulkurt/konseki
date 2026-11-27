/*
 * [INPUT]: 依赖状态机、平台观测、后台工作器和事实存储。
 * [OUTPUT]: 提供 Runner 生命周期、设置同步与采集状态快照。
 * [POS]: 采集生命周期的协调边界；首次采集前在循环中恢复会话。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Dependencies struct {
	Screen      Screen
	OCR         OCREngine
	Input       InputSource
	Totals      TotalsSource
	Front       FrontSource
	Lock        LockSource
	Clock       Clock
	Permissions PermissionSource
	SaveJPEG    func(*Frame, string, int) bool
}

func NewWithDependencies(cfg Settings, d Dependencies) (*Runner, error) {
	if d.Screen == nil || d.OCR == nil || d.Input == nil || d.Front == nil || d.Lock == nil || d.Clock == nil || d.Permissions == nil || d.SaveJPEG == nil {
		return nil, fmt.Errorf("capture: 采集依赖不完整")
	}
	return newRunner(cfg, dependencies{screen: d.Screen, ocr: d.OCR, input: d.Input, totals: d.Totals, front: d.Front, lock: d.Lock, clock: d.Clock, perms: d.Permissions, save: d.SaveJPEG})
}

type dependencies struct {
	screen         Screen
	ocr            OCREngine
	input          InputSource
	totals         TotalsSource
	front          FrontSource
	lock           LockSource
	clock          Clock
	perms          PermissionSource
	save           func(*Frame, string, int) bool
	appendSnapshot func(*Store, Snapshot) error
	maintenanceOps *maintenanceOps
}

type Runner struct {
	mu              sync.Mutex
	lifecycle       sync.Mutex
	bgMu            sync.Mutex
	bgWG            sync.WaitGroup
	cfg             Settings
	configured      bool
	st              *Store
	storage         *Storage
	deps            dependencies
	running         bool
	skipRecovery    bool
	stop            chan struct{}
	done            chan struct{}
	pausedUntil     time.Time
	machine         Machine
	counts          inputCounts
	countsVersion   uint64
	session         *Session
	front           Front
	lastShot        time.Time
	lastErr         error
	perms           Perms
	permsVersion    uint64
	baseline        *fingerprint
	generation      uint64
	runGen          uint64
	closedRun       bool
	flightSession   string
	flightProbe     bool
	closed          map[string]closedSession
	lastPath        string
	lastW, lastH    int
	blankFrames     int
	ocrPermit       chan struct{}
	queue           *ocrQueue
	enforce         chan struct{}
	maintenanceDone []<-chan struct{}
}

func New(cfg Settings) (*Runner, error) {
	native := nativeObservations{}
	return newRunner(cfg, dependencies{screen: newScreen(), ocr: newOCR(), input: native, totals: native, front: native, lock: native, clock: systemClock{}, perms: native, save: platformSaveJPEG})
}

func newRunner(cfg Settings, d dependencies) (*Runner, error) {
	st, err := NewStore(cfg.Root)
	if err != nil {
		return nil, err
	}
	if d.save == nil {
		d.save = platformSaveJPEG
	}
	ocrPermit := make(chan struct{}, 1)
	ocrPermit <- struct{}{}
	r := &Runner{st: st, storage: NewStorage(cfg.Root), deps: d, ocrPermit: ocrPermit}
	r.Apply(cfg)
	return r, nil
}

func (r *Runner) spawnBG(fn func()) bool {
	r.bgMu.Lock()
	if r.closedRun {
		r.bgMu.Unlock()
		return false
	}
	r.bgWG.Add(1)
	r.bgMu.Unlock()
	go func() {
		defer r.bgWG.Done()
		fn()
	}()
	return true
}

func (r *Runner) Apply(s Settings) {
	s.SensitiveApps = append([]string(nil), s.SensitiveApps...)
	s.OCR.Languages = append([]string(nil), s.OCR.Languages...)
	r.mu.Lock()
	if r.configured && r.cfg.InputCounts != s.InputCounts {
		r.counts.invalidate()
		r.countsVersion++
	}
	prune := r.configured && r.cfg.RetentionDays != s.RetentionDays
	enforce := r.configured && r.cfg.MaxStorage != s.MaxStorage
	r.cfg = s
	r.configured = true
	storage := r.storage
	runGen := r.runGen
	r.mu.Unlock()
	if prune || enforce {
		r.spawnBG(func() {
			r.applyStoragePolicy(storage, runGen, prune, enforce)
		})
	}
}

func (r *Runner) applyStoragePolicy(storage *Storage, runGen uint64, prune, enforce bool) {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()
	ops := defaultMaintenanceOps()
	if r.deps.maintenanceOps != nil {
		ops = *r.deps.maintenanceOps
	}
	if prune {
		_, err := ops.prune(storage, cfg.RetentionDays, r.deps.clock.Now())
		r.setErrorForRun(runGen, err)
	}
	if enforce && cfg.MaxStorage > 0 {
		_, err := ops.enforce(storage, cfg.MaxStorage)
		r.setErrorForRun(runGen, err)
	}
}

func (r *Runner) requestEnforce() {
	if r.enforce != nil {
		select {
		case r.enforce <- struct{}{}:
		default:
		}
	}
}

func (r *Runner) Storage() *Storage { r.mu.Lock(); defer r.mu.Unlock(); return r.storage }

func (r *Runner) Pause(until time.Time) {
	r.mu.Lock()
	if r.pausedUntil != until {
		r.counts.private = true
	}
	r.pausedUntil = until
	r.mu.Unlock()
}

func (r *Runner) Resume() { r.Pause(time.Time{}) }

func (r *Runner) SetPermissions(p Perms) {
	r.mu.Lock()
	r.perms = p
	r.permsVersion++
	r.mu.Unlock()
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	phase, block := r.machine.EffectivePhase(r.running, r.pausedUntil, r.deps.clock.Now())
	var session *Session
	if r.session != nil {
		v := *r.session
		v.Apps = append([]string(nil), v.Apps...)
		if v.Keys != nil {
			v.Keys = new(*v.Keys)
		}
		if v.Clicks != nil {
			v.Clicks = new(*v.Clicks)
		}
		session = &v
	}
	dropped := 0
	if r.queue != nil {
		r.queue.mu.Lock()
		dropped = r.queue.dropped
		r.queue.mu.Unlock()
	}
	return Status{OCRDropped: dropped, Phase: phase, Block: block, PausedUntil: r.pausedUntil, Front: r.front, Session: session, Usage: r.storage.Usage(), Perms: r.perms, LastShot: r.lastShot, LastErr: r.lastErr}
}

func (r *Runner) RecentSessions(n int) []Session {
	if n <= 0 {
		return nil
	}
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(st.root, "sessions"))
	if err != nil {
		return nil
	}
	var result []Session
	for i := len(entries) - 1; i >= 0 && len(result) < n; i-- {
		e := entries[i]
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		rows, err := st.ReadSessions(strings.TrimSuffix(e.Name(), ".jsonl"))
		if err == nil {
			result = append(result, rows...)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].End.After(result[j].End) })
	if len(result) > n {
		result = result[:n]
	}
	return result
}

func (r *Runner) Start() { r.lifecycle.Lock(); defer r.lifecycle.Unlock(); r.start() }

func (r *Runner) start() {
	r.bgMu.Lock()
	closed := r.closedRun
	r.bgMu.Unlock()
	if closed {
		return
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	r.enforce = make(chan struct{}, 1)
	r.machine = Machine{Phase: PhaseWaiting}
	r.counts.invalidate()
	r.countsVersion++
	r.session = nil
	r.baseline = nil
	r.lastPath = ""
	r.lastW, r.lastH = 0, 0
	r.blankFrames = 0
	r.flightSession = ""
	r.flightProbe = false
	r.lastErr = nil
	r.generation++
	r.runGen++
	runGen := r.runGen
	r.closed = make(map[string]closedSession)
	if r.ocrPermit == nil {
		r.ocrPermit = make(chan struct{}, 1)
		r.ocrPermit <- struct{}{}
	}
	r.queue = newOCRQueueWithPermit(r.deps.ocr, 30*time.Second, r.ocrPermit, r.setError)
	stop, done, enforce := r.stop, r.done, r.enforce
	st, storage, skipRecovery := r.st, r.storage, r.skipRecovery
	r.skipRecovery = false
	r.mu.Unlock()
	if err := cleanCaptureTemps(st.root, r.deps.clock.Now()); err != nil {
		r.setErrorForRun(runGen, err)
	}
	r.startMaintenance(stop, enforce, runGen)
	go r.loop(stop, done, st, storage, skipRecovery)
}

func cleanCaptureTemps(root string, now time.Time) error {
	dir := filepath.Join(root, "shots")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".capture-") || !strings.HasSuffix(entry.Name(), ".jpg") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) > time.Minute {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) Stop() { r.lifecycle.Lock(); defer r.lifecycle.Unlock(); r.stopLoop() }

func (r *Runner) Close() {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	r.bgMu.Lock()
	r.closedRun = true
	r.bgMu.Unlock()
	r.mu.Lock()
	r.runGen++
	r.mu.Unlock()
	r.stopLoop()
	r.waitMaintenance()
	r.bgMu.Lock()
	r.bgWG.Wait()
	r.bgMu.Unlock()
}

func (r *Runner) stopLoop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	close(r.stop)
	done := r.done
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	r.running = false
	r.enforce = nil
	r.mu.Unlock()
}

func (r *Runner) ClearAll() (Cleared, error) {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	r.mu.Lock()
	wasRunning := r.running
	r.mu.Unlock()
	if wasRunning {
		r.stopLoop()
	}
	r.waitMaintenance()
	r.bgMu.Lock()
	r.bgWG.Wait()
	r.bgMu.Unlock()
	r.mu.Lock()
	storage := r.storage
	r.mu.Unlock()
	cleared, err := storage.ClearAll()
	if errors.Is(err, ErrNotImplemented) {
		err = nil
	}
	r.mu.Lock()
	r.session = nil
	r.skipRecovery = true
	r.machine = Machine{}
	r.counts = inputCounts{}
	r.countsVersion++
	r.baseline = nil
	r.generation++
	r.runGen++
	r.flightSession = ""
	r.flightProbe = false
	r.closed = nil
	r.lastPath = ""
	r.lastShot = time.Time{}
	r.lastW, r.lastH = 0, 0
	r.lastErr = nil
	r.blankFrames = 0
	r.mu.Unlock()
	if wasRunning {
		r.start()
	}
	return cleared, err
}

func (r *Runner) setError(err error) {
	if err != nil && !errors.Is(err, ErrNotImplemented) {
		r.mu.Lock()
		r.lastErr = err
		r.mu.Unlock()
	}
}

func (r *Runner) setErrorForRun(gen uint64, err error) {
	if err != nil && !errors.Is(err, ErrNotImplemented) {
		r.mu.Lock()
		if r.runGen == gen {
			r.lastErr = err
		}
		r.mu.Unlock()
	}
}

func DoctorReport() string {
	return printPermReportFor(newScreen())
}

func (r *Runner) OCRIndex(dateKey string) (map[string]string, error) {
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	return st.OCRIndex(dateKey)
}

func (r *Runner) Snapshots(dateKey string) ([]Snapshot, error) {
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	return st.ReadDay(dateKey)
}

func (r *Runner) Root() string { r.mu.Lock(); defer r.mu.Unlock(); return r.st.root }
