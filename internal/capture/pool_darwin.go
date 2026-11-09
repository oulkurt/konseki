//go:build darwin

/*
 * [INPUT]: 依赖 Objective-C 运行时的线程局部自动释放池。
 * [OUTPUT]: 提供绑定系统线程的 withPool。
 * [POS]: 后台原生调用的生命周期边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

var poolOnce sync.Once
var poolPush, poolPop uintptr

func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	poolOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW)
		if err != nil {
			panic(err)
		}
		poolPush, err = purego.Dlsym(lib, "objc_autoreleasePoolPush")
		if err != nil {
			panic(err)
		}
		poolPop, err = purego.Dlsym(lib, "objc_autoreleasePoolPop")
		if err != nil {
			panic(err)
		}
	})
	p, _, _ := purego.SyscallN(poolPush)
	defer purego.SyscallN(poolPop, p)
	fn()
}
