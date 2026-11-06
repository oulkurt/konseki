/*
 * [INPUT]: 依赖临时应用配置和采集参数字段表。
 * [OUTPUT]: 验证字段提交、归一化与采集参数映射。
 * [POS]: 记录设置参数边界的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"testing"
	"time"
)

func TestRecorderFieldCommit(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	for i, tc := range []struct{ input, want int }{
		{100, 3000}, {310000, 300000}, {45000, 45000}, {1500, 1500}, {500, 10000}, {58, 60},
	} {
		a.commitRecorderField(i, tc.input)
		if got := recorderFields[i].get(a.cfg.Get().Recorder); got != tc.want {
			t.Errorf("%s: 得到 %d，期望 %d", recorderFields[i].key, got, tc.want)
		}
	}
	s := recorderSettings(a.cfg.Get().Recorder, a.appDir)
	if s.Debounce != 3*time.Second || s.Heartbeat != 300*time.Second || s.Idle != 45*time.Second || s.TypingPause != 1500*time.Millisecond || s.ScreenPoll != 10*time.Second || s.JPEGQuality != 60 {
		t.Fatalf("采集参数映射错误: %+v", s)
	}
}
