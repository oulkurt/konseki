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
	five := time.NewTicker(5 * time.Minute)
	defer five.Stop()
	half := time.NewTicker(30 * time.Minute)
	defer half.Stop()
	six := time.NewTicker(6 * time.Hour)
	defer six.Stop()
	settings := func() (*Storage, Settings) { r.mu.Lock(); defer r.mu.Unlock(); return r.storage, r.cfg }
	for {
		select {
		case <-stop:
			return
		case <-initial.C:
			st, cfg := settings()
			_ = st.Rescan()
			_, _ = st.Prune(cfg.RetentionDays, time.Now())
			_, _ = st.Degrade(time.Now())
			_, _ = st.Enforce(cfg.MaxStorage)
		case <-five.C:
			st, cfg := settings()
			_, _ = st.Enforce(cfg.MaxStorage)
		case <-half.C:
			st, _ := settings()
			_, _ = st.Degrade(time.Now())
			_ = st.Rescan()
		case <-six.C:
			st, cfg := settings()
			_, _ = st.Prune(cfg.RetentionDays, time.Now())
		case <-enforce:
			st, cfg := settings()
			_, _ = st.Enforce(cfg.MaxStorage)
		}
	}
}
