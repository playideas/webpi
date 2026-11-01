package webpi

import (
	"image"
	"image/color"
	"math"
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
	read := rgbRowReader(m)
	// 색차 한 행은 휘도 두 행에서 나오므로 두 행씩 묶어 읽는다
	n := p.mbh * 8
	bufs := make([][2][]uint8, rowWorkers(n))
	for w := range bufs {
		bufs[w] = [2][]uint8{make([]uint8, p.ys*rgbBytes), make([]uint8, p.ys*rgbBytes)}
	}
	parallelRows(n, func(w, cy int) {
		rows := &bufs[w]
		for k := range rows {
			read(min(2*cy+k, p.h-1), rows[k])
			lumaRow(p.y[(2*cy+k)*p.ys:(2*cy+k+1)*p.ys], rows[k])
		}
		chromaRow(p.u[cy*p.cs:(cy+1)*p.cs], p.v[cy*p.cs:(cy+1)*p.cs], rows)
	})
	return p
}

const rgbBytes = 3

func lumaRow(dst, rgb []uint8) {
	for x := range dst {
		r, g, bl := int(rgb[x*rgbBytes]), int(rgb[x*rgbBytes+1]), int(rgb[x*rgbBytes+2])
		dst[x] = uint8((yR*r + yG*g + yB*bl + yuvHalf + yOffset) >> yuvFix)
	}
}

func chromaRow(u, v []uint8, rows *[2][]uint8) {
	for x := range u {
		var s [rgbBytes]int
		for _, row := range rows {
			for c := range rgbBytes {
				s[c] += gammaToLinear[row[2*x*rgbBytes+c]] + gammaToLinear[row[(2*x+1)*rgbBytes+c]]
			}
		}
		sr, sg, sb := linearToGamma(s[0]), linearToGamma(s[1]), linearToGamma(s[2])
		u[x] = clipUV(uR*sr + uG*sg + uB*sb)
		v[x] = clipUV(vR*sr + vG*sg + vB*sb)
	}
}

// 색차 2×2 평균은 감마를 푼 선형 공간에서 낸다 — 밝기 경계에서 평균이 어두운 쪽으로 쏠려
// 색이 번지는 것을 줄인다. 상수와 고정소수점 처리는 libwebp picture_csp_enc.c와 같다.
const (
	gammaExp    = 0.80
	gammaFix    = 12 // 선형 값의 정밀도
	gammaTabFix = 7  // 역변환 표 보간의 정밀도
	gammaScale  = 1<<gammaFix - 1
	gammaTabLen = 1 << (gammaFix - gammaTabFix)
	// 네 값의 합(×4)을 받으므로 보간 분수부는 2비트 더 넓다
	gammaFracBits = gammaTabFix + 2
	gammaFracMask = 1<<gammaFracBits - 1
)

var gammaToLinear, linearToGammaTab = func() (g [256]int, l [gammaTabLen + 1]int) {
	for v := range g {
		g[v] = int(math.Pow(float64(v)/maxPixel, gammaExp)*gammaScale + 0.5)
	}
	scale := float64(1<<gammaTabFix) / gammaScale
	for v := range l {
		l[v] = int(maxPixel*math.Pow(scale*float64(v), 1/gammaExp) + 0.5)
	}
	return g, l
}()

const maxPixel = 255

// linearToGamma는 선형 값 네 개의 합을 감마 공간 값 네 개의 합(×4) 척도로 되돌린다.
func linearToGamma(sum int) int {
	pos, frac := sum>>gammaFracBits, sum&gammaFracMask
	y := linearToGammaTab[pos+1]*frac + linearToGammaTab[pos]*(gammaFracMask+1-frac)
	return (y + 1<<(gammaTabFix-1)) >> gammaTabFix
}

func clipUV(v int) uint8 {
	return clip8(int32((v + uvRound + uvOffset) >> (yuvFix + 2)))
}

// rgbRowReader는 원점 기준 행 y를 RGB 3바이트씩 dst에 채우는 함수를 돌려준다.
// dst가 이미지보다 넓으면 마지막 픽셀을 복제한다. 흔한 타입은 Pix를 직접 읽는다.
func rgbRowReader(m image.Image) func(y int, dst []uint8) {
	b := m.Bounds()
	w := b.Dx()
	fill := func(dst []uint8, px func(x int) (uint8, uint8, uint8)) {
		for x := range len(dst) / rgbBytes {
			dst[x*rgbBytes], dst[x*rgbBytes+1], dst[x*rgbBytes+2] = px(min(x, w-1))
		}
	}
	switch img := m.(type) {
	case *image.RGBA:
		if !img.Opaque() {
			// 알파가 곱해진 색이라 반투명 픽셀은 나눠서 되돌린다 — WebP는 곱하지 않은 색을 담는다
			return func(y int, dst []uint8) { copyRGBUnpremultiplied(dst, img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):], w) }
		}
		return func(y int, dst []uint8) { copyRGB(dst, img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):], w) }
	case *image.NRGBA:
		return func(y int, dst []uint8) { copyRGB(dst, img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):], w) }
	case *image.YCbCr:
		// JPEG 디코더의 출력 — At은 픽셀마다 인터페이스 값을 할당하므로 피한다
		return func(y int, dst []uint8) {
			fill(dst, func(x int) (uint8, uint8, uint8) {
				c := img.YCbCrAt(b.Min.X+x, b.Min.Y+y)
				return color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
			})
		}
	}
	return func(y int, dst []uint8) {
		fill(dst, func(x int) (uint8, uint8, uint8) {
			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			return c.R, c.G, c.B
		})
	}
}

// copyRGB는 RGBA 배치(4바이트) 행의 앞 w픽셀에서 알파를 버리고 dst에 옮긴 뒤, 남은 폭은 마지막 픽셀로 채운다.
func copyRGB(dst, row []uint8, w int) {
	const bpp = 4
	for x := range len(dst) / rgbBytes {
		i := min(x, w-1) * bpp
		dst[x*rgbBytes], dst[x*rgbBytes+1], dst[x*rgbBytes+2] = row[i], row[i+1], row[i+2]
	}
}

// copyRGBUnpremultiplied는 copyRGB와 같되 알파가 곱해진 색을 color.NRGBAModel과 같은 정수 계산으로 되돌린다.
func copyRGBUnpremultiplied(dst, row []uint8, w int) {
	const bpp, alphaIndex, widen, full16 = 4, 3, 0x101, 0xffff
	for x := range len(dst) / rgbBytes {
		i := min(x, w-1) * bpp
		a := uint32(row[i+alphaIndex]) * widen
		for c := range rgbBytes {
			v := row[i+c]
			if a != 0 && a != full16 {
				v = uint8(uint32(v) * widen * full16 / a >> 8)
			}
			dst[x*rgbBytes+c] = v
		}
	}
}
