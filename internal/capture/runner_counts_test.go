/*
 * [INPUT]: 依赖假时钟、累计读数、模拟画面和临时存储。
 * [OUTPUT]: 验证采集器的键鼠计数调用、会话与写盘回补。
 * [POS]: 采集器与纯计数逻辑之间的集成测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeTotals struct {
	mu    sync.Mutex
	value InputTotals
	ok    bool
	calls int
}

func (f *fakeTotals) Totals() (InputTotals, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.value, f.ok
}
func (f *fakeTotals) set(v InputTotals) { f.mu.Lock(); defer f.mu.Unlock(); f.value = v }
func (f *fakeTotals) reads() int        { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func TestRunnerCountsEnabledAndSession(t *testing.T) {
	d, clock, sys := fakeDependencies()
	source := &fakeTotals{ok: true}
	d.totals = source
	cfg := Settings{Root: t.TempDir(), Idle: time.Minute, Heartbeat: time.Hour, InputCounts: false}
	r, err := newRunner(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	clock.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	if source.reads() != 0 {
		t.Fatal("关闭时读取计数")
	}
	cfg.InputCounts = true
	r.Apply(cfg)
	clock.advance(250 * time.Millisecond)
	eventually(t, func() bool { return source.reads() == 1 })
	source.set(InputTotals{Keys: 3, Clicks: 1})
	sys.mu.Lock()
	sys.key = clock.Now()
	sys.mu.Unlock()
	clock.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Session != nil })
	r.mu.Lock()
	keys, clicks := r.counts.session.values()
	r.mu.Unlock()
	if keys == nil || *keys != 3 || clicks == nil || *clicks != 1 {
		t.Fatalf("打开会话的输入未计入: %v %v", keys, clicks)
	}
	if r.Status().Session.Keys != nil {
		t.Fatal("打开中的会话暴露计数")
	}
	r.Stop()
	rows := r.RecentSessions(1)
	if len(rows) != 1 || rows[0].Keys == nil || *rows[0].Keys != 3 || *rows[0].Clicks != 1 {
		t.Fatalf("会话计数: %+v", rows)
	}
}

func TestRunnerCountsPersistRestoreAndClosed(t *testing.T) {
	d, clock, _ := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), InputCounts: true}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.session = &Session{ID: "s"}
	r.counts.have = true
	r.counts.snapshot = countBucket{keys: 4, clicks: 2, valid: true}
	r.lastPath = "old.jpg"
	r.lastW, r.lastH = 1, 1
	res := captureResult{job: captureJob{session: "s", store: r.st, storage: r.storage, at: clock.Now()}, frame: &Frame{W: 1, H: 1}, duplicate: true}
	errWrite := errors.New("写入失败")
	r.deps.appendSnapshot = func(*Store, Snapshot) error { return errWrite }
	r.mu.Lock()
	err = r.persist(res)
	r.mu.Unlock()
	if !errors.Is(err, errWrite) || r.counts.snapshot.keys != 4 {
		t.Fatalf("失败未回补: %v %+v", err, r.counts.snapshot)
	}
	r.lastPath = ""
	r.mu.Lock()
	err = r.persist(res)
	r.mu.Unlock()
	if err != nil || r.counts.snapshot.keys != 4 {
		t.Fatal("跳过重复帧未回补")
	}
	r.lastPath = "old.jpg"
	var got Snapshot
	r.deps.appendSnapshot = func(_ *Store, s Snapshot) error { got = s; return nil }
	r.mu.Lock()
	err = r.persist(res)
	r.mu.Unlock()
	if err != nil || got.Keys == nil || *got.Keys != 4 || *got.Clicks != 2 {
		t.Fatalf("快照计数: %+v %v", got, err)
	}
	r.session = nil
	r.mu.Lock()
	err = r.persist(res)
	r.mu.Unlock()
	if err != nil || got.Keys != nil || got.Clicks != nil {
		t.Fatalf("关闭会话的快照带有计数: %+v", got)
	}
}

func TestRunnerCountsPrivacyAndToggle(t *testing.T) {
	d, clock, sys := fakeDependencies()
	source := &fakeTotals{ok: true}
	d.totals = source
	cfg := Settings{Root: t.TempDir(), Idle: time.Minute, Heartbeat: time.Hour, InputCounts: true, SensitiveApps: []string{"secret"}}
	r, err := newRunner(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	tick := func(keys uint32, secure bool) {
		t.Helper()
		source.set(InputTotals{Keys: keys, Clicks: keys, Secure: secure})
		clock.advance(250 * time.Millisecond)
		eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.counts.have && r.counts.last.Keys == keys })
	}
	tick(10, false)
	sys.mu.Lock()
	sys.key = clock.Now()
	sys.mu.Unlock()
	tick(12, false)
	if r.Status().Session == nil {
		t.Fatal("会话未开启")
	}
	sys.mu.Lock()
	sys.live = Front{Bundle: "secret"}
	sys.mu.Unlock()
	tick(20, false)
	sys.mu.Lock()
	sys.live = Front{}
	sys.mu.Unlock()
	tick(30, false)
	tick(35, false)
	tick(40, true)
	tick(45, false)
	tick(46, false)
	r.Pause(clock.Now().Add(2 * time.Second))
	tick(50, false)
	r.Resume()
	tick(60, false)
	tick(61, false)
	// 两拍之间暂停再恢复，也要丢弃跨越暂停的增量。
	r.Pause(clock.Now().Add(time.Second))
	r.Resume()
	tick(66, false)
	tick(67, false)
	r.mu.Lock()
	got := r.counts.session
	r.mu.Unlock()
	if got.keys != 10 || got.clicks != 20 || !got.valid {
		t.Fatalf("隐私状态计数: %+v", got)
	}
	cfg.InputCounts = false
	r.Apply(cfg)
	before := source.reads()
	clock.advance(250 * time.Millisecond)
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return !r.counts.have })
	if source.reads() != before {
		t.Fatal("关闭后调用了平台")
	}
	cfg.InputCounts = true
	r.Apply(cfg)
	tick(70, false)
	r.mu.Lock()
	valid := r.counts.session.valid
	r.mu.Unlock()
	if valid {
		t.Fatal("开关中断的会话不应输出计数")
	}
}

func TestRunnerCountsClearAndRecovery(t *testing.T) {
	d, clock, _ := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir(), InputCounts: true}, d)
	if err != nil {
		t.Fatal(err)
	}
	now := clock.Now()
	snap := Snapshot{TS: now.Format(time.RFC3339Nano), Session: "crash", Keys: new(7), Clicks: new(2)}
	if err := r.st.Append(snap); err != nil {
		t.Fatal(err)
	}
	if err := recoverSession(r.st, r.storage, now); err != nil {
		t.Fatal(err)
	}
	sessions := r.RecentSessions(1)
	if len(sessions) != 1 || sessions[0].Keys != nil || sessions[0].Clicks != nil {
		t.Fatalf("恢复会话带有计数: %+v", sessions)
	}
	r.counts = inputCounts{have: true, snapshot: countBucket{keys: 9, valid: true}, session: countBucket{clicks: 4, valid: true}}
	if _, err := r.ClearAll(); err != nil {
		t.Fatal(err)
	}
	if r.counts != (inputCounts{}) {
		t.Fatalf("清空后计数残留: %+v", r.counts)
	}
}

func TestRunnerStatusCopiesCountPointers(t *testing.T) {
	d, _, _ := fakeDependencies()
	r, err := newRunner(Settings{Root: t.TempDir()}, d)
	if err != nil {
		t.Fatal(err)
	}
	r.session = &Session{Keys: new(3), Clicks: new(2)}
	snapshot := r.Status().Session
	*snapshot.Keys = 99
	*snapshot.Clicks = 99
	if *r.session.Keys != 3 || *r.session.Clicks != 2 {
		t.Fatal("状态副本共享计数指针")
	}
}
