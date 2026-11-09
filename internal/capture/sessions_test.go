/*
 * [INPUT]: 依赖临时目录中的快照与会话关闭记录。
 * [OUTPUT]: 验证崩溃恢复只补最后一个未关闭会话。
 * [POS]: 会话恢复的磁盘证据测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRecoverSession(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if err := st.Append(Snapshot{TS: now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano), Session: "unfinished", App: "A"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := recoverSession(st, NewStorage(root), now); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ReadSessions(now.Format("2006-01-02"))
	if err != nil || len(rows) != 1 || rows[0].EndReason != EndCrash || rows[0].Snapshots != 2 || !rows[0].End.Equal(now.Add(time.Second)) {
		t.Fatal(rows, err)
	}
	if err := recoverSession(st, NewStorage(root), now); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.ReadSessions(now.Format("2006-01-02"))
	if len(rows) != 1 {
		t.Fatal(rows)
	}
}

func TestRecoverSessionAcrossOldDays(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 1, 1, 23, 59, 0, 0, time.Local)
	last := first.Add(2 * time.Minute)
	for _, ts := range []time.Time{first, last} {
		if err := st.Append(Snapshot{TS: ts.Format(time.RFC3339Nano), Session: "old", App: "A"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Append(Snapshot{TS: last.Add(4 * 24 * time.Hour).Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	if err := recoverSession(st, NewStorage(root), last.Add(90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ReadSessions(last.Format("2006-01-02"))
	if err != nil || len(rows) != 1 || rows[0].ID != "old" || rows[0].Snapshots != 2 || !rows[0].Start.Equal(first) || !rows[0].End.Equal(last) {
		t.Fatal(rows, err)
	}
	if err := recoverSession(st, NewStorage(root), last.Add(91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.ReadSessions(last.Format("2006-01-02"))
	if len(rows) != 1 {
		t.Fatal(rows)
	}
}

func TestRunnerStartReturnsBeforeRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要命名管道")
	}
	d, _, _ := fakeDependencies()
	root := t.TempDir()
	r, err := newRunner(Settings{Root: root}, d)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := r.st.Append(Snapshot{TS: now.Format(time.RFC3339Nano), Session: "unfinished"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(root, "sessions", now.Format("2006-01-02")+".jsonl")
	if err := exec.Command("mkfifo", pipe).Run(); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	go func() { r.Start(); close(started) }()
	select {
	case <-started:
	case <-time.After(300 * time.Millisecond):
		go func() {
			f, _ := os.OpenFile(pipe, os.O_WRONLY, 0)
			if f != nil {
				f.WriteString("{\"id\":\"unfinished\"}\n")
				f.Close()
			}
		}()
		<-started
		r.Stop()
		t.Fatal("Start 等待会话恢复")
	}
	go func() {
		f, _ := os.OpenFile(pipe, os.O_WRONLY, 0)
		if f != nil {
			f.WriteString("{\"id\":\"unfinished\"}\n")
			f.Close()
		}
	}()
	r.Stop()
}

func TestRecoveryCompletesBeforeNewSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要命名管道")
	}
	d, c, sys := fakeDependencies()
	root := t.TempDir()
	r, err := newRunner(Settings{Root: root, Heartbeat: time.Hour, Idle: time.Hour}, d)
	if err != nil {
		t.Fatal(err)
	}
	now := c.Now()
	if err := r.st.Append(Snapshot{TS: now.Format(time.RFC3339Nano), Session: "unfinished"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(root, "sessions", now.Format("2006-01-02")+".jsonl")
	if err := exec.Command("mkfifo", pipe).Run(); err != nil {
		t.Fatal(err)
	}
	r.Start()
	writer := make(chan *os.File, 1)
	go func() { f, _ := os.OpenFile(pipe, os.O_WRONLY, 0); writer <- f }()
	f := <-writer
	if f == nil {
		t.Fatal("无法打开管道")
	}
	c.advance(250 * time.Millisecond)
	if r.Status().Front.Bundle != "" {
		t.Fatal("恢复完成前执行了 tick")
	}
	if err := os.Remove(pipe); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.Status().Front.Bundle == "a" })
	sys.mu.Lock()
	sys.click = c.Now()
	sys.mu.Unlock()
	c.advance(250 * time.Millisecond)
	eventually(t, func() bool { return r.Status().Session != nil })
	r.Stop()
	rows, err := r.st.ReadSessions(now.Format("2006-01-02"))
	if err != nil || len(rows) != 2 || rows[0].ID != "unfinished" || rows[0].EndReason != EndCrash || rows[1].EndReason != EndStop {
		t.Fatal(rows, err)
	}
}

func TestRecoverSkipsClosedSessionOnLaterDay(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 1, 1, 23, 59, 0, 0, time.Local)
	if err := st.Append(Snapshot{TS: first.Format(time.RFC3339Nano), Session: "closed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(Snapshot{TS: first.Add(72 * time.Hour).Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	end := first.Add(24 * time.Hour)
	if err := st.AppendSession(Session{ID: "closed", End: end, EndReason: EndStop}); err != nil {
		t.Fatal(err)
	}
	if err := recoverSession(st, NewStorage(root), end); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ReadSessions(end.Format("2006-01-02"))
	if err != nil || len(rows) != 1 || rows[0].EndReason != EndStop {
		t.Fatal(rows, err)
	}
	rows, err = st.ReadSessions(first.Format("2006-01-02"))
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
