/*
 * [INPUT]: 依赖最近两天的快照和已关闭会话。
 * [OUTPUT]: 补写进程崩溃遗留的会话关闭记录。
 * [POS]: 启动恢复边界，仅根据磁盘证据重建会话。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

func recoverSession(st *Store, storage *Storage, now time.Time) error {
	closed := map[string]bool{}
	var latest Snapshot
	var last time.Time
	var snaps []Snapshot
	for i := 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		sessions, err := st.ReadSessions(day)
		if err != nil {
			return err
		}
		for _, s := range sessions {
			closed[s.ID] = true
		}
		rows, err := st.ReadDay(day)
		if err != nil {
			return err
		}
		snaps = append(snaps, rows...)
		for _, s := range rows {
			if s.Session == "" {
				continue
			}
			t, err := parseTS(s.TS)
			if err == nil && (last.IsZero() || t.After(last)) {
				last = t
				latest = s
			}
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
