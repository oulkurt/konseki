/*
 * [INPUT]: 依赖可注入的清除步骤和摘要完成信号。
 * [OUTPUT]: 验证清除顺序、等待和错误短路。
 * [POS]: 危险操作的并发和失败路径回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"konseki/internal/config"
	"konseki/internal/skill"
	"konseki/internal/summary"
)

func TestClearSequenceWaitsAndStopsOnFailure(t *testing.T) {
	done := make(chan struct{})
	started := make(chan struct{})
	result := make(chan error, 1)
	var steps []int
	failure := errors.New("summary failure")
	go func() {
		close(started)
		result <- clearSequence(done, func() error { steps = append(steps, 1); return nil }, func() error { steps = append(steps, 2); return failure }, func() error { steps = append(steps, 3); return nil })
	}()
	<-started
	select {
	case <-result:
		t.Fatal("摘要退出之前完成清除")
	case <-time.After(10 * time.Millisecond):
	}
	close(done)
	if err := <-result; !errors.Is(err, failure) {
		t.Fatalf("错误 = %v", err)
	}
	if !reflect.DeepEqual(steps, []int{1, 2}) {
		t.Fatalf("顺序 = %v", steps)
	}
}

func TestClearActivityWaitsForFinalSummaryWrite(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	exportDir := filepath.Join(t.TempDir(), "export")
	if err := a.cfg.Update(func(c *config.Config) { c.Skill.ExportDir = exportDir }); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	old := summary.Entry{ID: "old", Day: "2026-11-04", Headline: "private"}
	if err := a.sums.Upsert(old); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Export(skill.Options{Dir: exportDir}, []skill.DayData{{Day: old.Day, Entries: []summary.Entry{old}}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	canceled := false
	a.sumDone = done
	a.sumRunning = true
	a.sumCancel = func() { canceled = true }
	a.receipt = ReceiptDay{Lines: []ReceiptLine{{ID: "old"}}}
	a.lastLoad = time.Now()
	posted := make(chan func(), 1)
	a.post = func(f func()) { posted <- f }
	a.clearActivity()
	if !canceled {
		t.Fatal("未取消摘要")
	}
	// 模拟最后一段在收到取消后落盘，然后任务才退出。
	if err := a.sums.Upsert(summary.Entry{ID: "late", Day: old.Day}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-posted:
		t.Fatal("摘要退出前清除已完成")
	default:
	}
	close(done)
	select {
	case f := <-posted:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("清除超时")
	}
	if a.clearErr != "" {
		t.Fatal(a.clearErr)
	}
	if len(a.receipt.Lines) != 0 || !a.lastLoad.IsZero() {
		t.Fatalf("清除后小票或刷新节流未清空: %#v, %v", a.receipt, a.lastLoad)
	}
	if len(a.clearReceipt.Lines) != 1 || a.clearReceipt.Lines[0].ID != "old" {
		t.Fatal("未保留确认时的小票")
	}
	if got := a.sums.Days(); len(got) != 0 {
		t.Fatalf("摘要残留: %v", got)
	}
	for _, ext := range []string{".md", ".json"} {
		if _, err := os.Stat(filepath.Join(exportDir, "days", old.Day+ext)); !os.IsNotExist(err) {
			t.Fatalf("导出残留: %v", err)
		}
	}
}

func TestRefreshDoesNotReadDuringClear(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.clearRunning = true
	a.receipt = ReceiptDay{Lines: []ReceiptLine{{ID: "保留"}}}
	a.refresh()
	if !a.lastLoad.IsZero() || len(a.receipt.Lines) != 1 || a.receipt.Lines[0].ID != "保留" {
		t.Fatal("清除期间刷新了磁盘缓存")
	}
}
