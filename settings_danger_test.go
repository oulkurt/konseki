/*
 * [INPUT]: 依赖可注入的清除步骤和摘要完成信号。
 * [OUTPUT]: 验证清除顺序、等待、错误短路、失败提示、无数据时置灰、清除期间禁用录制入口和停止读盘，以及旧导出文件删除后保留新技能。
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

	"konseki/internal/capture"
	"konseki/internal/skill"
	"konseki/internal/summary"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func TestClearRunningDisablesRecorderControls(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.setRecorderEnabled(true)
	if !a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase == capture.PhaseOff {
		t.Fatal("录制未启动")
	}

	a.clearRunning = true
	tt := ui.NewTester(a.view, 760, 1400)
	if err := tt.Click("等待中"); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Get().Recorder.Enabled || a.runner.Status().Phase == capture.PhaseOff {
		t.Fatal("清除期间小票机按钮修改了录制状态")
	}

	a.tab = 1
	tt = ui.NewTester(a.view, 760, 1400)
	if err := tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("暂停记录"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("暂停 15 分钟") {
		t.Fatal("清除期间打开了暂停菜单")
	}
	rect, ok := tt.Find("活动记录器")
	if !ok {
		t.Fatal("设置开关缺失")
	}
	tt.ClickAt(700, rect.Y+12)
	a.pauseRecorder(time.Now().Add(15 * time.Minute))
	a.setRecorderEnabled(false)
	cfg := a.cfg.Get().Recorder
	phase := a.runner.Status().Phase
	if !cfg.Enabled || !cfg.PausedUntil.IsZero() || phase == capture.PhaseOff || phase == capture.PhasePaused {
		t.Fatalf("清除期间修改了录制状态: enabled=%v paused=%v phase=%v", cfg.Enabled, cfg.PausedUntil, phase)
	}
}

func TestDangerCancelPreservesData(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.sums.Upsert(summary.Entry{ID: "old", Day: "2026-11-04"}); err != nil {
		t.Fatal(err)
	}
	a.tab = 1
	a.refresh()
	tt := ui.NewTester(a.view, 760, 2600)
	if err := tt.Click("清除全部活动数据"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("删除全部活动数据？") {
		t.Fatal("缺少确认对话框")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.clearRunning || len(a.sums.Days()) != 1 {
		t.Fatal("取消后删除了数据")
	}
}

// 没有数据时清除按钮置灰，点了也不弹确认框；有了一条摘要以后才能点。
func TestDangerDisabledWithoutData(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab = 1
	a.refresh()
	tt := ui.NewTester(a.view, 760, 2600)
	_ = tt.Click("清除全部活动数据")
	if a.clearConfirm || tt.HasText("删除全部活动数据？") {
		t.Fatal("没有数据时清除按钮仍能打开确认框")
	}
	if err := a.sums.Upsert(summary.Entry{ID: "old", Day: "2026-11-04"}); err != nil {
		t.Fatal(err)
	}
	a.lastLoad = time.Time{}
	a.refresh()
	tt.Frame()
	if err := tt.Click("清除全部活动数据"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("删除全部活动数据？") {
		t.Fatal("有数据时清除按钮打不开确认框")
	}
}

func TestDangerConfirmClearsOnce(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.sums.Upsert(summary.Entry{ID: "old", Day: "2026-11-04"}); err != nil {
		t.Fatal(err)
	}
	a.tab = 1
	a.refresh()
	posted := make(chan func(), 2)
	a.post = func(f func()) { posted <- f }
	tt := ui.NewTester(a.view, 760, 2600)
	if err := tt.Click("清除全部活动数据"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("全部删除"); err != nil {
		t.Fatal(err)
	}
	if !a.clearRunning {
		t.Fatal("清除没有启动")
	}
	a.checkLegacy(locales.In("zh-Hans"))
	if !a.legacyChecked {
		t.Fatal("缓存未预热")
	}
	a.clearActivity()
	select {
	case f := <-posted:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("清除超时")
	}
	if a.clearErr != "" || a.clearRunning || len(a.sums.Days()) != 0 {
		t.Fatalf("清除未完成: %q", a.clearErr)
	}
	select {
	case <-posted:
		t.Fatal("重复启动了清除")
	default:
	}
	if a.hasSummaries || a.status.Usage.Sessions != 0 {
		t.Fatal("清除后统计未归零")
	}
}

func TestClearFailureStaysInSettings(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := os.RemoveAll(filepath.Join(a.appDir, "summaries")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.appDir, "summaries"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	a.tab = 1
	posted := make(chan func(), 1)
	a.post = func(f func()) { posted <- f }
	a.clearActivity()
	select {
	case f := <-posted:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("清除超时")
	}
	if a.clearErr == "" || a.tab != 1 || a.shred.pending {
		t.Fatalf("失败后状态错误：tab=%d error=%q shred=%v", a.tab, a.clearErr, a.shred.pending)
	}
}

func TestClearFailureMessagePlural(t *testing.T) {
	for _, tc := range []struct {
		lang string
		n    int
		want string
	}{{"zh-Hans", 2, "2 个文件无法删除"}, {"en", 1, "1 file could not be deleted"}, {"en", 2, "2 files could not be deleted"}} {
		got := locales.Catalog.Format(tc.lang, "recorderDeleteFailed", map[string]any{"count": tc.n})
		if got != tc.want {
			t.Errorf("%s/%d: %q", tc.lang, tc.n, got)
		}
	}
}

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
	legacyDir := a.legacyDir()
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(legacyDir, os.DirFS(filepath.Join("internal", "skill", "testdata", "legacy"))); err != nil {
		t.Fatal(err)
	}
	newSkillBytes, err := os.ReadFile(filepath.Join("skills", "konseki", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	newSkill := string(newSkillBytes)
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), []byte(newSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	if files, err := skill.FindLegacy(legacyDir); err != nil || len(files) != 3 {
		t.Fatalf("夹具: %v %v", files, err)
	}
	a.checkLegacy(locales.In("zh-Hans"))
	if !a.legacyChecked || len(a.legacyFiles) != 3 {
		t.Fatal("旧文件缓存未预热")
	}
	old := summary.Entry{ID: "old", Day: "2026-11-04", Headline: "private"}
	if err := a.sums.Upsert(old); err != nil {
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
	case <-time.After(10 * time.Second):
		t.Fatal("清除超时")
	}
	if a.legacyChecked || len(a.legacyFiles) != 0 {
		t.Fatal("旧文件缓存未刷新")
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
	if files, err := skill.FindLegacy(legacyDir); err != nil || len(files) != 0 {
		t.Fatalf("旧导出残留: %v %v", files, err)
	}
	b, err := os.ReadFile(filepath.Join(legacyDir, "SKILL.md"))
	if err != nil || string(b) != newSkill {
		t.Fatalf("新技能的 SKILL.md 被改动: %v", err)
	}
	entries, err := os.ReadDir(legacyDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Fatalf("清除写入了其他文件: %v %v", entries, err)
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
