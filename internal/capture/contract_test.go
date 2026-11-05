/*
 * [INPUT]: 依赖临时目录、采集存储和运行状态。
 * [OUTPUT]: 验证历史兼容、会话、OCR 与过期清理。
 * [POS]: 采集模块的跨版本磁盘契约测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStoreContracts(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().Format("2006-01-02")
	legacy := `{"ts":"2026-11-05T14:35:02Z","app":"old","title":"old title","pid":1,"ocrLines":0,"imgKB":1,"ocrMs":2}` + "\n"
	if err = os.WriteFile(filepath.Join(root, day+".jsonl"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	for _, snap := range []Snapshot{{ID: "a", TS: time.Now().Format(time.RFC3339), Title: "A"}, {ID: "b", TS: time.Now().Format(time.RFC3339), OCR: "existing", OCRLine: 8}} {
		if err = s.Append(snap); err != nil {
			t.Fatal(err)
		}
	}
	results := []OCRResult{{Snap: "a", Text: "recognized", Lines: 2, Ms: 4, Status: "ok"}, {Snap: "b", Text: "replacement", Lines: 3, Status: "ok"}}
	for _, v := range results {
		if err = s.AppendOCR(day, v); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ReadOCR(day)
	if err != nil || !reflect.DeepEqual(got, results) {
		t.Fatalf("OCR %v %v", got, err)
	}
	snaps, err := s.ReadDay(day)
	if err != nil || len(snaps) != 3 || snaps[0].App != "old" || snaps[1].OCR != "recognized" || snaps[1].OCRLine != 2 || snaps[2].OCR != "existing" || snaps[2].OCRLine != 8 {
		t.Fatalf("快照 %+v %v", snaps, err)
	}
	now := time.Now()
	session := Session{ID: "s", Start: now.Add(-time.Hour), End: now, EndReason: EndStop, Apps: []string{"A"}, Snapshots: 1}
	if err = s.AppendSession(session); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.ReadSessions(day)
	if err != nil || len(sessions) != 1 || sessions[0].ID != "s" || !sessions[0].End.Equal(now) || !reflect.DeepEqual(sessions[0].Apps, session.Apps) {
		t.Fatalf("会话 %v %v", sessions, err)
	}
	a, b := s.NewShotPath(now), s.NewShotPath(now)
	if a == b || filepath.Dir(a) != filepath.Join(root, "shots", day) {
		t.Fatalf("截图路径 %s %s", a, b)
	}
	for _, d := range []string{"2099-01-01"} {
		if rows, err := s.ReadOCR(d); err != nil || rows != nil {
			t.Fatal(rows, err)
		}
		if rows, err := s.ReadSessions(d); err != nil || rows != nil {
			t.Fatal(rows, err)
		}
		if rows, err := s.ReadDay(d); err != nil || rows != nil {
			t.Fatal(rows, err)
		}
	}
}

func TestStoragePrune(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	files := []string{"2026-11-02.jsonl", "ocr/2026-11-02.jsonl", "sessions/2026-11-02.jsonl", "shots/2026-11-02/a.jpg", "shots/20261102-123456.jpg", "2026-11-03.jsonl", "shots/2026-11-03/a.jpg", "shots/20261103-123456.jpg"}
	for _, p := range files {
		path := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStorage(root)
	if n, err := s.Prune(0, now); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := s.Prune(2, now); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	for i, p := range files {
		_, err := os.Stat(filepath.Join(root, p))
		if (i < 5) != os.IsNotExist(err) {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

func TestRunnerPauseAndSessions(t *testing.T) {
	root := t.TempDir()
	r, err := New(Settings{Root: root, Heartbeat: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	future := time.Now().Add(time.Hour)
	r.Pause(future)
	if got := r.Status(); got.Phase != PhasePaused || !got.PausedUntil.Equal(future) {
		t.Fatal(got)
	}
	r.Resume()
	if r.Status().Phase != PhaseActive {
		t.Fatal(r.Status())
	}
	r.Pause(time.Now().Add(-time.Second))
	if r.Status().Phase != PhaseActive {
		t.Fatal(r.Status())
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := r.st.AppendSession(Session{ID: string(rune('a' + i)), End: now.Add(-time.Duration(i) * 24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	rows := r.RecentSessions(2)
	if len(rows) != 2 || rows[0].ID != "a" || rows[1].ID != "b" {
		t.Fatal(rows)
	}
}
