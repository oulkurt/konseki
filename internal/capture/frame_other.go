//go:build !darwin

/*
 * [INPUT]: 依赖跨平台 RGBA 帧。
 * [OUTPUT]: 提供帧复制、释放和原生像素适配桩。
 * [POS]: 非 macOS 平台的帧生命周期边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

func retainFrame(f *Frame) *Frame {
	if f == nil {
		return nil
	}
	c := *f
	return &c
}
func releaseFrame(*Frame)        {}
func nativePixels(*Frame) []byte { return nil }
