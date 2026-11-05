/*
 * [INPUT]: 依赖全分辨率 RGBA 帧和固定去重阈值。
 * [OUTPUT]: 提供亮度指纹与帧重复判定。
 * [POS]: 采集与画面探测共用的纯图像比较边界。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"hash/fnv"
	"math"
)

type fingerprint struct {
	hash   uint32
	hist   [32]float64
	pixels []byte
}

func fingerprintFrame(f *Frame) *fingerprint {
	if f == nil || f.W <= 0 || f.H <= 0 {
		return nil
	}
	pixels := nativePixels(f)
	if pixels == nil {
		if len(f.RGBA) < f.W*f.H*4 {
			return nil
		}
		pixels = make([]byte, dedupWidth*dedupHeight*4)
		for y := 0; y < dedupHeight; y++ {
			for x := 0; x < dedupWidth; x++ {
				src := ((y*f.H/dedupHeight)*f.W + x*f.W/dedupWidth) * 4
				dst := (y*dedupWidth + x) * 4
				copy(pixels[dst:dst+4], f.RGBA[src:src+4])
			}
		}
	}
	p := &fingerprint{pixels: pixels}
	hash := fnv.New32a()
	for y := 0; y < dedupHeight; y++ {
		for x := 0; x < dedupWidth; x++ {
			dst := (y*dedupWidth + x) * 4
			lum := byte((77*int(pixels[dst]) + 150*int(pixels[dst+1]) + 29*int(pixels[dst+2])) >> 8)
			hash.Write([]byte{lum})
			p.hist[int(lum)/8]++
		}
	}
	p.hash = hash.Sum32()
	for i := range p.hist {
		p.hist[i] /= dedupWidth * dedupHeight
	}
	return p
}
func duplicate(a, b *fingerprint) bool {
	if a == nil || b == nil {
		return false
	}
	if a.hash == b.hash {
		return true
	}
	var h float64
	for i := range a.hist {
		d := math.Sqrt(a.hist[i]) - math.Sqrt(b.hist[i])
		h += d * d
	}
	if math.Sqrt(h/2) >= histogramThreshold {
		return false
	}
	changed := 0
	for i := 0; i < len(a.pixels); i += 4 {
		for c := 0; c < 3; c++ {
			d := int(a.pixels[i+c]) - int(b.pixels[i+c])
			if d > channelTolerance || d < -channelTolerance {
				changed++
				break
			}
		}
	}
	return float64(changed)/float64(dedupWidth*dedupHeight) < pixelDifferenceThreshold
}
