/*
 * [INPUT]: 依赖固定的六十个采集块和 MyGo 离屏测试器。
 * [OUTPUT]: 提供时间轴单帧构建与绘制基准。
 * [POS]: 根模块性能基线，供旧时间轴和小票比较。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"testing"
	"time"

	"konseki/internal/capture"

	"github.com/egoist/mygo/ui"
)

func BenchmarkTimelineFrame(b *testing.B) {
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local)
	a := &app{lastLoad: time.Now().Add(time.Hour)}
	for i := 0; i < 60; i++ {
		start := base.Add(time.Duration(i) * 10 * time.Minute)
		a.blocks = append(a.blocks, capture.Block{Start: start, End: start.Add(5 * time.Minute), App: fmt.Sprintf("App %d", i%5), Titles: []string{fmt.Sprintf("Window %d", i)}, Samples: 2})
	}
	a.dayFilter = base.Format("2006-01-02")
	a.receipt = buildReceiptWindow(base, nil, a.blocks, nil, 10*time.Minute)
	tt := ui.NewTester(a.view, 780, 900)
	tt.Advance(time.Second)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tt.Frame()
	}
}
