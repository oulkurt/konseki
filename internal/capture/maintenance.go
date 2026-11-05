/*
 * [INPUT]: 依赖存储句柄、运行设置和维护计时器。
 * [OUTPUT]: 调度用量对账、保留清理、降级与容量限制。
 * [POS]: 采集外壳的后台维护边界，合并超额请求。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "time"

type maintenanceOps struct {
	rescan  func(*Storage) error
	prune   func(*Storage, int, time.Time) (int, error)
	degrade func(*Storage, time.Time) (int, error)
	enforce func(*Storage, int64) (int64, error)
}

func defaultMaintenanceOps() maintenanceOps {
	return maintenanceOps{(*Storage).Rescan, (*Storage).Prune, (*Storage).Degrade, (*Storage).Enforce}
}

func (r *Runner) maintenance(stop <-chan struct{}, done chan<- struct{}, enforce <-chan struct{}) {
	defer close(done)
	ops := defaultMaintenanceOps()
	if r.deps.maintenanceOps != nil {
		ops = *r.deps.maintenanceOps
	}
	stopped := func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}
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
			r.setError(ops.rescan(st))
			if stopped() {
				return
			}
			_, err := ops.prune(st, cfg.RetentionDays, time.Now())
			r.setError(err)
			if stopped() {
				return
			}
			_, err = ops.degrade(st, time.Now())
			r.setError(err)
			if stopped() {
				return
			}
			_, err = ops.enforce(st, cfg.MaxStorage)
			r.setError(err)
			five = time.NewTicker(5 * time.Minute)
			half = time.NewTicker(30 * time.Minute)
			six = time.NewTicker(6 * time.Hour)
			fiveC, halfC, sixC = five.C, half.C, six.C
		case <-fiveC:
			st, cfg := settings()
			_, err := ops.enforce(st, cfg.MaxStorage)
			r.setError(err)
		case <-halfC:
			st, _ := settings()
			_, err := ops.degrade(st, time.Now())
			r.setError(err)
			if stopped() {
				return
			}
			r.setError(ops.rescan(st))
		case <-sixC:
			st, cfg := settings()
			_, err := ops.prune(st, cfg.RetentionDays, time.Now())
			r.setError(err)
		case <-enforce:
			st, cfg := settings()
			_, err := ops.enforce(st, cfg.MaxStorage)
			r.setError(err)
			r.mu.Lock()
			r.enforcePending = false
			r.mu.Unlock()
		}
	}
}
