//go:build darwin

/*
 * [INPUT]: 依赖 CoreGraphics、ImageIO、Vision 与 Objective-C 运行时。
 * [OUTPUT]: 提供窗口查询、位图缩放、JPEG 编码和文字识别原语。
 * [POS]: macOS 原生桥接层；screen_darwin.go 编排帧与资源生命周期。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// 通过 purego 直接和 Objective-C runtime 说话，不用 cgo。
// 好处：交叉编译干净、二进制小，和 mygo 走的是同一条路。

var (
	msgSend       uintptr
	msgSendDouble func(uintptr, uintptr, float64) uintptr
	imgFn         func(disp uint32) uintptr
	dispFn        func() uint32
	cgListFn      func(opt, rel, winid uint32) uintptr

	cgGetW func(img uintptr) int
	cgGetH func(img uintptr) int

	cgColorSpace     func() uintptr
	cgBitmapCreate   func(unsafe.Pointer, int, int, int, int, uintptr, uint32) uintptr
	cgBitmapImage    func(uintptr) uintptr
	cgBitmapData     func(uintptr) unsafe.Pointer
	cgContextDraw    func(uintptr, cgRect, uintptr)
	cgContextQuality func(uintptr, int32)
	cfRetain         func(uintptr) uintptr

	destNew func(data, uti, count uintptr, opts uintptr) uintptr
	destAdd func(dest, img, props uintptr)
	destFin func(dest uintptr) uint8
)

func initRuntime() {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	msgSend, err = purego.Dlsym(lib, "objc_msgSend")
	if err != nil {
		panic(err)
	}
	purego.RegisterFunc(&msgSendDouble, msgSend)
	for _, fw := range []string{
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		"/System/Library/Frameworks/AppKit.framework/AppKit",
		"/System/Library/Frameworks/Vision.framework/Vision",
	} {
		if _, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			panic(fw + ": " + err.Error())
		}
	}
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&cgListFn, cg, "CGWindowListCopyWindowInfo")
	purego.RegisterLibFunc(&dispFn, cg, "CGMainDisplayID")
	purego.RegisterLibFunc(&imgFn, cg, "CGDisplayCreateImage")

	purego.RegisterLibFunc(&cgGetW, cg, "CGImageGetWidth")
	purego.RegisterLibFunc(&cgGetH, cg, "CGImageGetHeight")
	mustReg(cg, "CGColorSpaceCreateDeviceRGB", &cgColorSpace)
	mustReg(cg, "CGBitmapContextCreate", &cgBitmapCreate)
	mustReg(cg, "CGBitmapContextCreateImage", &cgBitmapImage)
	mustReg(cg, "CGBitmapContextGetData", &cgBitmapData)
	mustReg(cg, "CGContextDrawImage", &cgContextDraw)
	mustReg(cg, "CGContextSetInterpolationQuality", &cgContextQuality)

	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	mustReg(cf, "CFRetain", &cfRetain)

	io, err := purego.Dlopen("/System/Library/Frameworks/ImageIO.framework/ImageIO", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	mustReg(io, "CGImageDestinationCreateWithData", &destNew)
	mustReg(io, "CGImageDestinationAddImage", &destAdd)
	mustReg(io, "CGImageDestinationFinalize", &destFin)
}

// O 发一条 ObjC 消息。指针参数经 uintptr 转换，靠 go:uintptrescapes 保活。
//
//go:uintptrescapes
func O(obj uintptr, sel string, args ...uintptr) uintptr {
	s := objc.SEL(objc.RegisterName(sel))
	var a [10]uintptr
	a[0], a[1] = obj, uintptr(s)
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSend, a[:n+2]...)
	return r
}

func mustReg(lib uintptr, name string, fn any) {
	sym, err := purego.Dlsym(lib, name)
	if err != nil {
		panic("dlsym " + name + ": " + err.Error())
	}
	purego.RegisterFunc(fn, sym)
}

func nativeGlobal(lib uintptr, name string) uintptr {
	sym, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(err)
	}
	return *(*uintptr)(unsafe.Pointer(sym))
}

func objcBool(v uintptr) bool { return uint8(v) != 0 }

func cls(name string) uintptr { return uintptr(objc.GetClass(name)) }

func mutableArray() uintptr {
	a := O(cls("NSMutableArray"), "array")
	return a
}

//go:uintptrescapes
func nsstr(s string) uintptr {
	c := append([]byte(s), 0)
	return O(cls("NSString"), "stringWithUTF8String:", uintptr(unsafe.Pointer(&c[0])))
}

func gostr(ns uintptr) string {
	if ns == 0 {
		return ""
	}
	c := O(ns, "UTF8String")
	if c == 0 {
		return ""
	}
	// 这里对 uintptr -> unsafe.Pointer 的转换 go vet 会警告，但在此安全：
	// c 是 ObjC 返回的 C 字符串，其生命周期由 autorelease pool 保证，
	// 读取发生在同一次调用之内，不存在 Go 堆移动问题。
	p := unsafe.Pointer(c) //nolint:govet
	n := 0
	for n < 1<<16 && *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

func nsInt(v uintptr) int64 {
	if v == 0 {
		return 0
	}
	return int64(O(v, "longLongValue"))
}

// frontWindow 取前台窗口。layer==0 的窗口是普通应用窗口，
// 顺带过滤掉菜单栏、Dock、输入法候选这些噪声。
func frontWindow() (app, title string, pid int) {
	arr := cgListFn(1|16, 0, 0) // OnScreenOnly | ExcludeDesktopElements
	if arr == 0 {
		return
	}
	defer cfRelease(arr)
	n := O(arr, "count")
	for i := uintptr(0); i < n; i++ {
		d := O(arr, "objectAtIndex:", i)
		if nsInt(O(d, "objectForKey:", nsstr("kCGWindowLayer"))) != 0 {
			continue
		}
		app = gostr(O(d, "objectForKey:", nsstr("kCGWindowOwnerName")))
		title = gostr(O(d, "objectForKey:", nsstr("kCGWindowName")))
		pid = int(nsInt(O(d, "objectForKey:", nsstr("kCGWindowOwnerPID"))))
		return
	}
	return
}

// grab 抓一帧屏幕，返回原始 CGImage。
func grab() uintptr { return imgFn(dispFn()) }

// cgRect 与 CoreGraphics 的 CGRect 使用相同的四个 CGFloat。
type cgRect struct{ X, Y, W, H float64 }

// 截屏 CGImage 的底层数据可为 BGRA，不能假定通道顺序。
// 此处由 CoreGraphics 绘入明确的 RGBA 目标，指纹只读取目标字节。
func bitmap(img uintptr, w, h int, createImage bool) ([]byte, uintptr) {
	if img == 0 || w <= 0 || h <= 0 {
		return nil, 0
	}
	space := cgColorSpace()
	if space == 0 {
		return nil, 0
	}
	defer cfRelease(space)
	ctx := cgBitmapCreate(nil, w, h, 8, w*4, space, 1|4<<12)
	if ctx == 0 {
		return nil, 0
	}
	defer cfRelease(ctx)
	cgContextQuality(ctx, 3)
	cgContextDraw(ctx, cgRect{W: float64(w), H: float64(h)}, img)
	if createImage {
		return nil, cgBitmapImage(ctx)
	}
	data := cgBitmapData(ctx)
	if data == nil {
		return nil, 0
	}
	pix := make([]byte, w*h*4)
	copy(pix, unsafe.Slice((*byte)(data), len(pix)))
	return pix, 0
}

func isBlank(img uintptr) bool {
	w, h := cgGetW(img), cgGetH(img)
	if w <= 0 || h <= 0 {
		return true
	}
	h = max(1, h*64/w)
	pix, _ := bitmap(img, 64, h, false)
	if len(pix) == 0 {
		return false
	}
	var sum uint64
	for i := 0; i < len(pix); i += 4 {
		sum += uint64(pix[i]) + uint64(pix[i+1]) + uint64(pix[i+2])
	}
	avg := float64(sum) / float64(len(pix)/4*3*255)
	return avg < 0.05 || avg > 0.98
}

func downscaleTo(img uintptr, limit int) uintptr {
	w, h := cgGetW(img), cgGetH(img)
	if w <= 0 || h <= 0 {
		return 0
	}
	if w <= limit && h <= limit {
		return img
	}
	if w >= h {
		h = max(1, h*limit/w)
		w = limit
	} else {
		w = max(1, w*limit/h)
		h = limit
	}
	_, scaled := bitmap(img, w, h, true)
	return scaled
}

// writeJPEGData 把 CGImage 编码成内存里的 JPEG NSData。
func writeJPEGData(img uintptr, quality float64) uintptr {
	if destNew == nil || img == 0 {
		return 0
	}
	data := O(cls("NSMutableData"), "data")
	if data == 0 {
		return 0
	}
	dest := destNew(data, nsstr("public.jpeg"), 1, 0)
	if dest == 0 {
		return 0
	}
	num := msgSendDouble(cls("NSNumber"), uintptr(objc.SEL(objc.RegisterName("numberWithDouble:"))), quality)
	props := O(cls("NSDictionary"), "dictionaryWithObject:forKey:",
		num, nsstr("kCGImageDestinationLossyCompressionQuality"))
	defer cfRelease(dest)
	destAdd(dest, img, props)
	if destFin(dest) == 0 {
		return 0
	}
	return data
}

// ocrImage 用 Vision 的 accurate 模式识别原始 CGImage。
func ocrImage(data uintptr, langs []string) (string, int, error) {
	if data == 0 {
		return "", 0, errors.New("Vision 识别初始化失败")
	}
	req := O(O(cls("VNRecognizeTextRequest"), "alloc"), "init")
	defer O(req, "release")
	O(req, "setRecognitionLevel:", 0) // accurate
	O(req, "setUsesLanguageCorrection:", 1)
	// 显式指定语言：不指定时 Vision 对中英混排的识别会明显退化

	if marr := mutableArray(); marr != 0 {
		for _, l := range langs {
			O(marr, "addObject:", nsstr(l))
		}
		O(req, "setRecognitionLanguages:", marr)
	}

	handler := O(O(cls("VNImageRequestHandler"), "alloc"), "initWithCGImage:options:", data, 0)
	if handler == 0 {
		return "", 0, errors.New("Vision 识别初始化失败")
	}
	defer O(handler, "release")
	if !objcBool(O(handler, "performRequests:error:", O(cls("NSArray"), "arrayWithObject:", req), 0)) {
		return "", 0, errors.New("Vision 识别失败")
	}

	results := O(req, "results")
	n := O(results, "count")
	var out []byte
	for i := uintptr(0); i < n; i++ {
		obs := O(results, "objectAtIndex:", i)
		cands := O(obs, "topCandidates:", 1)
		if O(cands, "count") == 0 {
			continue
		}
		out = append(out, gostr(O(O(cands, "objectAtIndex:", 0), "string"))...)
		out = append(out, '\n')
	}
	return string(out), int(n), nil
}

// 合成帧的来源是 Go 的 RGBA 像素，不是屏幕截图的 BGRA 缓冲区。
// 位图上下文复制这些字节，并返回需要释放的 CGImage。
func rgbaToImage(f *Frame) uintptr {
	if f == nil || f.W <= 0 || f.H <= 0 || len(f.RGBA) < f.W*f.H*4 {
		return 0
	}
	space := cgColorSpace()
	if space == 0 {
		return 0
	}
	defer cfRelease(space)
	ctx := cgBitmapCreate(nil, f.W, f.H, 8, f.W*4, space, 1|4<<12)
	if ctx == 0 {
		return 0
	}
	defer cfRelease(ctx)
	data := cgBitmapData(ctx)
	if data == nil {
		return 0
	}
	copy(unsafe.Slice((*byte)(data), f.W*f.H*4), f.RGBA)
	return cgBitmapImage(ctx)
}
