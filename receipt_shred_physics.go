/*
 * [INPUT]: 依赖送纸尺寸和固定时钟步长。
 * [OUTPUT]: 提供可重放的碎纸绳索模拟。
 * [POS]: 粉碎动画的物理层，不依赖界面和绘图。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Copyright (c) 2026 Caelyreth
// Licensed under the MIT License
package main

import (
	"math"
	"time"
)

const (
	shredSegments             = 48
	shredPhysicsStep          = time.Second / 120
	shredFeedDuration         = 1200 * time.Millisecond
	shredLead         float32 = 24
)

type shredNode struct{ x, y, px, py float32 }
type shredStrand struct{ nodes []shredNode }
type shredSimulation struct {
	strips               []shredStrand
	width, height, speed float32
	elapsed              float32
	lag                  time.Duration
	last                 time.Time
}

func (s *shredSimulation) reset(now time.Time, width, height float32) {
	*s = shredSimulation{last: now, width: width, height: height, speed: (height + shredLead) / float32(shredFeedDuration.Seconds()), strips: make([]shredStrand, max(1, int(width/7)))}
}

func (s *shredSimulation) advance(now time.Time) {
	if !now.After(s.last) {
		return
	}
	s.lag += min(now.Sub(s.last), 120*time.Millisecond)
	s.last = now
	for s.lag >= shredPhysicsStep {
		s.step(float32(shredPhysicsStep.Seconds()))
		s.lag -= shredPhysicsStep
	}
}

func shredWind(x, y, t float32) (float32, float32) {
	xWind := 280*math.Sin(float64(t)*1.8) + 180*math.Sin(float64(y)*0.018-float64(t)*2.4) + 70*math.Sin(float64(x)*0.025+float64(t)*3.1)
	yWind := 85 * math.Sin(float64(x)*0.018+float64(y)*0.009+float64(t)*2.2)
	return float32(xWind), float32(yWind)
}

func (s *shredSimulation) step(dt float32) {
	s.elapsed += dt
	top := -s.height - shredLead + s.speed*s.elapsed
	stepLen := s.height / shredSegments
	attached := top < 0
	for i := range s.strips {
		line := &s.strips[i]
		center := (float32(i) + 0.5) * s.width / float32(len(s.strips))
		for len(line.nodes) < shredSegments+1 && top+s.height-float32(len(line.nodes))*stepLen >= 0 {
			flutter := float32(math.Sin(float64(i)*1.71+float64(len(line.nodes))*0.53)) * 30
			line.nodes = append(line.nodes, shredNode{x: center, px: center - flutter*dt, py: -s.speed * dt})
		}
		for k := range line.nodes {
			n := &line.nodes[k]
			vx, vy := (n.x-n.px)*0.994, (n.y-n.py)*0.994
			n.px, n.py = n.x, n.y
			windX, windY := shredWind(n.x, n.y, s.elapsed)
			n.x += vx + windX*dt*dt
			n.y += vy + (700+windY)*dt*dt
		}
		for pass := 0; pass < 7; pass++ {
			if attached && len(line.nodes) > 0 {
				tail := min(stepLen, max(0, top+s.height-float32(len(line.nodes)-1)*stepLen))
				shredAnchor(&line.nodes[len(line.nodes)-1], center, tail)
			}
			for k := len(line.nodes) - 1; k > 0; k-- {
				shredPair(&line.nodes[k], &line.nodes[k-1], stepLen)
			}
		}
	}
}

func shredAnchor(n *shredNode, x, length float32) {
	dx, dy := n.x-x, n.y
	d := float32(math.Hypot(float64(dx), float64(dy)))
	if d < 0.0001 {
		n.x, n.y = x, length
		return
	}
	n.x, n.y = x+dx*length/d, dy*length/d
}

func shredPair(a, b *shredNode, length float32) {
	dx, dy := b.x-a.x, b.y-a.y
	d := float32(math.Hypot(float64(dx), float64(dy)))
	if d < 0.0001 {
		dx, dy, d = 0, 1, 1
	}
	correction := (d - length) / (2 * d)
	a.x += dx * correction
	a.y += dy * correction
	b.x -= dx * correction
	b.y -= dy * correction
}
