/*
 * [INPUT]: 依赖存储句柄、运行设置和维护计时器。
 * [OUTPUT]: 调度用量对账、保留清理、降级与容量限制。
 * [POS]: 采集外壳的后台维护边界，合并超额请求。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

func (r *Runner) maintenance(stop <-chan struct{}, done chan<- struct{}, enforce <-chan struct{}) {
	defer close(done)
	initial := time.NewTimer(time.Minute)
	defer initial.Stop()
	var five, half, six *time.Ticker
	var fiveC, halfC, sixC <-chan time.Time
	defer func() {
		if five != nil {
			five.Stop()
			half.Stop()
			six.Stop()
		}
	}()
	settings := func() (*Storage, Settings) { r.mu.Lock(); defer r.mu.Unlock(); return r.storage, r.cfg }
	for {
		select {
		case <-stop:
			return
		case <-initial.C:
			st, cfg := settings()
			r.setError(st.Rescan())
			_, err := st.Prune(cfg.RetentionDays, time.Now())
			r.setError(err)
			_, err = st.Degrade(time.Now())
			r.setError(err)
			_, err = st.Enforce(cfg.MaxStorage)
			r.setError(err)
			five = time.NewTicker(5 * time.Minute)
			half = time.NewTicker(30 * time.Minute)
			six = time.NewTicker(6 * time.Hour)
			fiveC, halfC, sixC = five.C, half.C, six.C
		case <-fiveC:
			st, cfg := settings()
			_, err := st.Enforce(cfg.MaxStorage)
			r.setError(err)
		case <-halfC:
			st, _ := settings()
			_, err := st.Degrade(time.Now())
			r.setError(err)
			r.setError(st.Rescan())
		case <-sixC:
			st, cfg := settings()
			_, err := st.Prune(cfg.RetentionDays, time.Now())
			r.setError(err)
		case <-enforce:
			st, cfg := settings()
			_, err := st.Enforce(cfg.MaxStorage)
			r.setError(err)
			r.mu.Lock()
			r.enforcePending = false
			r.mu.Unlock()
		}
	}
}
