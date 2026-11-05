/*
 * [INPUT]: 依赖合成 RGBA 帧与去重阈值。
 * [OUTPUT]: 验证哈希、像素变化和直方图变化。
 * [POS]: 去重算法的无平台测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "testing"

func TestDedup(t *testing.T) {
	frame := func() *Frame {
		f := &Frame{W: 160, H: 90, RGBA: make([]byte, 160*90*4)}
		for i := 0; i < len(f.RGBA); i += 4 {
			v := byte(50)
			if (i/4)%2 == 0 {
				v = 200
			}
			f.RGBA[i] = v
			f.RGBA[i+1] = v
			f.RGBA[i+2] = v
			f.RGBA[i+3] = 255
		}
		return f
	}
	a := frame()
	b := frame()
	if !duplicate(fingerprintFrame(a), fingerprintFrame(b)) {
		t.Fatal("相同帧")
	}
	for i := 0; i < len(b.RGBA); i += 4 {
		v := byte(200)
		if (i/4)%2 == 0 {
			v = 50
		}
		b.RGBA[i] = v
		b.RGBA[i+1] = v
		b.RGBA[i+2] = v
	}
	if duplicate(fingerprintFrame(a), fingerprintFrame(b)) {
		t.Fatal("像素变化")
	}
	for i := 0; i < len(b.RGBA); i += 4 {
		b.RGBA[i] = 100
		b.RGBA[i+1] = 100
		b.RGBA[i+2] = 100
	}
	if duplicate(fingerprintFrame(a), fingerprintFrame(b)) {
		t.Fatal("直方图变化")
	}
}
