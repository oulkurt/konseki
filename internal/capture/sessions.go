/*
 * [INPUT]: 依赖最近的快照日期与其后的会话关闭记录。
 * [OUTPUT]: 补写进程崩溃遗留的会话关闭记录。
 * [POS]: 启动恢复边界；向旧日期扫描至会话不再出现。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func recoverSession(st *Store, storage *Storage, now time.Time) error {
	entries, err := os.ReadDir(st.root)
	if err != nil {
		return err
	}
	var latest Snapshot
	var last time.Time
	var session Session
	seen := map[string]bool{}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.IsDir() || !dayJSON(entry.Name()) {
			continue
		}
		day := strings.TrimSuffix(entry.Name(), ".jsonl")
		if latest.Session == "" {
			rows, err := st.ReadSnapshots(day)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.Session == "" {
					continue
				}
				t, err := parseTS(row.TS)
				if err == nil && (last.IsZero() || t.After(last)) {
					last, latest = t, row
				}
			}
			if latest.Session == "" {
				continue
			}
			closed, err := sessionClosed(st, latest.Session, day)
			if err != nil || closed {
				return err
			}
			session = Session{ID: latest.Session, Start: last, End: last, EndReason: EndCrash}
			for _, row := range rows {
				addRecoverySnapshot(&session, seen, row)
			}
			continue
		}
		found, err := scanRecoveryDay(filepath.Join(st.root, day+".jsonl"), &session, seen)
		if err != nil {
			return err
		}
		if !found {
			break
		}
	}
	if latest.Session == "" {
		return nil
	}
	if err := st.AppendSession(session); err != nil {
		return err
	}
	storage.CountSession()
	return nil
}

func sessionClosed(st *Store, id, fromDay string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(st.root, "sessions"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !dayJSON(entry.Name()) || entry.Name() < fromDay+".jsonl" {
			continue
		}
		rows, err := st.ReadSessions(strings.TrimSuffix(entry.Name(), ".jsonl"))
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if row.ID == id {
				return true, nil
			}
		}
	}
	return false, nil
}

func scanRecoveryDay(path string, session *Session, seen map[string]bool) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	found := false
	for sc.Scan() {
		var row struct {
			TS      string `json:"ts"`
			Session string `json:"session"`
			App     string `json:"app"`
		}
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.Session == session.ID {
			found = true
			addRecoverySnapshot(session, seen, Snapshot{TS: row.TS, Session: row.Session, App: row.App})
		}
	}
	return found, sc.Err()
}

func addRecoverySnapshot(session *Session, seen map[string]bool, s Snapshot) {
	if s.Session != session.ID {
		return
	}
	if t, err := parseTS(s.TS); err == nil && t.Before(session.Start) {
		session.Start = t
	}
	session.Snapshots++
	if s.App != "" && !seen[s.App] {
		session.Apps = append(session.Apps, s.App)
		seen[s.App] = true
	}
}
