/*
 * [INPUT]: 依赖状态机、平台观测、后台工作器和事实存储。
 * [OUTPUT]: 提供 Runner、状态快照与时间轴聚合。
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

// Dependencies 允许调用方隔离平台观测与图像编码。
type Dependencies struct {
	Screen      Screen
	OCR         OCREngine
	Input       InputSource
	Front       FrontSource
	Lock        LockSource
	Clock       Clock
	Permissions PermissionSource
	SaveJPEG    func(*Frame, string, int) bool
}

// NewWithDependencies 创建使用指定依赖的采集循环。
func NewWithDependencies(cfg Settings, d Dependencies) (*Runner, error) {
	if d.Screen == nil || d.OCR == nil || d.Input == nil || d.Front == nil || d.Lock == nil || d.Clock == nil || d.Permissions == nil || d.SaveJPEG == nil {
		return nil, fmt.Errorf("capture: 采集依赖不完整")
	}
	return newRunner(cfg, dependencies{screen: d.Screen, ocr: d.OCR, input: d.Input, front: d.Front, lock: d.Lock, clock: d.Clock, perms: d.Permissions, save: d.SaveJPEG})
}

type dependencies struct {
	screen         Screen
	ocr            OCREngine
	input          InputSource
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
	session         *Session
	front           Front
	lastShot        time.Time
	lastErr         error
	perms           Perms
	baseline        *fingerprint
	generation      uint64
	flightSession   string
	closed          map[string]closedSession
	lastPath        string
	lastW, lastH    int
	blankFrames     int
	queue           *ocrQueue
	enforce         chan struct{}
	maintenanceDone []<-chan struct{}
}

func New(cfg Settings) (*Runner, error) {
	native := nativeObservations{}
	return newRunner(cfg, dependencies{screen: newScreen(), ocr: newOCR(), input: native, front: native, lock: native, clock: systemClock{}, perms: native, save: platformSaveJPEG})
}
func newRunner(cfg Settings, d dependencies) (*Runner, error) {
	st, err := NewStore(cfg.Root)
	if err != nil {
		return nil, err
	}
	if d.save == nil {
		d.save = platformSaveJPEG
	}
	r := &Runner{st: st, storage: NewStorage(cfg.Root), deps: d}
	r.Apply(cfg)
	return r, nil
}
func (r *Runner) Apply(s Settings) {
	s.SensitiveApps = append([]string(nil), s.SensitiveApps...)
	s.OCR.Languages = append([]string(nil), s.OCR.Languages...)
	r.mu.Lock()
	prune := r.configured && r.cfg.RetentionDays != s.RetentionDays
	enforce := r.configured && r.cfg.MaxStorage != s.MaxStorage
	r.cfg = s
	r.configured = true
	storage := r.storage
	r.mu.Unlock()
	if prune || enforce {
		go r.applyStoragePolicy(storage, prune, enforce)
	}
}

func (r *Runner) applyStoragePolicy(storage *Storage, prune, enforce bool) {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()
	ops := defaultMaintenanceOps()
	if r.deps.maintenanceOps != nil {
		ops = *r.deps.maintenanceOps
	}
	if prune {
		_, err := ops.prune(storage, cfg.RetentionDays, r.deps.clock.Now())
		r.setError(err)
	}
	if enforce && cfg.MaxStorage > 0 {
		_, err := ops.enforce(storage, cfg.MaxStorage)
		r.setError(err)
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
func (r *Runner) Storage() *Storage     { r.mu.Lock(); defer r.mu.Unlock(); return r.storage }
func (r *Runner) Pause(until time.Time) { r.mu.Lock(); r.pausedUntil = until; r.mu.Unlock() }
func (r *Runner) Resume()               { r.Pause(time.Time{}) }
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	phase, block := r.machine.Phase, r.machine.Block
	if !r.running {
		phase = PhaseOff
		block = BlockNone
	} else if !r.pausedUntil.IsZero() && r.deps.clock.Now().Before(r.pausedUntil) {
		phase = PhasePaused
		block = BlockNone
	}
	var session *Session
	if r.session != nil {
		v := *r.session
		v.Apps = append([]string(nil), v.Apps...)
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
	r.closed = make(map[string]closedSession)
	r.queue = newOCRQueue(r.deps.ocr, 30*time.Second, r.setError)
	stop, done, enforce := r.stop, r.done, r.enforce
	st, storage, skipRecovery := r.st, r.storage, r.skipRecovery
	r.skipRecovery = false
	r.mu.Unlock()
	if err := cleanCaptureTemps(st.root, r.deps.clock.Now()); err != nil {
		r.setError(err)
	}
	r.startMaintenance(stop, enforce)
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
	r.baseline = nil
	r.generation++
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

var errBlankScreen = errors.New("截图全黑，屏幕录制授权后需要重启 Konseki 才能生效")

type closedSession struct {
	session *Session
	store   *Store
	storage *Storage
}

type captureJob struct {
	trigger    Trigger
	at         time.Time
	front      Front
	session    string
	store      *Store
	baseline   *fingerprint
	generation uint64
	storage    *Storage
	quality    int
	ocr        OCRSettings
	probe      bool
}
type captureResult struct {
	job         captureJob
	frame       *Frame
	fingerprint *fingerprint
	duplicate   bool
	blank       bool
	sensitive   bool
	title       string
	tmp         string
	err         error
}

func (r *Runner) grab(job captureJob) captureResult {
	result := captureResult{job: job}
	validFront := func() bool {
		f := r.deps.front.Front()
		return !r.deps.lock.Locked() && f.Bundle == job.front.Bundle && f.PID == job.front.PID
	}
	if !validFront() {
		return result
	}
	if r.liveSensitive() {
		result.sensitive = true
		return result
	}
	f := r.deps.screen.Grab()
	result.frame = f
	if !validFront() {
		releaseFrame(f)
		result.frame = nil
		return result
	}
	if r.liveSensitive() {
		releaseFrame(f)
		result.frame = nil
		result.sensitive = true
		return result
	}
	if f == nil {
		result.err = errors.New("抓取屏幕失败")
		return result
	}
	if f.Blank {
		result.blank = true
		return result
	}
	if !job.probe {
		_, result.title, _ = r.deps.screen.FrontWindow()
		if !validFront() {
			releaseFrame(f)
			result.frame = nil
			return result
		}
	}
	fp := fingerprintFrame(f)
	if fp == nil {
		result.err = errors.New("屏幕帧无效")
		return result
	}
	result.frame = f
	result.fingerprint = fp
	result.duplicate = duplicate(job.baseline, fp)
	if job.probe || result.duplicate {
		return result
	}
	dir := filepath.Join(job.store.root, "shots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		result.err = err
		return result
	}
	file, err := os.CreateTemp(dir, ".capture-*.jpg")
	if err != nil {
		result.err = err
		return result
	}
	result.tmp = file.Name()
	if err = file.Close(); err != nil {
		result.err = err
		os.Remove(result.tmp)
		return result
	}
	if !r.deps.save(f, result.tmp, job.quality) {
		result.err = errors.New("编码截图失败")
		os.Remove(result.tmp)
		result.tmp = ""
	}
	return result
}
func (r *Runner) liveSensitive() bool {
	source, ok := r.deps.front.(interface{ LiveFront() Front })
	if !ok {
		return false
	}
	live := source.LiveFront()
	r.mu.Lock()
	defer r.mu.Unlock()
	return sensitiveFront(live, r.cfg.SensitiveApps)
}

func (r *Runner) loop(stop <-chan struct{}, done chan<- struct{}, st *Store, storage *Storage, skipRecovery bool) {
	defer close(done)
	if !skipRecovery {
		if err := recoverSession(st, storage, r.deps.clock.Now()); err != nil {
			r.setError(err)
		}
	}
	ticks, cancel := r.deps.clock.Ticks()
	defer cancel()
	results := make(chan captureResult, 1)
	var inFlight bool
	var front Front
	var locked, liveSensitive bool
	var lastSystem, previousTick, lastPermissions time.Time
	var perms Perms
	finish := func(res captureResult, now time.Time, stopping bool) {
		inFlight = false
		defer releaseFrame(res.frame)
		r.mu.Lock()
		defer r.mu.Unlock()
		if res.sensitive {
			liveSensitive = true
		}
		o := Observation{Now: now, Front: front, LiveSensitive: liveSensitive, Locked: locked, ScreenPermission: perms.Screen, PausedUntil: r.pausedUntil, SinceKey: now.Sub(r.machine.lastKey), SinceClick: now.Sub(r.machine.lastClick), SinceScroll: now.Sub(r.machine.lastScroll), Stop: stopping}
		if res.blank {
			r.blankFrames++
			if r.blankFrames >= 3 {
				r.lastErr = errBlankScreen
			}
		} else if res.frame != nil {
			r.blankFrames = 0
			if r.lastErr == errBlankScreen {
				r.lastErr = nil
			}
		}
		if res.job.probe {
			o.ProbeDone = true
			o.ProbeDuplicate = res.duplicate || res.blank || res.frame == nil
			if res.err != nil {
				r.lastErr = res.err
			}
			r.finishClosed(res.job.session)
			r.perform(r.machine.Step(o, r.cfg), now, front, results, &inFlight)
			return
		}
		o.CaptureDone = true
		o.CaptureFailed = res.err != nil
		o.CaptureSkipped = res.blank
		if res.err != nil {
			r.lastErr = res.err
		}
		if res.frame != nil && !res.blank && res.err == nil && res.job.generation == r.generation && !locked && r.machine.Phase != PhaseBlocked && r.machine.Phase != PhasePaused && !systemFront(front) && !sensitiveFront(front, r.cfg.SensitiveApps) && (r.pausedUntil.IsZero() || !now.Before(r.pausedUntil)) {
			if !res.duplicate || res.job.trigger == TriggerHeartbeat {
				if err := r.persist(res); err != nil {
					r.lastErr = err
					o.CaptureFailed = true
				} else {
					o.CaptureSaved = true
				}
			}
		}
		if res.tmp != "" {
			r.mu.Unlock()
			_ = os.Remove(res.tmp)
			r.mu.Lock()
		}
		r.finishClosed(res.job.session)
		r.perform(r.machine.Step(o, r.cfg), now, front, results, &inFlight)
	}
	for {
		select {
		case <-stop:
			if inFlight {
				res := <-results
				finish(res, r.deps.clock.Now(), true)
				for inFlight {
					res = <-results
					finish(res, r.deps.clock.Now(), true)
				}
			}
			r.mu.Lock()
			r.perform(r.machine.Step(Observation{Now: r.deps.clock.Now(), Stop: true}, r.cfg), r.deps.clock.Now(), front, results, &inFlight)
			q := r.queue
			r.queue = nil
			r.mu.Unlock()
			q.close()
			return
		case res := <-results:
			finish(res, r.deps.clock.Now(), false)
		case <-ticks:
			key, click, scroll := r.deps.input.Intervals()
			now := r.deps.clock.Now()
			if lastPermissions.IsZero() || now.Sub(lastPermissions) >= 5*time.Second {
				perms = Perms{Screen: r.deps.screen.Permission(false), Accessibility: r.deps.perms.Accessibility()}
				lastPermissions = now
			}
			front = r.deps.front.Front()
			liveSensitive = r.liveSensitive()
			if lastSystem.IsZero() || now.Sub(lastSystem) >= time.Second {
				locked = r.deps.lock.Locked()
				lastSystem = now
			}
			r.mu.Lock()
			r.front = front
			r.perms = perms
			if !r.pausedUntil.IsZero() && !now.Before(r.pausedUntil) {
				r.pausedUntil = time.Time{}
			}
			o := Observation{Now: now, SinceKey: key, SinceClick: click, SinceScroll: scroll, Front: front, LiveSensitive: liveSensitive, Locked: locked, ScreenPermission: perms.Screen, PausedUntil: r.pausedUntil, PreviousTick: previousTick, Slept: !previousTick.IsZero() && now.Round(0).Sub(previousTick.Round(0)) > 5*time.Second}
			previousTick = now
			r.perform(r.machine.Step(o, r.cfg), now, front, results, &inFlight)
			r.mu.Unlock()
		}
	}
}
func (r *Runner) perform(actions []Action, now time.Time, front Front, results chan<- captureResult, inFlight *bool) {
	for _, a := range actions {
		switch a.Kind {
		case ActionResetBaseline:
			r.generation++
			r.baseline = nil
			r.lastPath = ""
			r.lastW, r.lastH = 0, 0
		case ActionOpenSession:
			root, _ := filepath.Abs(r.cfg.Root)
			if r.st.root != root {
				root := r.cfg.Root
				r.mu.Unlock()
				st, err := NewStore(root)
				r.mu.Lock()
				if err != nil {
					r.lastErr = err
				} else {
					r.st = st
					r.storage = NewStorage(root)
					storage := r.storage
					go func() { r.setError(storage.Rescan()) }()
					r.baseline = nil
					r.generation++
					r.lastPath = ""
					r.lastW, r.lastH = 0, 0
				}
			}
			r.session = &Session{ID: r.st.NewSnapshotID(now), Start: now}
		case ActionCloseSession:
			if r.session != nil {
				r.session.End = a.At
				r.session.EndReason = a.Reason
				if *inFlight && r.flightSession == r.session.ID {
					r.closed[r.session.ID] = closedSession{r.session, r.st, r.storage}
				} else {
					r.writeSession(closedSession{r.session, r.st, r.storage})
				}
				r.session = nil
			}
		case ActionCapture, ActionProbe:
			session := ""
			if r.session != nil {
				session = r.session.ID
			}
			job := captureJob{trigger: a.Trigger, at: now, front: front, session: session, store: r.st, baseline: r.baseline, generation: r.generation, storage: r.storage, quality: r.cfg.JPEGQuality, ocr: r.cfg.OCR, probe: a.Kind == ActionProbe}
			*inFlight = true
			r.flightSession = session
			go func() { results <- r.grab(job) }()
		}
	}
}
func (r *Runner) writeSession(c closedSession) {
	if r.queue != nil {
		r.queue.mu.Lock()
		delete(r.queue.counts, c.session.ID)
		r.queue.mu.Unlock()
	}
	r.mu.Unlock()
	err := c.store.AppendSession(*c.session)
	if err == nil {
		c.storage.CountSession()
	}
	r.mu.Lock()
	if err != nil {
		r.lastErr = err
	}
}
func (r *Runner) finishClosed(id string) {
	if c, ok := r.closed[id]; ok {
		r.writeSession(c)
		delete(r.closed, id)
	}
}
func (r *Runner) persist(res captureResult) error {
	job := res.job
	id := job.store.NewSnapshotID(job.at)
	path := r.lastPath
	size := int64(0)
	w, h := res.frame.W, res.frame.H
	if res.duplicate {
		w, h = r.lastW, r.lastH
	}
	r.mu.Unlock()
	err := func() error {
		if !res.duplicate {
			dest := job.store.ShotFile(id)
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			if err := os.Rename(res.tmp, dest); err != nil {
				return err
			}
			info, err := os.Stat(dest)
			if err != nil {
				return err
			}
			size = info.Size()
			path, err = filepath.Rel(job.store.root, dest)
			if err != nil {
				return err
			}
			if w > screenshotMaxDimension || h > screenshotMaxDimension {
				if w >= h {
					h = h * screenshotMaxDimension / w
					w = screenshotMaxDimension
				} else {
					w = w * screenshotMaxDimension / h
					h = screenshotMaxDimension
				}
			}
		} else if path == "" {
			return nil
		}
		snap := Snapshot{ID: id, TS: job.at.Format(time.RFC3339Nano), Session: job.session, Trigger: job.trigger, App: job.front.App, Bundle: job.front.Bundle, Title: res.title, PID: job.front.PID, Shot: path, Dup: res.duplicate, W: w, H: h, Bytes: size, ImgKB: int(size / 1024)}
		appendSnapshot := r.deps.appendSnapshot
		if appendSnapshot == nil {
			appendSnapshot = (*Store).Append
		}
		if err := appendSnapshot(job.store, snap); err != nil {
			if !res.duplicate {
				_ = os.Remove(job.store.ShotFile(id))
			}
			return err
		}
		return nil
	}()
	r.mu.Lock()
	if err != nil {
		return err
	}
	if res.duplicate && path == "" {
		return nil
	}

	if !res.duplicate {
		r.baseline = res.fingerprint
		r.lastPath = path
		r.lastW, r.lastH = w, h
		job.storage.Account(size)
		if r.cfg.MaxStorage > 0 && job.storage.Usage().Bytes > r.cfg.MaxStorage {
			r.requestEnforce()
		}
	}
	r.lastShot = job.at
	session := r.session
	if session == nil || session.ID != job.session {
		session = r.closed[job.session].session
	}
	if session != nil && session.ID == job.session {
		session.Snapshots++
		seen := false
		for _, app := range session.Apps {
			if app == job.front.App {
				seen = true
			}
		}
		if !seen && job.front.App != "" {
			session.Apps = append(session.Apps, job.front.App)
		}
	}
	if !res.duplicate {
		r.queue.submit(job.session, job.ocr, ocrTask{frame: res.frame, id: id, day: job.at.In(time.Local).Format("2006-01-02"), store: job.store})
	}
	return nil
}

// ---------- 时间轴聚合 ----------

// Block 是一个时间段：同一 App、间隔小于 10 分钟的连续采样归并而成。
type Block struct {
	Start, End time.Time
	App        string
	Titles     []string
	Samples    int
}

// Duration 返回这个时间块的跨度。
func (b Block) Duration() time.Duration { return b.End.Sub(b.Start) }

// Timeline 读某一天的数据并聚合成时间块。
// dateKey 形如 2026-11-04；传空字符串表示今天。
func (r *Runner) Timeline(dateKey string) ([]Block, error) {
	if dateKey == "" {
		dateKey = r.deps.clock.Now().Format("2006-01-02")
	}
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	snaps, err := st.ReadSnapshots(dateKey)
	if err != nil {
		return nil, err
	}
	return Aggregate(snaps), nil
}

// Aggregates 把原始采样聚合成时间块。
//
// 规则：同一 App 且与上一块的间隔小于 10 分钟，就并入同一块。
// 这样一次连续的工作不会因为中间心跳几次就被切碎。
func Aggregate(snaps []Snapshot) []Block {
	var blocks []Block
	for _, s := range snaps {
		ts, err := parseTS(s.TS)
		if err != nil {
			continue
		}
		if n := len(blocks); n > 0 && blocks[n-1].App == s.App && ts.Sub(blocks[n-1].End) < 10*time.Minute {
			b := &blocks[n-1]
			b.End = ts
			b.Samples++
			if s.Title != "" && (len(b.Titles) == 0 || b.Titles[len(b.Titles)-1] != s.Title) {
				b.Titles = append(b.Titles, s.Title)
			}
			continue
		}
		blocks = append(blocks, Block{Start: ts, End: ts, App: s.App, Titles: []string{s.Title}, Samples: 1})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start.Before(blocks[j].Start) })
	return blocks
}

// parseTS 容忍多种时间格式，避免数据源差异导致整段丢记录。
func parseTS(v string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339, time.RFC3339Nano,
		"2006-01-02T15:04:05", "2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", v)
}

// DoctorReport 返回一份人可读的环境检查结果。
func DoctorReport() string {
	return printPermReportFor(newScreen())
}

// OCRIndex 暴露存储层的 OCR 索引，供摘要层取原文。
func (r *Runner) OCRIndex(dateKey string) (map[string]string, error) {
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	return st.OCRIndex(dateKey)
}

// Snapshots 读回某天的原始采样。
func (r *Runner) Snapshots(dateKey string) ([]Snapshot, error) {
	r.mu.Lock()
	st := r.st
	r.mu.Unlock()
	return st.ReadDay(dateKey)
}

// Root 返回数据根目录，导出 skill 时要用。
func (r *Runner) Root() string { r.mu.Lock(); defer r.mu.Unlock(); return r.st.root }
