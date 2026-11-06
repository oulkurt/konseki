//go:build e2e

/*
 * [INPUT]: 依赖粉碎动画每一帧的帧时间和 KONSEKI_HOME。
 * [OUTPUT]: 提供 probeShredFrame 与 probeShredDone；动画结束时把帧间隔统计写进 e2e-shred-frames.json。
 * [POS]: 测试构建的真机帧耗时探针，scripts/live 读它的输出。正式构建用 e2e_frames_off.go 的空实现。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// shredProbeState 按毫秒记粉碎动画的帧间隔。
// 两帧开头之间的间隔反映前一帧的耗时，所以间隔记在前一帧所处的阶段下。
type shredProbeState struct {
	last           time.Time
	retract, shred []float64
}

var shredProbe shredProbeState

// probeShredFrame 在每一帧开头调用。shading 表示上一帧已进入碎纸阶段，画了着色器。
func probeShredFrame(now time.Time, shading bool) {
	p := &shredProbe
	if !p.last.IsZero() {
		gap := float64(now.Sub(p.last)) / float64(time.Millisecond)
		switch {
		case gap > 1000:
			// 超过一秒是新一轮动画，上一轮没有走到结尾。
			p.retract, p.shred = nil, nil
		case shading:
			p.shred = append(p.shred, gap)
		default:
			p.retract = append(p.retract, gap)
		}
	}
	p.last = now
}

// probeShredDone 在动画结束时写出统计，然后清零。没有 KONSEKI_HOME 时不写。
func probeShredDone() {
	p := shredProbe
	shredProbe = shredProbeState{}
	home := os.Getenv("KONSEKI_HOME")
	if home == "" {
		return
	}
	b, err := json.MarshalIndent(map[string]any{"retract": frameStats(p.retract), "shred": frameStats(p.shred)}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(home, "e2e-shred-frames.json"), b, 0o600)
}

// frameStats 汇总帧间隔：帧数、中位数、95 分位、最大值，
// 以及超过 16.7 毫秒（60Hz 一帧）的帧在本阶段里的序号和间隔，用来区分一次性开销和持续掉帧。
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
