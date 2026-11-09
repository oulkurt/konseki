/*
 * [INPUT]: 依赖人工推进的观测时间与采集设置。
 * [OUTPUT]: 验证触发、会话、阻塞和调度决策。
 * [POS]: 纯状态机的行为测试，不调用平台接口。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"testing"
	"time"
)

func TestMachineTriggersAndDebounce(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Settings{Idle: time.Minute, Debounce: time.Second, TypingPause: 500 * time.Millisecond, Heartbeat: time.Hour}
	m := Machine{}
	o := Observation{Now: now, SinceKey: time.Hour, SinceClick: time.Hour, SinceScroll: time.Hour, Front: Front{Bundle: "a"}, ScreenPermission: PermGranted}
	m.Step(o, s)
	o.Now = now.Add(time.Second)
	o.Front.Bundle = "b"
	o.SinceKey = time.Hour + time.Second
	o.SinceClick = time.Hour + time.Second
	o.SinceScroll = time.Hour + time.Second
	acts := m.Step(o, s)
	requireAction(t, acts, ActionOpenSession, "")
	requireAction(t, acts, ActionCapture, TriggerAppFocus)
	o.Now = o.Now.Add(250 * time.Millisecond)
	o.SinceClick = 0
	o.SinceKey += 250 * time.Millisecond
	o.SinceScroll += 250 * time.Millisecond
	if a := m.Step(o, s); hasAction(a, ActionCapture, "") {
		t.Fatal(a)
	}
	o.Now = now.Add(2 * time.Second)
	o.SinceClick = time.Second - 250*time.Millisecond
	o.SinceKey = time.Hour + 2*time.Second
	o.SinceScroll = o.SinceKey
	o.CaptureDone = true
	o.CaptureSaved = true
	requireAction(t, m.Step(o, s), ActionCapture, TriggerClick)
}
func TestMachineSessionClosures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason EndReason
		change func(*Observation)
	}{{"idle", EndIdle, func(o *Observation) { o.Now = o.Now.Add(2 * time.Second) }}, {"lock", EndLock, func(o *Observation) { o.Now = o.Now.Add(time.Second); o.Locked = true }}, {"suspend", EndSuspend, func(o *Observation) { o.PreviousTick = o.Now; o.Now = o.Now.Add(8 * time.Hour); o.Slept = true }}, {"stop", EndStop, func(o *Observation) { o.Stop = true }}} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			s := Settings{Idle: time.Second}
			m := Machine{}
			o := Observation{Now: now, SinceKey: time.Hour, SinceClick: time.Hour, SinceScroll: time.Hour, Front: Front{Bundle: "a"}, ScreenPermission: PermGranted}
			m.Step(o, s)
			o.Now = now.Add(time.Millisecond * 250)
			o.SinceScroll = 0
			m.Step(o, s)
			tc.change(&o)
			o.SinceKey = time.Hour + o.Now.Sub(now)
			o.SinceClick = o.SinceKey
			o.SinceScroll = o.Now.Sub(now.Add(250 * time.Millisecond))
			a := m.Step(o, s)
			found := false
			for _, x := range a {
				if x.Kind == ActionCloseSession && x.Reason == tc.reason {
					found = true
				}
			}
			if !found {
				t.Fatal(a)
			}
		})
	}
}
func TestMachineLockedWithoutSession(t *testing.T) {
	m := Machine{}
	a := m.Step(Observation{Now: time.Now(), Locked: true}, Settings{})
	if hasAction(a, ActionOpenSession, "") || hasAction(a, ActionCloseSession, "") {
		t.Fatal(a)
	}
}
func hasAction(a []Action, k ActionKind, tr Trigger) bool {
	for _, x := range a {
		if x.Kind == k && (tr == "" || tr == x.Trigger) {
			return true
		}
	}
	return false
}
func requireAction(t *testing.T, a []Action, k ActionKind, tr Trigger) {
	t.Helper()
	if !hasAction(a, k, tr) {
		t.Fatalf("缺少动作 %v %s: %+v", k, tr, a)
	}
}

type machineScenario struct {
	m                  Machine
	s                  Settings
	o                  Observation
	key, click, scroll time.Time
}

func newMachineScenario() *machineScenario {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	x := &machineScenario{s: Settings{Idle: 10 * time.Second, TypingPause: time.Second, Debounce: time.Second, Heartbeat: time.Hour}, o: Observation{Now: now, Front: Front{Bundle: "a"}, ScreenPermission: PermGranted}, key: now.Add(-time.Hour), click: now.Add(-time.Hour), scroll: now.Add(-time.Hour)}
	x.step(0)
	return x
}
func (x *machineScenario) step(d time.Duration) []Action {
	x.o.Now = x.o.Now.Add(d)
	x.o.SinceKey = x.o.Now.Sub(x.key)
	x.o.SinceClick = x.o.Now.Sub(x.click)
	x.o.SinceScroll = x.o.Now.Sub(x.scroll)
	a := x.m.Step(x.o, x.s)
	x.o.CaptureDone = false
	x.o.CaptureFailed = false
	x.o.CaptureSaved = false
	x.o.ProbeDone = false
	return a
}
func TestMachineTypingPauseAndClick(t *testing.T) {
	x := newMachineScenario()
	x.key = x.o.Now.Add(250 * time.Millisecond)
	requireAction(t, x.step(250*time.Millisecond), ActionOpenSession, "")
	if a := x.step(750 * time.Millisecond); hasAction(a, ActionCapture, "") {
		t.Fatal(a)
	}
	requireAction(t, x.step(250*time.Millisecond), ActionCapture, TriggerTypingPause)
	x.o.CaptureDone = true
	x.o.CaptureSaved = true
	x.step(0)
	x.click = x.o.Now.Add(250 * time.Millisecond)
	if a := x.step(250 * time.Millisecond); hasAction(a, ActionCapture, "") {
		t.Fatal(a)
	}
	requireAction(t, x.step(750*time.Millisecond), ActionCapture, TriggerClick)
}
func TestMachineHeartbeatWithoutSession(t *testing.T) {
	x := newMachineScenario()
	x.s.Heartbeat = time.Second
	a := x.step(time.Second)
	requireAction(t, a, ActionCapture, TriggerHeartbeat)
	if hasAction(a, ActionOpenSession, "") {
		t.Fatal(a)
	}
}
func TestMachineProbeBackoffAndReset(t *testing.T) {
	x := newMachineScenario()
	x.s.ScreenPoll = time.Second
	requireAction(t, x.step(time.Second), ActionProbe, "")
	x.o.ProbeDone = true
	x.o.ProbeDuplicate = true
	x.step(0)
	if a := x.step(time.Second); hasAction(a, ActionProbe, "") {
		t.Fatal(a)
	}
	requireAction(t, x.step(time.Second), ActionProbe, "")
	x.o.ProbeDone = true
	x.o.ProbeDuplicate = false
	requireAction(t, x.step(0), ActionCapture, TriggerVisual)
	x.o.CaptureDone = true
	x.o.CaptureSaved = true
	x.step(0)
	requireAction(t, x.step(time.Second), ActionProbe, "")
}
func TestMachineProbeBackoffMaximumAndInputReset(t *testing.T) {
	x := newMachineScenario()
	x.s.ScreenPoll = time.Second
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		requireAction(t, x.step(delay), ActionProbe, "")
		x.o.ProbeDone = true
		x.o.ProbeDuplicate = true
		x.step(0)
	}
	if x.m.probeFactor != 8 {
		t.Fatal(x.m.probeFactor)
	}
	x.scroll = x.o.Now.Add(time.Second)
	x.step(time.Second)
	if x.m.probeFactor != 1 {
		t.Fatal(x.m.probeFactor)
	}
}
func TestMachineBlockedAndResume(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*machineScenario)
		phase  Phase
		block  BlockReason
	}{{"sensitive", func(x *machineScenario) {
		x.s.SensitiveApps = []string{"COM.EXAMPLE.SECRET"}
		x.o.Front.Bundle = "com.example.secret"
	}, PhaseBlocked, BlockSensitiveApp}, {"permission", func(x *machineScenario) { x.o.ScreenPermission = PermDenied }, PhaseBlocked, BlockNoScreenPermission}, {"locked", func(x *machineScenario) { x.o.Locked = true }, PhaseBlocked, BlockScreenLocked}, {"pause", func(x *machineScenario) { x.o.PausedUntil = x.o.Now.Add(time.Second) }, PhasePaused, BlockNone}} {
		t.Run(tc.name, func(t *testing.T) {
			x := newMachineScenario()
			x.s.Heartbeat = time.Millisecond
			x.s.ScreenPoll = time.Millisecond
			tc.change(x)
			a := x.step(250 * time.Millisecond)
			if x.m.Phase != tc.phase || x.m.Block != tc.block || hasAction(a, ActionCapture, "") || hasAction(a, ActionProbe, "") {
				t.Fatal(x.m, a)
			}
			if tc.name == "pause" {
				x.step(time.Second)
				if x.m.Phase != PhaseWaiting {
					t.Fatal(x.m.Phase)
				}
			}
			if tc.name == "locked" {
				x.o.Locked = false
				requireAction(t, x.step(time.Second), ActionResetBaseline, "")
			}
		})
	}
}
func TestMachinePendingPriorityAndSingleFlight(t *testing.T) {
	x := newMachineScenario()
	x.click = x.o.Now.Add(250 * time.Millisecond)
	requireAction(t, x.step(250*time.Millisecond), ActionCapture, TriggerClick)
	x.o.Front.Bundle = "b"
	a := x.step(250 * time.Millisecond)
	if hasAction(a, ActionCapture, "") {
		t.Fatal(a)
	}
	x.o.CaptureDone = true
	x.o.CaptureSaved = true
	if a = x.step(250 * time.Millisecond); hasAction(a, ActionCapture, "") {
		t.Fatal(a)
	}
	requireAction(t, x.step(500*time.Millisecond), ActionCapture, TriggerAppFocus)
}
func TestMachineFailureBackoff(t *testing.T) {
	x := newMachineScenario()
	x.s.Debounce = 0
	x.click = x.o.Now.Add(250 * time.Millisecond)
	requireAction(t, x.step(250*time.Millisecond), ActionCapture, TriggerClick)
	for _, delay := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second} {
		x.o.CaptureDone = true
		x.o.CaptureFailed = true
		x.step(0)
		if a := x.step(delay - time.Millisecond); hasAction(a, ActionCapture, "") {
			t.Fatal(a)
		}
		requireAction(t, x.step(time.Millisecond), ActionCapture, TriggerClick)
	}
	x.o.CaptureDone = true
	x.o.CaptureSaved = true
	x.step(0)
	if x.m.failures != 0 || !x.m.retryAt.IsZero() {
		t.Fatal(x.m.failures, x.m.retryAt)
	}
	x.m.failures = 15
	x.o.CaptureDone = true
	x.o.CaptureFailed = true
	x.step(0)
	if x.m.failures != 16 || x.m.retryAt.Sub(x.o.Now) != 30*time.Second {
		t.Fatal(x.m.failures, x.m.retryAt)
	}
}
func TestMachineIdleAppliesNextSession(t *testing.T) {
	x := newMachineScenario()
	x.s.Idle = 2 * time.Second
	x.scroll = x.o.Now.Add(250 * time.Millisecond)
	x.step(250 * time.Millisecond)
	x.s.Idle = 10 * time.Second
	requireAction(t, x.step(3*time.Second), ActionCloseSession, "")
	x.scroll = x.o.Now.Add(250 * time.Millisecond)
	requireAction(t, x.step(250*time.Millisecond), ActionOpenSession, "")
	if a := x.step(3 * time.Second); hasAction(a, ActionCloseSession, "") {
		t.Fatal(a)
	}
}

func TestMachineAllTriggerPriorities(t *testing.T) {
	order := []Trigger{TriggerAppFocus, TriggerClick, TriggerTypingPause, TriggerVisual, TriggerHeartbeat}
	for i, want := range order {
		t.Run(string(want), func(t *testing.T) {
			m := Machine{}
			for j := len(order) - 1; j >= i; j-- {
				m.queue(order[j])
			}
			if m.pending != want {
				t.Fatal(m.pending, want)
			}
		})
	}
}
func TestMachineIdleWhileBlocked(t *testing.T) {
	x := newMachineScenario()
	x.s.Idle = time.Second
	x.scroll = x.o.Now.Add(250 * time.Millisecond)
	x.step(250 * time.Millisecond)
	x.s.SensitiveApps = []string{"secret"}
	x.o.Front.Bundle = "secret"
	x.step(250 * time.Millisecond)
	a := x.step(time.Second)
	requireAction(t, a, ActionCloseSession, "")
	if x.m.Phase != PhaseBlocked || x.m.Block != BlockSensitiveApp {
		t.Fatal(x.m)
	}
}

func TestMachineSuspendEndsAtPreviousTick(t *testing.T) {
	// macOS 的单调钟在睡眠中不走，必须用墙钟差值判断。
	before := time.Date(2026, 11, 5, 23, 0, 0, 0, time.Local)
	after := before.Add(8 * time.Hour)
	m := Machine{}
	cfg := Settings{Idle: 24 * time.Hour}
	o := Observation{Now: before.Add(-time.Second), SinceKey: time.Hour, SinceClick: time.Hour, SinceScroll: time.Hour, Front: Front{Bundle: "a"}}
	m.Step(o, cfg)
	o.Now = before
	o.SinceClick = 0
	m.Step(o, cfg)
	o.Now = after
	o.SinceClick = 8 * time.Hour
	o.SinceKey += 8 * time.Hour
	o.SinceScroll += 8 * time.Hour
	o.PreviousTick = before
	o.Slept = after.Round(0).Sub(before.Round(0)) > 5*time.Second
	for _, a := range m.Step(o, cfg) {
		if a.Kind == ActionCloseSession && a.Reason == EndSuspend && a.At.Equal(before) {
			return
		}
	}
	t.Fatal("睡眠关闭时间不正确")
}

func TestMachineBlankAdvancesHeartbeatAndBackoff(t *testing.T) {
	x := newMachineScenario()
	x.s.Heartbeat = 4 * time.Second
	x.s.ScreenPoll = time.Second
	x.step(0)
	requireAction(t, x.step(4*time.Second), ActionCapture, TriggerHeartbeat)
	x.o.CaptureDone, x.o.CaptureSkipped = true, true
	x.step(0)
	x.o.CaptureDone, x.o.CaptureSkipped = false, false
	if a := x.step(time.Second); hasAction(a, ActionCapture, "") || hasAction(a, ActionProbe, "") {
		t.Fatal(a)
	}
	requireAction(t, x.step(time.Second), ActionProbe, "")
}

func TestMachineSystemFrontDoesNotCountOrCapture(t *testing.T) {
	for _, front := range []Front{{Bundle: "com.apple.loginwindow"}, {Bundle: "com.apple.ScreenSaver.Engine"}, {App: "ScreenSaverEngine"}} {
		x := newMachineScenario()
		x.s.Heartbeat = time.Second
		x.o.Front = Front{Bundle: "a"}
		x.step(0)
		x.o.Front = front
		x.o.SinceClick = 0
		actions := x.step(2 * time.Second)
		if hasAction(actions, ActionOpenSession, "") || hasAction(actions, ActionCapture, "") || hasAction(actions, ActionProbe, "") {
			t.Fatalf("%+v: %+v", front, actions)
		}
		x.o.Front = Front{Bundle: "a"}
		x.o.SinceClick = 250 * time.Millisecond
		actions = x.step(250 * time.Millisecond)
		if hasAction(actions, ActionOpenSession, "") {
			t.Fatalf("系统界面输入被延迟计数: %+v", actions)
		}
	}
}
