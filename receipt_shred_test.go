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
