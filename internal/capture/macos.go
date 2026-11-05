//go:build darwin

/*
 * [INPUT]: 依赖平台屏幕与文字识别能力。
 * [OUTPUT]: 提供采集接口的平台实现。
 * [POS]: 采集模块的平台适配，与外壳隔离。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// 通过 purego 直接和 Objective-C runtime 说话，不用 cgo。
// 好处：交叉编译干净、二进制小，和 mygo 走的是同一条路。

const (
	thumbWidth  = 1200 // 磁盘上缩略图的宽度
	jpegQuality = 0.60 // 缩略图的 JPEG 质量
)

var (
	msgSend  uintptr
	imgFn    func(disp uint32) uintptr
	dispFn   func() uint32
	cgListFn func(opt, rel, winid uint32) uintptr

	cgGetW func(img uintptr) int
	cgGetH func(img uintptr) int

	cgDataProvider         func(img uintptr) uintptr
	cgDataProviderCopyData func(prov uintptr) uintptr
	cgBitsPerPixel         func(img uintptr) uint32
	cgBytesPerRow          func(img uintptr) int
	cfDataLen              func(d uintptr) int
	cfDataBytePtr          func(d uintptr) uintptr

	destNew        func(data, uti, count uintptr, opts uintptr) uintptr
	destAdd        func(dest, img, props uintptr)
	destFin        func(dest uintptr) uint8
	imgSrcFromData func(data uintptr, idx int) uintptr
	imgSrcThumb    func(src uintptr, idx int, opts uintptr) uintptr
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
	purego.RegisterLibFunc(&cgDataProvider, cg, "CGImageGetDataProvider")
	purego.RegisterLibFunc(&cgDataProviderCopyData, cg, "CGDataProviderCopyData")
	purego.RegisterLibFunc(&cgBitsPerPixel, cg, "CGImageGetBitsPerPixel")
	purego.RegisterLibFunc(&cgBytesPerRow, cg, "CGImageGetBytesPerRow")

	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	mustReg(cf, "CFDataGetLength", &cfDataLen)
	mustReg(cf, "CFDataGetBytePtr", &cfDataBytePtr)

	io, err := purego.Dlopen("/System/Library/Frameworks/ImageIO.framework/ImageIO", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	mustReg(io, "CGImageDestinationCreateWithData", &destNew)
	mustReg(io, "CGImageDestinationAddImage", &destAdd)
	mustReg(io, "CGImageDestinationFinalize", &destFin)
	mustReg(io, "CGImageSourceCreateWithData", &imgSrcFromData)
	mustReg(io, "CGImageSourceCreateThumbnailAtIndex", &imgSrcThumb)
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

// ocrImage 在全分辨率的 CGImage 上做 OCR。
//
// 这是关键设计：OCR 用全分辨率原图（内存里的 CGImage，不落盘），
// 磁盘上只留 1200px 的缩略图当视觉参考。
// 于是「识别准」和「占空间小」不再互相牺牲——
// 实测缩到 1400px 再 OCR，中文界面会糊成 "'•¥s.JJ •.'# *\""。
func ocrImage(img uintptr, languages []string) (string, int) {
	data := writeJPEGData(img, 0.92)
	if data == 0 {
		return "", 0
	}
	return ocrData(data, languages)
}

// isBlank 判断是不是黑屏/白屏。
//
// 为什么要这个：macOS 在锁屏、屏幕保护、或权限刚被吊销时会返回一张
// 全黑图。实测遇到过 —— 落盘 19KB、OCR 0 行、像素均值 2。
// 不拦的话会攒一堆无意义的「黑屏」记录，还会污染时长统计。
//
// 实现走 CGImage 的 dataProvider，直接读原始像素缓冲区：
// NSBitmapImageRep 的 bitmapData 在小尺寸下会返回无效指针（实测 SIGSEGV），
// 而 CGImageGetDataProvider + CFDataGetBytePtr 是所有尺寸都可靠的路径。
func isBlank(img uintptr) bool {
	if img == 0 {
		return true
	}
	small := downscaleTo(img, 64)
	if small == 0 || cfDataLen == nil {
		return false
	}
	prov := cgDataProvider(small)
	if prov == 0 {
		return false
	}
	data := cgDataProviderCopyData(prov)
	if data == 0 {
		return false
	}
	n := cfDataLen(data)
	if n < 16 {
		return false
	}
	ptr := cfDataBytePtr(data)
	if ptr == 0 {
		return false
	}

	bpp := int64(cgBitsPerPixel(small)) / 8 // 每像素字节数
	if bpp <= 0 {
		bpp = 4
	}
	rowBytes := int64(cgBytesPerRow(small))
	if rowBytes <= 0 {
		return false
	}
	height := int64(cgGetH(small))

	// 同上：ptr 来自 CFDataGetBytePtr，指向 CFData 的内部缓冲区，
	// CFData 在本函数作用域内被 data 变量持有，不会被释放。
	p := unsafe.Pointer(ptr) //nolint:govet
	var sum, cnt uint64
	// 稀疏采样：每 4 行、每 16 像素取一个
	for y := int64(0); y < height; y += 4 {
		row := unsafe.Add(p, y*rowBytes)
		for x := int64(0); x*3*bpp < rowBytes; x += 16 {
			off := unsafe.Add(row, x*bpp)
			if uintptr(off)-uintptr(p) >= uintptr(n)-4 {
				break
			}
			// 实测 CGDisplayCreateImage 出来的是 BGRA：
			// 全黑屏的字节序列是 255 2 2 2（alpha 在最前，然后是 BGR）。
			// 所以取的是 +1/+2/+3，跳过 alpha —— 否则 alpha=255
			// 会把均值拉到 0.34 左右，黑屏永远判定不出来。
			c0 := *(*byte)(unsafe.Add(off, 1))
			c1 := *(*byte)(unsafe.Add(off, 2))
			c2 := *(*byte)(unsafe.Add(off, 3))
			sum += uint64(c0) + uint64(c1) + uint64(c2)
			cnt += 3
		}
	}
	if cnt == 0 {
		return false
	}
	avg := float64(sum) / float64(cnt) / 255.0
	return avg < 0.05 || avg > 0.98
}

// saveThumb 把 CGImage 缩成小图存盘，只作为「那天屏幕长什么样」的凭证。
func saveThumb(img uintptr, path string) bool {
	small := downscale(img)
	if small == 0 {
		return false
	}
	return writeJPEG(small, path)
}

// writeJPEG 用 ImageIO 的 CGImageDestination 编码到文件。
func writeJPEG(img uintptr, path string) bool {
	data := writeJPEGData(img, jpegQuality)
	if data == 0 {
		return false
	}
	return O(data, "writeToFile:atomically:", nsstr(path), 1) != 0
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
	var q [1]float64
	q[0] = quality
	num := O(cls("NSNumber"), "numberWithDouble:", *(*uintptr)(unsafe.Pointer(&q[0])))
	props := O(cls("NSDictionary"), "dictionaryWithObject:forKey:",
		num, nsstr("kCGImageDestinationLossyCompressionQuality"))
	destAdd(dest, img, props)
	if destFin(dest) == 0 {
		return 0
	}
	return data
}

// downscaleTo 缩到指定宽度。
func downscaleTo(img uintptr, targetW int) uintptr {
	w, h := cgGetW(img), cgGetH(img)
	if h == 0 {
		return 0
	}
	if w <= targetW {
		return img
	}
	full := writeJPEGData(img, 0.85)
	if full == 0 {
		return 0
	}
	src := imgSrcFromData(full, 0)
	if src == 0 {
		return 0
	}
	opts := O(cls("NSMutableDictionary"), "dictionary")
	O(opts, "setObject:forKey:", O(cls("NSNumber"), "numberWithInt:", uintptr(targetW)), nsstr("kCGImageSourceThumbnailMaxPixelSize"))
	O(opts, "setObject:forKey:", O(cls("NSNumber"), "numberWithBool:", 1), nsstr("kCGImageSourceCreateThumbnailFromImageAlways"))
	O(opts, "setObject:forKey:", O(cls("NSNumber"), "numberWithBool:", 0), nsstr("kCGImageSourceCreateThumbnailWithTransform"))
	return imgSrcThumb(src, 0, opts)
}

// downscale 用 ImageIO 的缩略图接口，直接给个小 CGImage，不碰 AppKit 重绘。
func downscale(img uintptr) uintptr {
	w, h := cgGetW(img), cgGetH(img)
	if h == 0 {
		return 0
	}
	if w <= thumbWidth {
		return img
	}
	full := writeJPEGData(img, 0.9)
	if full == 0 {
		return 0
	}
	src := imgSrcFromData(full, 0)
	if src == 0 {
		return 0
	}
	opts := O(cls("NSMutableDictionary"), "dictionary")
	O(opts, "setObject:forKey:", O(cls("NSNumber"), "numberWithInt:", thumbWidth), nsstr("kCGImageSourceThumbnailMaxPixelSize"))
	O(opts, "setObject:forKey:", O(cls("NSNumber"), "numberWithBool:", 1), nsstr("kCGImageSourceCreateThumbnailFromImageAlways"))
	O(opts, "setObject:forKey:", O(cls("NSNumber"), "numberWithBool:", 0), nsstr("kCGImageSourceCreateThumbnailWithTransform"))
	return imgSrcThumb(src, 0, opts)
}

// ocrData 用 Vision 框架识别文字。accurate 模式，中英日都能认。
func ocrData(data uintptr, langs []string) (string, int) {
	if data == 0 {
		return "", 0
	}
	req := O(O(cls("VNRecognizeTextRequest"), "alloc"), "init")
	O(req, "setRecognitionLevel:", 1) // accurate
	O(req, "setUsesLanguageCorrection:", 1)
	// 显式指定语言：不指定时 Vision 对中英混排的识别会明显退化

	if marr := mutableArray(); marr != 0 {
		for _, l := range langs {
			O(marr, "addObject:", nsstr(l))
		}
		O(req, "setRecognitionLanguages:", marr)
	}

	handler := O(O(cls("VNImageRequestHandler"), "alloc"), "initWithData:options:", data, 0)
	if handler == 0 {
		return "", 0
	}
	O(handler, "performRequests:error:", O(cls("NSArray"), "arrayWithObject:", req), 0)

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
	return string(out), int(n)
}

// ---------- Frame 与 CGImage 的互转 ----------
//
// 接口层用 RGBA 统一各平台，这里负责和 CGImage 的原生 BGRA 互转。
// 记住：CGDisplayCreateImage 的缓冲区是 BGRA 且 alpha 在最前，
// 详见 docs/findings.md 第 4 节。

// imageToRGBA 把 CGImage 读成紧凑的 RGBA 切片。
func imageToRGBA(img uintptr) []byte {
	w, h := int(cgGetW(img)), int(cgGetH(img))
	if w == 0 || h == 0 || cgDataProvider == nil {
		return nil
	}
	prov := cgDataProvider(img)
	if prov == 0 {
		return nil
	}
	data := cgDataProviderCopyData(prov)
	if data == 0 {
		return nil
	}
	n := cfDataLen(data)
	ptr := cfDataBytePtr(data)
	if ptr == 0 || n < 16 {
		return nil
	}
	rowBytes := int(cgBytesPerRow(img))
	bpp := int(cgBitsPerPixel(img)) / 8
	if rowBytes <= 0 || bpp < 3 {
		return nil
	}

	out := make([]byte, w*h*4)
	// uintptr -> unsafe.Pointer 的转换，go vet 会警告。此处安全：
	// ptr 是 CFDataGetBytePtr 返回的内部缓冲区指针，CFData 由 data
	// 变量持有到函数结束，期间不会被释放或移动。读操作全在本函数内。
	src := unsafe.Pointer(ptr) //nolint:govet
	for y := 0; y < h; y++ {
		row := unsafe.Add(src, y*rowBytes)
		for x := 0; x < w; x++ {
			off := unsafe.Add(row, x*bpp)
			if uintptr(off)-uintptr(src) >= uintptr(n)-4 {
				return out
			}
			// BGRA -> RGBA
			b := *(*byte)(off)
			g := *(*byte)(unsafe.Add(off, 1))
			r := *(*byte)(unsafe.Add(off, 2))
			a := *(*byte)(unsafe.Add(off, 3))
			d := (y*w + x) * 4
			out[d] = r
			out[d+1] = g
			out[d+2] = b
			out[d+3] = a
		}
	}
	return out
}

// rgbaToImage 把 Frame 重新组装成 CGImage，交给 Vision 做 OCR。
//
// 绕一圈似乎多余，但这样主循环就完全不依赖平台原生句柄，
// 而 OCR 用全分辨率这一关键设计仍然成立。
func rgbaToImage(f *Frame) uintptr {
	if f == nil || len(f.RGBA) < f.W*f.H*4 || f.W == 0 || f.H == 0 {
		return 0
	}
	// 用 NSBitmapImageRep 从 RGBA 像素构造，再取 CGImage
	rep := O(O(cls("NSBitmapImageRep"), "alloc"), "initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:",
		0, uintptr(f.W), uintptr(f.H), 8, 4, 1, 0, nsstr("NSDeviceRGBColorSpace"), uintptr(f.W*4), 32)
	if rep == 0 {
		return 0
	}
	dst := O(rep, "bitmapData")
	if dst == 0 {
		return 0
	}
	// 直接拷贝 RGBA 进 rep 的缓冲区。
	// 同上：dst 是 NSBitmapImageRep 的内部像素缓冲区，rep 在作用域内被持有，
	// 拷贝立即完成，不存在悬垂风险。
	copy(unsafe.Slice((*byte)(unsafe.Pointer(dst)), f.W*f.H*4), f.RGBA) //nolint:govet
	// 让 rep 重新解析像素
	O(rep, "setSize:", 0)
	cg := O(rep, "CGImage")
	if cg == 0 {
		return 0
	}
	return cg
}
