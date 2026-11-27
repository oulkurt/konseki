//go:build e2e

/*
 * [INPUT]: 依赖粉碎动画每一帧的帧时间和 KONSEKI_HOME。
 * [OUTPUT]: 提供 probeShredFrame、probeShredReset 与 probeShredDone；动画结束时把帧间隔统计写进 e2e-shred-frames.json。
 * [POS]: 测试构建的真机帧耗时探针，scripts/live 读它的输出。正式构建用 e2e_frames_off.go 的空实现。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type shredProbeState struct {
	mu             sync.Mutex
	last           time.Time
	retract, shred []float64
}

var shredProbe shredProbeState

func probeShredReset() {
	shredProbe.mu.Lock()
	defer shredProbe.mu.Unlock()
	shredProbe.last = time.Time{}
	shredProbe.retract = nil
	shredProbe.shred = nil
}

func probeShredFrame(now time.Time, shading bool) {
	p := &shredProbe
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.last.IsZero() && now.After(p.last) {
		gap := float64(now.Sub(p.last)) / float64(time.Millisecond)
		if shading {
			p.shred = append(p.shred, gap)
		} else {
			p.retract = append(p.retract, gap)
		}
	}
	p.last = now
}

func probeShredDone() {
	p := &shredProbe
	p.mu.Lock()
	retract := slices.Clone(p.retract)
	shred := slices.Clone(p.shred)
	p.last = time.Time{}
	p.retract = nil
	p.shred = nil
	p.mu.Unlock()
	home := os.Getenv("KONSEKI_HOME")
	if home == "" {
		return
	}
	b, err := json.MarshalIndent(map[string]any{"retract": frameStats(retract), "shred": frameStats(shred)}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(home, "e2e-shred-frames.json"), b, 0o600)
}

func frameStats(ms []float64) map[string]any {
	if len(ms) == 0 {
		return map[string]any{"frames": 0}
	}
	s := slices.Sorted(slices.Values(ms))
	at := func(q float64) float64 { return s[int(q*float64(len(s)-1))] }
	slow := [][2]float64{}
	for i, v := range ms {
		if v > 16.7 {
			slow = append(slow, [2]float64{float64(i), v})
		}
	}
	return map[string]any{"frames": len(s), "p50": at(0.5), "p95": at(0.95), "max": s[len(s)-1], "over16_7ms": len(slow), "slow": slow}
}
