/*
 * [INPUT]: 依赖碎纸物理模拟和固定时间步长。
 * [OUTPUT]: 验证模拟可确定重放及纸条送出。
 * [POS]: 粉碎动画的物理边界回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestShredPhysicsDeterministic(t *testing.T) {
	start := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	var a, b shredSimulation
	a.reset(start, 520, 400)
	b.reset(start, 520, 400)
	for i := 1; i <= 60; i++ {
		at := start.Add(time.Duration(i) * time.Second / 60)
		a.advance(at)
		b.advance(at)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("相同输入产生了不同的纸条轨迹")
	}
	if len(a.strips) != 74 || len(a.strips[0].nodes) == 0 {
		t.Fatalf("纸条未送出: %d", len(a.strips))
	}
}

func TestShredStagesAndReducedMotion(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	shotDay(t, a)
	a.clearReceipt = a.receipt
	a.receipt = ReceiptDay{}
	a.dayFilter = ""
	a.lastLoad = time.Now().Add(time.Hour)
	a.shred.pending = true
	tt := ui.NewTester(a.view, 300, 400)
	if !a.shred.pending || a.shred.feedStarted {
		t.Fatal("未从收回阶段开始")
	}
	tt.Advance(650 * time.Millisecond)
	if !a.shred.feedStarted {
		t.Fatal("收回后未开始碎纸")
	}
	if a.shred.height <= 0 || a.shred.mask == nil {
		t.Fatal("未创建纸面蒙版")
	}
	tt.Advance(100 * time.Millisecond)
	if len(a.shred.physics.strips[0].nodes) < 2 {
		t.Fatal("碎纸没有送出")
	}
	a.stopShred()
	if a.shred.pending || len(a.clearReceipt.Lines) != 0 {
		t.Fatal("中途结束未释放旧小票")
	}
	a.shred.pending = true
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if a.shred.pending {
		t.Fatal("减少动态效果时未立即结束")
	}
}
