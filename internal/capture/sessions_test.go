/*
 * [INPUT]: 依赖临时目录中的快照与会话关闭记录。
 * [OUTPUT]: 验证崩溃恢复只补最后一个未关闭会话。
 * [POS]: 会话恢复的磁盘证据测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
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
