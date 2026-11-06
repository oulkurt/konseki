/*
 * [INPUT]: 依赖可注入的清除步骤和摘要完成信号。
 * [OUTPUT]: 验证清除顺序、等待和错误短路。
 * [POS]: 危险操作的并发和失败路径回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestClearSequenceWaitsAndStopsOnFailure(t *testing.T) {
	done := make(chan struct{})
	started := make(chan struct{})
	result := make(chan error, 1)
	var steps []int
	failure := errors.New("summary failure")
	go func() {
		close(started)
		result <- clearSequence(done, func() error { steps = append(steps, 1); return nil }, func() error { steps = append(steps, 2); return failure }, func() error { steps = append(steps, 3); return nil })
	}()
	<-started
	select {
	case <-result:
		t.Fatal("摘要退出之前完成清除")
	case <-time.After(10 * time.Millisecond):
	}
	close(done)
	if err := <-result; !errors.Is(err, failure) {
		t.Fatalf("错误 = %v", err)
	}
	if !reflect.DeepEqual(steps, []int{1, 2}) {
		t.Fatalf("顺序 = %v", steps)
	}
}
