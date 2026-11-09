/*
 * [INPUT]: 依赖存储句柄、运行设置和维护计时器。
 * [OUTPUT]: 调度用量对账、保留清理、降级与容量限制。
 * [POS]: 采集外壳的后台维护边界；停止不等待，清空等待旧任务。
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

// 调用方持有 lifecycle；停止采集不等待，清空数据必须等待。
func (r *Runner) startMaintenance(stop <-chan struct{}, enforce <-chan struct{}) {
	pending := r.maintenanceDone[:0]
	for _, done := range r.maintenanceDone {
		select {
		case <-done:
		default:
			pending = append(pending, done)
		}
	}
	done := make(chan struct{})
	r.maintenanceDone = append(pending, done)
	go r.maintenance(stop, done, enforce)
}
func (r *Runner) waitMaintenance() {
	for _, done := range r.maintenanceDone {
		<-done
	}
	r.maintenanceDone = nil
}

func (r *Runner) maintenance(stop <-chan struct{}, done chan<- struct{}, enforce <-chan struct{}) {
	defer close(done)
	ops := defaultMaintenanceOps()
	if r.deps.maintenanceOps != nil {
		ops = *r.deps.maintenanceOps
	}
	enforceLimit := func(st *Storage, cfg Settings) error {
		if cfg.MaxStorage <= 0 {
			return nil
		}
		_, err := ops.enforce(st, cfg.MaxStorage)
		return err
	}
	stopped := func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}
	clock, ok := r.deps.clock.(maintenanceClock)
	if !ok {
		clock = systemClock{}
	}
	initial, stopInitial := clock.Timer(time.Minute)
	defer stopInitial()
	var fiveC, halfC, sixC <-chan time.Time
	var stopFive, stopHalf, stopSix func()
	defer func() {
		if stopFive != nil {
			stopFive()
			stopHalf()
			stopSix()
		}
	}()
	settings := func() (*Storage, Settings) { r.mu.Lock(); defer r.mu.Unlock(); return r.storage, r.cfg }
	for {
		select {
		case <-stop:
			return
		case <-initial:
			st, cfg := settings()
			r.setError(ops.rescan(st))
			if stopped() {
				return
			}
			_, err := ops.prune(st, cfg.RetentionDays, r.deps.clock.Now())
			r.setError(err)
			if stopped() {
				return
			}
			_, err = ops.degrade(st, r.deps.clock.Now())
			r.setError(err)
			if stopped() {
				return
			}
			r.setError(enforceLimit(st, cfg))
			fiveC, stopFive = clock.Ticker(5 * time.Minute)
			halfC, stopHalf = clock.Ticker(30 * time.Minute)
			sixC, stopSix = clock.Ticker(6 * time.Hour)
		case <-fiveC:
			st, cfg := settings()
			r.setError(enforceLimit(st, cfg))
		case <-halfC:
			st, _ := settings()
			_, err := ops.degrade(st, r.deps.clock.Now())
			r.setError(err)
			if stopped() {
				return
			}
			r.setError(ops.rescan(st))
		case <-sixC:
			st, cfg := settings()
			_, err := ops.prune(st, cfg.RetentionDays, r.deps.clock.Now())
			r.setError(err)
		case <-enforce:
			st, cfg := settings()
			r.setError(enforceLimit(st, cfg))
		}
	}
}
