package webpi

import (
	"image"
	"image/color"
)

// BT.601 limited range 변환 계수 (× 2^16) — 디코더(dwebp)의 역변환과 짝
const (
	yuvFix   = 16
	yuvHalf  = 1 << (yuvFix - 1)
	yR, yG   = 16839, 33059
	yB       = 6420
	yOffset  = 16 << yuvFix
	uR, uG   = -9719, -19081
	uB       = 28800
	vR, vG   = 28800, -24116
	vB       = -4684
	uvOffset = 128 << (yuvFix + 2) // 2×2 합(×4)에 대한 오프셋
	uvRound  = 1 << (yuvFix + 1)
)

// yuvPlanes는 매크로블록 배수로 채운 4:2:0 평면이다. 채움 영역은 가장자리 복제.
type yuvPlanes struct {
	w, h     int
	mbw, mbh int
	y, u, v  []uint8
	ys, cs   int // 휘도/색차 stride
}

func toYUV420(m image.Image) *yuvPlanes {
	b := m.Bounds()
	p := &yuvPlanes{w: b.Dx(), h: b.Dy()}
	p.mbw, p.mbh = (p.w+15)/16, (p.h+15)/16
	p.ys, p.cs = p.mbw*16, p.mbw*8
	p.y = make([]uint8, p.ys*p.mbh*16)
	p.u = make([]uint8, p.cs*p.mbh*8)
	p.v = make([]uint8, p.cs*p.mbh*8)
	rgb := rgbReader(m)
	for y := range p.mbh * 16 {
		for x := range p.mbw * 16 {
			r, g, bl := rgb(min(x, p.w-1), min(y, p.h-1))
			p.y[y*p.ys+x] = uint8((yR*r + yG*g + yB*bl + yuvHalf + yOffset) >> yuvFix)
		}
	}
	for y := range p.mbh * 8 {
		for x := range p.mbw * 8 {
			var sr, sg, sb int
			for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				r, g, bl := rgb(min(2*x+d[0], p.w-1), min(2*y+d[1], p.h-1))
				sr, sg, sb = sr+r, sg+g, sb+bl
			}
			p.u[y*p.cs+x] = clipUV(uR*sr + uG*sg + uB*sb)
			p.v[y*p.cs+x] = clipUV(vR*sr + vG*sg + vB*sb)
		}
	}
	return p
}

func clipUV(v int) uint8 {
	return clip8(int32((v + uvRound + uvOffset) >> (yuvFix + 2)))
}

// rgbReader는 원점 기준 좌표로 8비트 RGB를 읽는 함수를 돌려준다. 흔한 타입은 빠른 경로.
func rgbReader(m image.Image) func(x, y int) (int, int, int) {
	b := m.Bounds()
	switch img := m.(type) {
	case *image.RGBA:
		return func(x, y int) (int, int, int) {
			i := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			return int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
		}
	case *image.NRGBA:
		return func(x, y int) (int, int, int) {
			i := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			return int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
		}
	}
	return func(x, y int) (int, int, int) {
		c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
		return int(c.R), int(c.G), int(c.B)
	}
}
