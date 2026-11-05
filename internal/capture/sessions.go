/*
 * [INPUT]: 依赖全部日期的快照和已关闭会话。
 * [OUTPUT]: 补写进程崩溃遗留的会话关闭记录。
 * [POS]: 启动恢复边界，仅根据磁盘证据重建会话。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func recoverSession(st *Store, storage *Storage, now time.Time) error {
	closed := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(st.root, "sessions"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !dayJSON(entry.Name()) {
			continue
		}
		sessions, err := st.ReadSessions(strings.TrimSuffix(entry.Name(), ".jsonl"))
		if err != nil {
			return err
		}
		for _, session := range sessions {
			closed[session.ID] = true
		}
	}
	entries, err = os.ReadDir(st.root)
	if err != nil {
		return err
	}
	var latest Snapshot
	var last time.Time
	var snaps []Snapshot
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.IsDir() || !dayJSON(entry.Name()) {
			continue
		}
		rows, err := st.ReadDay(strings.TrimSuffix(entry.Name(), ".jsonl"))
		if err != nil {
			return err
		}
		if latest.Session == "" {
			for _, row := range rows {
				if row.Session == "" {
					continue
				}
				t, err := parseTS(row.TS)
				if err == nil && (last.IsZero() || t.After(last)) {
					last, latest = t, row
				}
			}
		}
		if latest.Session != "" {
			snaps = append(snaps, rows...)
		}
	}

	if latest.Session == "" || closed[latest.Session] {
		return nil
	}
	session := Session{ID: latest.Session, Start: last, End: last, EndReason: EndCrash}
	seen := map[string]bool{}
	for _, s := range snaps {
		if s.Session != session.ID {
			continue
		}
		t, err := parseTS(s.TS)
		if err == nil && t.Before(session.Start) {
			session.Start = t
		}
		session.Snapshots++
		if s.App != "" && !seen[s.App] {
			session.Apps = append(session.Apps, s.App)
			seen[s.App] = true
		}
	}
	if err := st.AppendSession(session); err != nil {
		return err
	}
	storage.CountSession()
	return nil
}
