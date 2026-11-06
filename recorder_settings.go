/*
 * [INPUT]: 依赖 internal/config 的 Recorder 设置和 internal/capture 的 Settings 契约。
 * [OUTPUT]: 提供 recorderSettings 换算、applyRecorder 热更新和 setRecorderEnabled 总开关。
 * [POS]: 应用层唯一把设置换成采集参数的地方。capture 不依赖 config，单位换算（毫秒、MB）只在这里做。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"slices"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
)

func recorderSettings(r config.Recorder, appDir string) capture.Settings {
	ms := func(v int) time.Duration { return time.Duration(v) * time.Millisecond }
	return capture.Settings{
		Root:        r.Dir(appDir),
		Debounce:    ms(r.DebounceMs),
		Heartbeat:   ms(r.HeartbeatMs),
		Idle:        ms(r.IdleMs),
		TypingPause: ms(r.TypingPauseMs),
		ScreenPoll:  ms(r.ScreenPollMs),
		JPEGQuality: r.JPEGQuality,
		OCR: capture.OCRSettings{
			Enabled:   r.OCR.Enabled,
			Languages: slices.Clone(r.OCR.Languages),
			EveryN:    r.OCR.EveryN,
		},
		InputMonitor:  r.InputMonitor,
		SensitiveApps: slices.Clone(r.SensitiveApps),
		MaxStorage:    int64(r.MaxStorageMB) << 20,
		RetentionDays: r.RetentionDays,
	}
}

func (a *app) applyRecorder() {
	a.cfgNow = a.cfg.Get()
	a.runner.Apply(recorderSettings(a.cfgNow.Recorder, a.appDir))
}

func (a *app) setRecorderEnabled(enabled bool) {
	if a.clearRunning || a.runner == nil {
		return
	}
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
