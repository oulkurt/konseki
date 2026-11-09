//go:build darwin

/*
 * [INPUT]: 依赖 macOS 的只读输入、前台应用和锁屏查询。
 * [OUTPUT]: 验证系统观测、累计键鼠计数和运行中应用列表；测量累计读数耗时。
 * [POS]: 平台桥接冒烟测试，不请求权限或抓屏。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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

// 登录会话里访达总在运行，列表不该为空；每一项都要有 bundle ID，并按名字排好。
func TestNativeRunningApps(t *testing.T) {
	apps := RunningApps()
	if !slices.ContainsFunc(apps, func(f Front) bool { return f.Bundle == "com.apple.finder" }) {
		t.Fatalf("运行中应用里没有访达：%v", apps)
	}
	for i, f := range apps {
		if f.Bundle == "" || f.PID <= 0 {
			t.Fatalf("第 %d 项缺少 bundle ID 或进程号：%+v", i, f)
		}
		if i > 0 && strings.ToLower(apps[i-1].App) > strings.ToLower(f.App) {
			t.Fatalf("没有按名字排序：%q 排在 %q 前面", apps[i-1].App, f.App)
		}
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

func TestNativeBoolAndAccessibilitySymbols(t *testing.T) {
	var _ func(int32, uint32) float64 = eventSeconds
	if objcBool(0x100) || !objcBool(0x101) {
		t.Fatal("BOOL 只能读取低八位")
	}
	initPlatform()
	if accessibilityWithOptions == nil || accessibilityPrompt == 0 {
		t.Fatal("辅助功能请求符号缺失")
	}
}

func BenchmarkLiveFront(b *testing.B) {
	source := nativeObservations{}
	source.LiveFront()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source.LiveFront()
	}
}

func TestNativeTotals(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		n := nativeObservations{}
		first, ok := n.Totals()
		if !ok {
			t.Error("累计读数不可用")
			return
		}
		second, ok := n.Totals()
		if !ok || second.Keys < first.Keys || second.Clicks < first.Clicks {
			t.Errorf("累计读数递减: %+v %+v", first, second)
		}
		start := time.Now()
		for i := 0; i < 1000; i++ {
			if _, ok := n.Totals(); !ok {
				t.Error("累计读数不可用")
				return
			}
		}
		us := float64(time.Since(start).Nanoseconds()) / 1000 / 1000
		t.Logf("Totals 平均 %.3f 微秒；每秒 4 拍 CPU %.6f%%", us, us*4/10000)
	}()
	<-done
}
