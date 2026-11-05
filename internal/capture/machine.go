/*
 * [INPUT]: 依赖观测时间、系统状态与采集设置。
 * [OUTPUT]: 提供纯状态机和采集、探测、会话动作。
 * [POS]: 采集决策边界；外壳执行动作并反馈结果。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"strings"
	"time"
)

type Observation struct {
	Now                               time.Time
	SinceKey, SinceClick, SinceScroll time.Duration
	Front                             Front
	Locked                            bool
	ScreenPermission                  PermState
	PausedUntil                       time.Time
	Stop                              bool
	CaptureDone                       bool
	CaptureFailed                     bool
	CaptureSaved                      bool
	ProbeDone                         bool
	ProbeDuplicate                    bool
}

type ActionKind uint8

const (
	ActionOpenSession ActionKind = iota
	ActionCloseSession
	ActionCapture
	ActionProbe
	ActionResetBaseline
)

type Action struct {
	Kind    ActionKind
	Trigger Trigger
	Reason  EndReason
	At      time.Time
}

type Machine struct {
	Phase                                               Phase
	Block                                               BlockReason
	active                                              bool
	idle                                                time.Duration
	lastTick, lastInput, lastKey, lastClick, lastScroll time.Time
	front                                               string
	initialized, locked, typing                         bool
	lastAttempt, lastSaved, lastProbe                   time.Time
	inFlight, probing                                   bool
	pending, flying                                     Trigger
	failures                                            int
	retryAt                                             time.Time
	probeFactor                                         int
}

func (m *Machine) Step(o Observation, s Settings) []Action {
	now := o.Now
	var actions []Action
	add := func(k ActionKind, t Trigger, reason EndReason) {
		actions = append(actions, Action{Kind: k, Trigger: t, Reason: reason, At: now})
	}
	closeSession := func(reason EndReason) {
		if m.active {
			add(ActionCloseSession, "", reason)
			m.active = false
		}
	}
	if o.CaptureDone {
		m.inFlight = false
		if o.CaptureFailed {
			m.queue(m.flying)
			if m.failures < 16 {
				m.failures++
			}
			d := 500 * time.Millisecond * time.Duration(1<<uint(m.failures-1))
			if d > 30*time.Second {
				d = 30 * time.Second
			}
			m.retryAt = now.Add(d)
		} else {
			m.failures = 0
			m.retryAt = time.Time{}
		}
		if o.CaptureSaved {
			m.lastSaved = now
			m.lastProbe = now
		}
	}
	if o.ProbeDone {
		m.probing = false
		m.lastProbe = now
		if o.ProbeDuplicate {
			if m.probeFactor < 8 {
				m.probeFactor *= 2
				if m.probeFactor > 8 {
					m.probeFactor = 8
				}
			}
		} else {
			m.probeFactor = 1
			m.queue(TriggerVisual)
		}
	}
	if m.probeFactor == 0 {
		m.probeFactor = 1
	}
	if o.Stop {
		closeSession(EndStop)
		m.Phase = PhaseOff
		m.Block = BlockNone
		m.pending = ""
		m.typing = false
		return actions
	}
	slept := !m.lastTick.IsZero() && now.Sub(m.lastTick) > 5*time.Second
	m.lastTick = now
	if slept {
		closeSession(EndSuspend)
	}
	if o.Locked {
		closeSession(EndLock)
	}
	if m.locked && !o.Locked {
		add(ActionResetBaseline, "", "")
		m.probeFactor = 1
	}
	m.locked = o.Locked
	if !m.initialized {
		m.initialized = true
		m.front = o.Front.Bundle
		m.lastKey = eventTime(now, o.SinceKey)
		m.lastClick = eventTime(now, o.SinceClick)
		m.lastScroll = eventTime(now, o.SinceScroll)
		m.lastSaved = now
		m.lastProbe = now
	}
	key := eventTime(now, o.SinceKey)
	click := eventTime(now, o.SinceClick)
	scroll := eventTime(now, o.SinceScroll)
	newKey := key.After(m.lastKey.Add(20 * time.Millisecond))
	newClick := click.After(m.lastClick.Add(20 * time.Millisecond))
	newScroll := scroll.After(m.lastScroll.Add(20 * time.Millisecond))
	if newKey {
		m.lastKey = key
		m.typing = true
	}
	if newClick {
		m.lastClick = click
	}
	if newScroll {
		m.lastScroll = scroll
	}
	focus := o.Front.Bundle != m.front && o.Front.Bundle != ""
	if focus {
		m.front = o.Front.Bundle
		add(ActionResetBaseline, "", "")
	}
	if m.active && m.idle > 0 && now.Sub(m.lastInput) > m.idle {
		closeSession(EndIdle)
	}
	if o.Locked {
		m.Phase = PhaseBlocked
		m.Block = BlockScreenLocked
		m.pending = ""
		m.typing = false
		return actions
	}
	if !o.PausedUntil.IsZero() && now.Before(o.PausedUntil) {
		m.Phase = PhasePaused
		m.Block = BlockNone
		m.pending = ""
		m.typing = false
		return actions
	}
	if o.ScreenPermission == PermDenied {
		m.Phase = PhaseBlocked
		m.Block = BlockNoScreenPermission
		m.pending = ""
		m.typing = false
		return actions
	}
	if sensitiveFront(o.Front, s.SensitiveApps) {
		m.Phase = PhaseBlocked
		m.Block = BlockSensitiveApp
		m.pending = ""
		m.typing = false
		return actions
	}

	input := newKey || newClick || newScroll || focus
	if input {
		m.lastInput = now
		m.probeFactor = 1
		if !m.active {
			m.active = true
			m.idle = s.Idle
			add(ActionOpenSession, "", "")
		}
	}
	if focus {
		m.queue(TriggerAppFocus)
	}
	if newClick {
		m.queue(TriggerClick)
	}
	if m.typing && now.Sub(m.lastKey) >= s.TypingPause {
		m.typing = false
		m.queue(TriggerTypingPause)
	}
	if s.Heartbeat > 0 && now.Sub(m.lastSaved) >= s.Heartbeat {
		m.queue(TriggerHeartbeat)
	}
	if m.active {
		m.Phase = PhaseActive
	} else {
		m.Phase = PhaseWaiting
	}
	m.Block = BlockNone
	if m.pending != "" && !m.inFlight && !m.probing && !now.Before(m.retryAt) && (m.lastAttempt.IsZero() || now.Sub(m.lastAttempt) >= s.Debounce) {
		t := m.pending
		m.pending = ""
		m.inFlight = true
		m.flying = t
		m.lastAttempt = now
		add(ActionCapture, t, "")
	}
	if !m.inFlight && !m.probing && m.pending == "" && s.ScreenPoll > 0 && !now.Before(m.retryAt) && now.Sub(m.lastSaved) >= s.ScreenPoll*time.Duration(m.probeFactor) && now.Sub(m.lastProbe) >= s.ScreenPoll*time.Duration(m.probeFactor) {
		m.probing = true
		add(ActionProbe, "", "")
	}
	return actions
}
func eventTime(now time.Time, d time.Duration) time.Time {
	if d >= 365*24*time.Hour {
		return time.Time{}
	}
	if d < 0 {
		return now
	}
	return now.Add(-d)
}
func (m *Machine) queue(t Trigger) {
	rank := func(t Trigger) int {
		switch t {
		case TriggerAppFocus:
			return 5
		case TriggerClick:
			return 4
		case TriggerTypingPause:
			return 3
		case TriggerVisual:
			return 2
		case TriggerHeartbeat:
			return 1
		}
		return 0
	}
	if rank(t) > rank(m.pending) {
		m.pending = t
	}
}

func sensitiveFront(front Front, apps []string) bool {
	for _, app := range apps {
		if front.Bundle != "" && strings.EqualFold(strings.TrimSpace(app), front.Bundle) {
			return true
		}
	}
	return false
}
