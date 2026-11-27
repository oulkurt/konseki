//go:build darwin

/*
 * [INPUT]: 依赖 Objective-C 运行时的线程局部自动释放池。
 * [OUTPUT]: 提供绑定系统线程的 withPool。
 * [POS]: 后台原生调用的生命周期边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	poolMu            sync.Mutex
	poolReady         bool
	poolPush, poolPop uintptr
)

func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	poolMu.Lock()
	if !poolReady {
		lib := dlopenChecked("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW)
		push, err := purego.Dlsym(lib, "objc_autoreleasePoolPush")
		if err != nil {
			poolMu.Unlock()
			panic(err)
		}
		if push == 0 {
			poolMu.Unlock()
			panic(errors.New("objc_autoreleasePoolPush returned 0"))
		}
		pop, err := purego.Dlsym(lib, "objc_autoreleasePoolPop")
		if err != nil {
			poolMu.Unlock()
			panic(err)
		}
		if pop == 0 {
			poolMu.Unlock()
			panic(errors.New("objc_autoreleasePoolPop returned 0"))
		}
		poolPush = push
		poolPop = pop
		poolReady = true
	}
	push, pop := poolPush, poolPop
	poolMu.Unlock()
	p, _, _ := purego.SyscallN(push)
	defer purego.SyscallN(pop, p)
	fn()
}
