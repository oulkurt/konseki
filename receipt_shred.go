/*
 * [INPUT]: 依赖确认时的小票、碎纸物理与 MyGo 蒙版着色器。
 * [OUTPUT]: 提供收回、碎纸和恢复送纸的动画。
 * [POS]: 时间轴的清除过渡；旧纸面不接收输入。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Copyright (c) 2026 Caelyreth
// Licensed under the MIT License
package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

const shredRetractDuration = 600 * time.Millisecond
const shredFallDuration = 1700 * time.Millisecond

var shredCoordinates = shader.MustCompile(`package main
var Points [49]vec2
var Count int
var Offset vec2
var Scale vec2
var HalfWidth float
var Index float
var StripCount float
func Fragment(dst vec4) vec4 {
    q := dst.xy / Scale - Offset
    best := 1000000.0
    bestAlong := 0.0
    bestAcross := 0.5
    bestSegment := 0.0
    for i := 0; i < 48; i++ {
        if i+1 >= Count { break }
        a := Points[i]
        tangent := Points[i+1] - a
        l2 := max(dot(tangent,tangent), 0.0001)
        along := clamp(dot(q-a,tangent)/l2,0,1)
        delta := q-(a+tangent*along)
        distance := length(delta)
        if distance < best {
            best = distance
            normal := vec2(-tangent.y,tangent.x)/sqrt(l2)
            bestAcross = clamp(dot(q-a,normal)/(2*HalfWidth)+0.5,0,1)
            bestAlong = along
            bestSegment = float(i)
        }
    }
    coverage := 1.0-smoothstep(HalfWidth-0.6,HalfWidth+0.6,best)
    u := (Index + bestAcross) / StripCount
    v := 1.0 - (bestSegment + bestAlong) / 48.0
    return vec4(u*coverage,v*coverage,0,coverage)
}
`)

var shredEffect = shader.MustCompile(`package main
var SourceSize vec2
func LayerEffect(pos vec2) vec4 {
    m := image0Sample(pos * image0Size() / rectSize())
    if m.a < 0.01 { return vec4(0) }
    uv := clamp(m.rg / m.a, vec2(0), vec2(1))
    src := vec2((rectSize().x - SourceSize.x) / 2.0, 0) + uv * SourceSize
    return layerSample(src) * m.a
}
`)

type shredAnimation struct {
	pending     bool
	start       time.Time
	feedStarted bool
	height      float32
	mask        *ui.Canvas
	handle      ui.Handle
	physics     shredSimulation
}

func (a *app) stopShred() {
	a.shred.pending = false
	a.clearReceipt = ReceiptDay{}
	a.feedDay = ""
}

func (a *app) renderShred(c *ui.Context, width float32) {
	if c.Preferences().ReduceMotion {
		a.stopShred()
		a.renderPaper(c, width)
		return
	}
	s := &a.shred
	if s.start.IsZero() {
		s.start = c.Now()
		s.feedStarted = false
		s.height = 0
	}
	stage := ui.Box(c).FillWidth().Grow(1).Clip()
	bounds := stage.LayoutBounds()
	if bounds.W <= 0 || bounds.H <= 0 {
		bounds.W, bounds.H = c.Size()
	}
	if h := s.handle.Bounds(c).H; h > 0 && !s.feedStarted {
		s.height = min(h, bounds.H)
	}
	elapsed := c.Now().Sub(s.start)
	if elapsed < shredRetractDuration || s.height <= 0 {
		progress := min(1, max(0, float64(elapsed)/float64(shredRetractDuration)))
		offset := -s.height * float32(progress*progress*progress)
		stage.Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Translate(0, offset).Children(func() { a.renderShredPaper(c, width) })
		})
		c.AnimationFrame()
		return
	}
	if !s.feedStarted {
		s.feedStarted = true
		s.physics.reset(c.Now(), width, s.height)
	}
	s.physics.advance(c.Now())
	if s.physics.elapsed >= float32((shredFeedDuration + shredFallDuration).Seconds()) {
		a.stopShred()
		stage.Children(func() { ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() { a.renderPaper(c, width) }) })
		return
	}
	if s.mask == nil {
		s.mask = ui.NewCanvas(1024, 512)
	}
	stage.Children(func() {
		layer := ui.Box(c).Fill().PassThrough().Effect(shredEffect.Effect(shader.Options{
			Uniforms: map[string]any{"SourceSize": []float32{width, s.height}}, Images: [4]*ui.Bitmap{s.mask.Bitmap()},
		}))
		layer.Draw(func(p *ui.Painter, r ui.Rect) {
			s.mask.Draw(p, func(cp *ui.Painter) {
				w, h := s.mask.Size()
				sx, sy := float32(w)/max(r.W, 1), float32(h)/max(r.H, 1)
				for i := range s.physics.strips {
					drawShredMask(cp, (r.W-width)/2, sx, sy, i, &s.physics)
				}
			})
		})
		layer.Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() { a.renderShredPaper(c, width) })
		})
	})
	c.AnimationFrame()
}

func (a *app) renderShredPaper(c *ui.Context, width float32) {
	paper := ui.Hex("#FFFDF7")
	if c.Theme().Dark {
		paper = ui.Hex("#ECE8DF")
	}
	ui.Column(c).Width(width).Bind(&a.shred.handle).Children(func() {
		ui.Column(c).FillWidth().Background(paper).Padding(20, 22, 16, 22).Gap(9).Children(func() { a.renderReceiptDayContents(c, a.clearReceipt, false) })
		receiptZigzag(c, paper)
	})
}

func drawShredMask(p *ui.Painter, x, sx, sy float32, index int, sim *shredSimulation) {
	line := &sim.strips[index]
	if len(line.nodes) < 2 {
		return
	}
	points := make([]float32, 0, (shredSegments+1)*2)
	minX, minY, maxX, maxY := line.nodes[0].x, line.nodes[0].y, line.nodes[0].x, line.nodes[0].y
	for _, n := range line.nodes {
		points = append(points, n.x, n.y)
		minX, minY = min(minX, n.x), min(minY, n.y)
		maxX, maxY = max(maxX, n.x), max(maxY, n.y)
	}
	half := sim.width / float32(len(sim.strips)) * 0.46
	pad := half + 2
	area := ui.Rect{X: (x + minX - pad) * sx, Y: (minY - pad) * sy, W: (maxX - minX + 2*pad) * sx, H: (maxY - minY + 2*pad) * sy}
	shredCoordinates.Draw(p, area, shader.Options{Uniforms: map[string]any{
		"Points": points, "Count": len(line.nodes), "Offset": []float32{x, 0}, "Scale": []float32{sx, sy},
		"HalfWidth": half, "Index": float32(index), "StripCount": float32(len(sim.strips)),
	}})
}
