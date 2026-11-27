/*
 * [INPUT]: 依赖 Runner 状态、状态机动作、平台采集与存储句柄。
 * [OUTPUT]: 提供采集工作流、会话关闭落盘与快照持久化实现。
 * [POS]: Runner 状态推进与异步抓帧边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

type captureOutcome uint8

const (
	outcomeSaved captureOutcome = iota
	outcomeDuplicate
	outcomeBlank
	outcomeSensitive
	outcomeAborted
	outcomeFailed
)

var errBlankScreen = errors.New("截图全黑，屏幕录制授权后需要重启 Konseki 才能生效")

type closedSession struct {
	session  *Session
	store    *Store
	storage  *Storage
	snapshot countBucket
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
	outcome     captureOutcome
	frame       *Frame
	fingerprint *fingerprint
	title       string
	tmp         string
	err         error
}

func (r *Runner) grab(job captureJob) captureResult {
	result := captureResult{job: job, outcome: outcomeAborted}
	validFront := func() bool {
		f := r.deps.front.Front()
		return !r.deps.lock.Locked() && f.Bundle == job.front.Bundle && f.PID == job.front.PID
	}
	if !validFront() {
		result.outcome = outcomeAborted
		return result
	}
	if r.liveSensitive() {
		result.outcome = outcomeSensitive
		return result
	}
	f := r.deps.screen.Grab()
	result.frame = f
	if !validFront() {
		releaseFrame(f)
		result.frame = nil
		result.outcome = outcomeAborted
		return result
	}
	if r.liveSensitive() {
		releaseFrame(f)
		result.frame = nil
		result.outcome = outcomeSensitive
		return result
	}
	if f == nil {
		result.outcome = outcomeFailed
		result.err = errors.New("抓取屏幕失败")
		return result
	}
	if f.Blank {
		result.outcome = outcomeBlank
		return result
	}
	if !job.probe {
		_, result.title, _ = r.deps.screen.FrontWindow()
		if !validFront() {
			releaseFrame(f)
			result.frame = nil
			result.outcome = outcomeAborted
			return result
		}
	}
	fp := fingerprintFrame(f)
	if fp == nil {
		result.outcome = outcomeFailed
		result.err = errors.New("屏幕帧无效")
		return result
	}
	result.frame = f
	result.fingerprint = fp
	if duplicate(job.baseline, fp) {
		result.outcome = outcomeDuplicate
		return result
	}
	if job.probe {
		result.outcome = outcomeSaved
		return result
	}
	dir := filepath.Join(job.store.root, "shots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		result.outcome = outcomeFailed
		result.err = err
		return result
	}
	file, err := os.CreateTemp(dir, ".capture-*.jpg")
	if err != nil {
		result.outcome = outcomeFailed
		result.err = err
		return result
	}
	result.tmp = file.Name()
	if err = file.Close(); err != nil {
		result.outcome = outcomeFailed
		result.err = err
		_ = os.Remove(result.tmp)
		result.tmp = ""
		return result
	}
	if !r.deps.save(f, result.tmp, job.quality) {
		result.outcome = outcomeFailed
		result.err = errors.New("编码截图失败")
		_ = os.Remove(result.tmp)
		result.tmp = ""
		return result
	}
	result.outcome = outcomeSaved
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
	finish := func(res captureResult, now time.Time, stopping bool) {
		inFlight = false
		defer releaseFrame(res.frame)
		if stopping {
			if res.tmp != "" {
				_ = os.Remove(res.tmp)
				res.tmp = ""
			}
			r.mu.Lock()
			r.flightSession = ""
			r.flightProbe = false
			r.finishClosed(res.job.session)
			r.machine.abandonJob()
			r.mu.Unlock()
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.flightSession = ""
		r.flightProbe = false
		if res.job.generation != r.generation {
			if res.tmp != "" {
				tmp := res.tmp
				res.tmp = ""
				r.mu.Unlock()
				_ = os.Remove(tmp)
				r.mu.Lock()
			}
			r.finishClosed(res.job.session)
			r.machine.abandonJob()
			o := Observation{Now: now, Front: front, LiveSensitive: liveSensitive, Locked: locked, ScreenPermission: r.perms.Screen, PausedUntil: r.pausedUntil, SinceKey: now.Sub(r.machine.lastKey), SinceClick: now.Sub(r.machine.lastClick), SinceScroll: now.Sub(r.machine.lastScroll)}
			r.perform(r.machine.Step(o, r.cfg), now, front, results, &inFlight)
			return
		}
		if res.outcome == outcomeSensitive {
			liveSensitive = true
		}
		o := Observation{Now: now, Front: front, LiveSensitive: liveSensitive, Locked: locked, ScreenPermission: r.perms.Screen, PausedUntil: r.pausedUntil, SinceKey: now.Sub(r.machine.lastKey), SinceClick: now.Sub(r.machine.lastClick), SinceScroll: now.Sub(r.machine.lastScroll)}
		switch res.outcome {
		case outcomeBlank:
			r.blankFrames++
			if r.blankFrames >= 3 {
				r.lastErr = errBlankScreen
			}
		case outcomeSaved, outcomeDuplicate:
			r.blankFrames = 0
			if r.lastErr == errBlankScreen {
				r.lastErr = nil
			}
		}
		if res.job.probe {
			if res.err != nil {
				r.lastErr = res.err
			}
			if res.outcome == outcomeAborted || res.outcome == outcomeSensitive {
				r.machine.abandonProbe(now)
			} else {
				o.ProbeDone = true
				o.ProbeDuplicate = res.outcome == outcomeDuplicate || res.outcome == outcomeBlank || res.outcome == outcomeFailed
			}
			r.finishClosed(res.job.session)
			r.perform(r.machine.Step(o, r.cfg), now, front, results, &inFlight)
			return
		}
		o.CaptureDone = true
		o.CaptureFailed = res.outcome == outcomeFailed || res.err != nil
		o.CaptureSkipped = res.outcome == outcomeBlank
		if res.err != nil {
			r.lastErr = res.err
		}
		if (res.outcome == outcomeSaved || res.outcome == outcomeDuplicate) && res.err == nil && !locked && r.machine.Phase != PhaseBlocked && r.machine.Phase != PhasePaused && !systemFront(front) && !sensitiveFront(front, r.cfg.SensitiveApps) && (r.pausedUntil.IsZero() || !now.Before(r.pausedUntil)) {
			if res.outcome == outcomeSaved || res.job.trigger == TriggerHeartbeat {
				saved, err := r.persist(&res)
				if err != nil {
					r.lastErr = err
					o.CaptureFailed = true
				} else if saved {
					o.CaptureSaved = true
				}
			}
		}
		if res.tmp != "" {
			tmp := res.tmp
			res.tmp = ""
			r.mu.Unlock()
			_ = os.Remove(tmp)
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
			}
			r.mu.Lock()
			stopNow := r.deps.clock.Now()
			r.perform(r.machine.Step(Observation{Now: stopNow, Stop: true}, r.cfg), stopNow, front, results, &inFlight)
			q := r.queue
			r.queue = nil
			r.mu.Unlock()
			q.close()
			return
		case res := <-results:
			finish(res, r.deps.clock.Now(), false)
		case <-ticks:
			r.mu.Lock()
			enabled, version := r.cfg.InputCounts, r.countsVersion
			permsVersion := r.permsVersion
			r.mu.Unlock()
			var totals InputTotals
			var totalsOK bool
			if enabled && r.deps.totals != nil {
				totals, totalsOK = r.deps.totals.Totals()
			}
			key, click, scroll := r.deps.input.Intervals()
			now := r.deps.clock.Now()
			var refreshedPerms bool
			var freshPerms Perms
			if lastPermissions.IsZero() || now.Sub(lastPermissions) >= 5*time.Second {
				freshPerms = Perms{Screen: r.deps.screen.Permission(false), Accessibility: r.deps.perms.Accessibility()}
				lastPermissions = now
				refreshedPerms = true
			}
			front = r.deps.front.Front()
			liveSensitive = r.liveSensitive()
			if lastSystem.IsZero() || now.Sub(lastSystem) >= time.Second {
				locked = r.deps.lock.Locked()
				lastSystem = now
			}
			r.mu.Lock()
			if version != r.countsVersion || !r.cfg.InputCounts {
				totalsOK = false
			}
			enabled = r.cfg.InputCounts
			if !enabled || !totalsOK {
				r.counts.invalidate()
			}
			r.front = front
			if refreshedPerms && r.permsVersion == permsVersion {
				r.perms = freshPerms
			}
			perms := r.perms
			if !r.pausedUntil.IsZero() && !now.Before(r.pausedUntil) {
				r.pausedUntil = time.Time{}
			}
			o := Observation{Now: now, SinceKey: key, SinceClick: click, SinceScroll: scroll, Front: front, LiveSensitive: liveSensitive, Locked: locked, ScreenPermission: perms.Screen, PausedUntil: r.pausedUntil, PreviousTick: previousTick, Slept: !previousTick.IsZero() && now.Round(0).Sub(previousTick.Round(0)) > 5*time.Second}
			previousTick = now
			r.perform(r.machine.Step(o, r.cfg), now, front, results, &inFlight)
			if o.Slept {
				r.counts.invalidate()
			}
			enabled = r.cfg.InputCounts
			if !enabled || version != r.countsVersion {
				totalsOK = false
			}
			private := locked || liveSensitive || systemFront(front) || sensitiveFront(front, r.cfg.SensitiveApps) || (!r.pausedUntil.IsZero() && now.Before(r.pausedUntil))
			r.counts.observe(enabled, totalsOK, totals, private, r.session != nil && r.machine.Phase == PhaseActive && r.machine.Block == BlockNone)
			r.mu.Unlock()
		}
	}
}

func (r *Runner) ensureTargetRootLocked() bool {
	for {
		target := r.cfg.Root
		absTarget, _ := filepath.Abs(target)
		if r.st.root == absTarget {
			return true
		}
		runGen := r.runGen
		r.mu.Unlock()
		st, err := NewStore(target)
		r.mu.Lock()
		if err != nil {
			r.lastErr = err
			return false
		}
		curAbs, _ := filepath.Abs(r.cfg.Root)
		if curAbs != st.root {
			continue
		}
		r.st = st
		storage := NewStorage(target)
		r.storage = storage
		r.lastErr = nil
		r.baseline = nil
		r.generation++
		r.lastPath = ""
		r.lastW, r.lastH = 0, 0
		r.mu.Unlock()
		r.spawnBG(func() {
			r.setErrorForRun(runGen, storage.Rescan())
		})
		r.mu.Lock()
		return true
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
			if !r.ensureTargetRootLocked() {
				r.machine.abortSessionOpen()
				r.session = nil
				return
			}
			r.session = &Session{ID: r.st.NewSnapshotID(now), Start: now}
			r.counts.open(r.cfg.InputCounts)
		case ActionCloseSession:
			if r.session != nil {
				s := r.session
				r.session = nil
				s.Keys, s.Clicks = r.counts.session.values()
				s.End = a.At
				s.EndReason = a.Reason
				c := closedSession{session: s, store: r.st, storage: r.storage, snapshot: r.counts.snapshot}
				r.counts.snapshot = countBucket{}
				r.counts.session = countBucket{}
				if *inFlight && !r.flightProbe && r.flightSession == s.ID {
					r.closed[s.ID] = c
				} else {
					r.writeSession(c)
				}
			}
		case ActionCapture, ActionProbe:
			if r.session == nil && !r.ensureTargetRootLocked() {
				r.machine.abortSessionOpen()
				return
			}
			session := ""
			if r.session != nil {
				session = r.session.ID
			}
			job := captureJob{trigger: a.Trigger, at: now, front: front, session: session, store: r.st, baseline: r.baseline, generation: r.generation, storage: r.storage, quality: r.cfg.JPEGQuality, ocr: r.cfg.OCR, probe: a.Kind == ActionProbe}
			*inFlight = true
			r.flightSession = session
			r.flightProbe = a.Kind == ActionProbe
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

func (r *Runner) persist(res *captureResult) (bool, error) {
	job := res.job
	dup := res.outcome == outcomeDuplicate
	path := r.lastPath
	if dup && path == "" {
		return false, nil
	}
	id := job.store.NewSnapshotID(job.at)
	size := int64(0)
	var counts countBucket
	countSession := r.session != nil && r.session.ID == job.session
	if countSession {
		counts = r.counts.takeSnapshot(r.cfg.InputCounts)
	} else if c, ok := r.closed[job.session]; ok {
		counts = c.snapshot
	}
	keys, clicks := counts.values()
	w, h := res.frame.W, res.frame.H
	if dup {
		w, h = r.lastW, r.lastH
	}
	tmpPath := res.tmp
	r.mu.Unlock()
	err := func() error {
		if !dup {
			dest := job.store.ShotFile(id)
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			if err := os.Rename(tmpPath, dest); err != nil {
				return err
			}
			res.tmp = ""
			info, err := os.Stat(dest)
			if err != nil {
				_ = os.Remove(dest)
				return err
			}
			size = info.Size()
			path, err = filepath.Rel(job.store.root, dest)
			if err != nil {
				_ = os.Remove(dest)
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
		}
		snap := Snapshot{ID: id, TS: job.at.Format(time.RFC3339Nano), Session: job.session, Trigger: job.trigger, App: job.front.App, Bundle: job.front.Bundle, Title: res.title, PID: job.front.PID, Shot: path, Dup: dup, W: w, H: h, Bytes: size, Keys: keys, Clicks: clicks, ImgKB: int(size / 1024)}
		appendSnapshot := r.deps.appendSnapshot
		if appendSnapshot == nil {
			appendSnapshot = (*Store).Append
		}
		if err := appendSnapshot(job.store, snap); err != nil {
			if !dup {
				_ = os.Remove(job.store.ShotFile(id))
			}
			return err
		}
		return nil
	}()
	r.mu.Lock()
	if err != nil {
		if countSession && r.session != nil && r.session.ID == job.session {
			r.counts.restoreSnapshot(counts)
		}
		return false, err
	}
	if !dup {
		r.baseline = res.fingerprint
		r.lastPath = path
		r.lastW, r.lastH = w, h
		job.storage.Account(size)
		if r.cfg.MaxStorage > 0 && job.storage.Usage().Bytes > r.cfg.MaxStorage {
			r.requestEnforce()
		}
	}
	job.storage.CountSnapshot()
	r.lastShot = job.at
	r.lastErr = nil
	session := r.session
	if session == nil || session.ID != job.session {
		if c, ok := r.closed[job.session]; ok {
			session = c.session
			c.snapshot = countBucket{}
			r.closed[job.session] = c
		}
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
	if !dup && r.queue != nil {
		r.queue.submit(job.session, job.ocr, ocrTask{frame: res.frame, id: id, day: job.at.In(time.Local).Format("2006-01-02"), store: job.store})
	}
	return true, nil
}
