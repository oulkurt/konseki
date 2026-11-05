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
	}{{"idle", EndIdle, func(o *Observation) { o.Now = o.Now.Add(2 * time.Second) }}, {"lock", EndLock, func(o *Observation) { o.Now = o.Now.Add(time.Second); o.Locked = true }}, {"suspend", EndSuspend, func(o *Observation) { o.Now = o.Now.Add(6 * time.Second) }}, {"stop", EndStop, func(o *Observation) { o.Stop = true }}} {
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
