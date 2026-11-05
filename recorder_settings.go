/*
 * [INPUT]: 依赖持久化设置和采集运行参数。
 * [OUTPUT]: 提供设置转换与采集开关同步。
 * [POS]: 应用层连接配置存储和采集模块。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"konseki/internal/capture"
	"konseki/internal/config"
	"time"
)

func recorderSettings(r config.Recorder, appDir string) capture.Settings {
	return capture.Settings{Root: r.Dir(appDir), Debounce: time.Duration(r.DebounceMs) * time.Millisecond, Heartbeat: time.Duration(r.HeartbeatMs) * time.Millisecond, Idle: time.Duration(r.IdleMs) * time.Millisecond, TypingPause: time.Duration(r.TypingPauseMs) * time.Millisecond, ScreenPoll: time.Duration(r.ScreenPollMs) * time.Millisecond, JPEGQuality: r.JPEGQuality, OCR: capture.OCRSettings{Enabled: r.OCR.Enabled, Languages: append([]string(nil), r.OCR.Languages...), EveryN: r.OCR.EveryN}, InputMonitor: r.InputMonitor, SensitiveApps: append([]string(nil), r.SensitiveApps...), MaxStorage: int64(r.MaxStorageMB) * 1024 * 1024, RetentionDays: r.RetentionDays}
}
func (a *app) applyRecorder() {
	a.cfgNow = a.cfg.Get()
	a.runner.Apply(recorderSettings(a.cfgNow.Recorder, a.appDir))
}
func (a *app) setRecorderEnabled(enabled bool) {
	if err := a.cfg.Update(func(c *config.Config) { c.Recorder.Enabled = enabled }); err != nil {
		a.err = err.Error()
		a.cfgNow = a.cfg.Get()
		return
	}
	a.applyRecorder()
	if enabled {
		a.runner.Start()
	} else {
		a.runner.Stop()
	}
	a.status = a.runner.Status()
}
