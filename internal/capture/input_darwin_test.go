//go:build darwin

/*
 * [INPUT]: 依赖 macOS 的只读输入、前台应用和锁屏查询。
 * [OUTPUT]: 验证系统观测可调用且返回合理值。
 * [POS]: 平台桥接冒烟测试，不请求权限或抓屏。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"runtime"
	"sync"
	"testing"
)

func TestNativeObservationsSmoke(t *testing.T) {
	n := nativeObservations{}
	key, click, scroll := n.Intervals()
	front := n.Front()
	locked := n.Locked()
	t.Logf("输入间隔: key=%v click=%v scroll=%v；前台: app=%q bundle=%q pid=%d；锁屏: %v", key, click, scroll, front.App, front.Bundle, front.PID, locked)
	if key < 0 || click < 0 || scroll < 0 || front.PID < 0 {
		t.Fatal("系统观测值无效")
	}
}

func TestPoolFrontLockedStress(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := nativeObservations{}
			for j := 0; j < 3000; j++ {
				n.Front()
				runtime.Gosched()
				n.Locked()
				if j%32 == 0 {
					b := make([]byte, 256*1024)
					b[j%len(b)] = byte(j)
					runtime.KeepAlive(b)
				}
			}
		}()
	}
	wg.Wait()
}
