package webpi

import (
	"image"
	"math"
	"slices"
)

// 알파 평면 — VP8L 무손실로 압축해 ALPH 청크에 담는다 (WebP Container Specification).
// 판단 PD-20261101-ce327ff6: 알파는 libwebp처럼 VP8L 무손실로 압축한다
const (
	opaqueAlpha = 0xff
	// ALPH 헤더 바이트: 압축 방식(하위 2비트), 필터(그 위 2비트)
	alphaCompressionNone     = 0
	alphaCompressionLossless = 1
	alphaFilterShift         = 2
	// ALPH 필터: 없음, 가로, 세로, 기울기
	alphaFilterNone       = 0
	alphaFilterHorizontal = 1
	alphaFilterVertical   = 2
	alphaFilterGradient   = 3
	numAlphaFilters       = 4
	// 필터를 고를 때 엔트로피를 재는 행 간격
	alphaEstimateRowStep = 4
	// 고른 필터의 잔차가 픽셀당 이 비트를 넘으면(8이 완전한 잡음) 필터 없음 후보를 따로 인코딩하지 않는다
	alphaNoiseBitsPerPixel = 7
	// VP8L 색 인덱싱 변환 (변환 종류 2비트, 팔레트 크기−1 8비트)
	transformColorIndexing = 3
	transformTypeBits      = 2
	paletteSizeBits        = 8
	maxPaletteSize         = 256
	// 인덱스를 묶을 수 있는(한 바이트에 2개 이상) 팔레트 크기 상한
	maxBundledPalette = 16
	// ARGB에서 알파 값을 담는 초록 채널의 위치
	greenShift = 8
	// color.Color.RGBA()의 16비트 값을 8비트로 줄이는 이동량
	color16To8Shift = 8
)

// alphaPlane은 m의 알파 값을 행 우선으로 돌려준다. 모두 불투명이면 nil이다.
func alphaPlane(m image.Image) []uint8 {
	if o, ok := m.(interface{ Opaque() bool }); ok && o.Opaque() {
		return nil
	}
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]uint8, w*h)
	switch img := m.(type) {
	case *image.NRGBA:
		copyAlpha(out, img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y), w, h)
	case *image.RGBA:
		copyAlpha(out, img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y), w, h)
	default:
		for y := range h {
			for x := range w {
				_, _, _, a := m.At(b.Min.X+x, b.Min.Y+y).RGBA()
				out[y*w+x] = uint8(a >> color16To8Shift)
			}
		}
	}
	for _, a := range out {
		if a != opaqueAlpha {
			return out
		}
	}
	return nil
}

func copyAlpha(dst, pix []uint8, stride, off, w, h int) {
	const bpp, alphaIndex = 4, 3
	for y := range h {
		row := pix[off+y*stride:]
		for x := range w {
			dst[y*w+x] = row[x*bpp+alphaIndex]
		}
	}
}

// encodeAlpha는 ALPH 청크 본문을 만든다.
// 판단 PD-20261102-811150d4: 후보(필터 없음·리터럴만·원본 저장)를 실제 크기로 견줘 작은 쪽을 쓴다
func encodeAlpha(alpha []uint8, w, h int) []byte {
	// 압축해도 줄지 않는 잡음은 libwebp처럼 압축 없이 담는다 (헤더 바이트 하나 + 원본)
	if c := compressAlpha(alpha, w, h); len(c) <= len(alpha) {
		return c
	}
	return append([]byte{alphaCompressionNone}, alpha...)
}

// compressAlpha는 VP8L로 압축한 ALPH 본문이다. 값이 16개 이하면 인덱스를 묶는 팔레트를 쓰고
// (묶지 않은 원본이 더 작은 마스크도 있지만 픽셀이 8배라 시간이 몇 배 든다),
// 그보다 많으면 필터를 쓴다 — 묶지 못하는 팔레트는 필터보다 크게 나왔다.
func compressAlpha(alpha []uint8, w, h int) []byte {
	if countValues(alpha) <= maxBundledPalette {
		return encodeAlphaCandidate(alpha, w, h, alphaFilterNone, true)
	}
	// 잔차 엔트로피는 LZ77을 모른다. 경계가 부드러운 마스크는 필터 없이 윗줄을 통째로 복사하는 쪽이
	// 훨씬 작은데 엔트로피로는 기울기 필터가 뽑힌다 — 필터 없음도 인코딩해 작은 쪽을 쓴다
	// 잔차가 잡음에 가까우면 어느 쪽도 줄지 않으니 후보를 늘리지 않는다 (잡음 알파의 시간·메모리가 두 배가 된다)
	best, bitsPerPixel := bestAlphaFilter(alpha, w, h)
	filters := []int{best}
	if best != alphaFilterNone && bitsPerPixel < alphaNoiseBitsPerPixel {
		filters = append(filters, alphaFilterNone)
	}
	out := make([][]byte, len(filters))
	parallelRows(len(filters), func(_, i int) { out[i] = encodeAlphaCandidate(alpha, w, h, filters[i], false) })
	return slices.MinFunc(out, func(a, b []byte) int { return len(a) - len(b) })
}

func countValues(alpha []uint8) int {
	var seen [maxPaletteSize]bool
	n := 0
	for _, v := range alpha {
		if !seen[v] {
			seen[v] = true
			n++
		}
	}
	return n
}

// bestAlphaFilter는 필터마다 잔차 값의 엔트로피(비트 합)를 재 가장 낮은 필터와 그 픽셀당 비트를 돌려준다.
// 순위만 필요하므로 몇 행마다 한 행만 본다.
func bestAlphaFilter(alpha []uint8, w, h int) (int, float64) {
	var hist [numAlphaFilters][maxPaletteSize]int
	n := 0
	for y := 0; y < h; y += alphaEstimateRowStep {
		for x := range w {
			for f := range numAlphaFilters {
				hist[f][alpha[y*w+x]-alphaPred(alpha, w, x, y, f)]++
			}
		}
		n += w
	}
	best, bestBits := alphaFilterNone, math.MaxFloat64
	for f := range numAlphaFilters {
		if b := entropyBits(hist[f][:], n); b < bestBits {
			best, bestBits = f, b
		}
	}
	return best, bestBits / float64(n)
}

func entropyBits(hist []int, total int) float64 {
	var bitsSum float64
	for _, c := range hist {
		if c > 0 {
			bitsSum -= float64(c) * math.Log2(float64(c)/float64(total))
		}
	}
	return bitsSum
}

func encodeAlphaCandidate(alpha []uint8, w, h, filter int, palette bool) []byte {
	bw := &bitWriter{buf: []byte{byte(filter<<alphaFilterShift | alphaCompressionLossless)}}
	vals := filterAlpha(alpha, w, h, filter)
	argb, width := make([]uint32, len(vals)), w
	for i, v := range vals {
		argb[i] = uint32(v) << greenShift
	}
	if palette {
		argb, width = writeColorIndexing(bw, vals, w, h)
	}
	// 변환 끝
	bw.put(0, 1)
	writeEntropyImage(bw, argb, width, true)
	return bw.bytes()
}

// filterAlpha는 ALPH 필터로 예측값을 뺀 평면을 돌려준다 (mod 256).
func filterAlpha(alpha []uint8, w, h, filter int) []uint8 {
	if filter == alphaFilterNone {
		return alpha
	}
	out := make([]uint8, len(alpha))
	for y := range h {
		for x := range w {
			out[y*w+x] = alpha[y*w+x] - alphaPred(alpha, w, x, y, filter)
		}
	}
	return out
}

// alphaPred는 (x, y)의 필터 예측값이다. 맨 왼쪽 위는 0, 첫 행은 왼쪽, 첫 열은 위 픽셀로 예측한다.
func alphaPred(alpha []uint8, w, x, y, filter int) uint8 {
	i := y*w + x
	switch {
	case filter == alphaFilterNone || (x == 0 && y == 0):
		return 0
	case y == 0:
		return alpha[i-1]
	case x == 0:
		return alpha[i-w]
	}
	return alphaPredict(filter, alpha[i-1], alpha[i-w], alpha[i-w-1])
}

func alphaPredict(filter int, left, top, topLeft uint8) uint8 {
	switch filter {
	case alphaFilterHorizontal:
		return left
	case alphaFilterVertical:
		return top
	}
	return clip8(int32(left) + int32(top) - int32(topLeft))
}

// writeColorIndexing은 색 인덱싱 변환을 쓰고, 인덱스로 바꿔 묶은 픽셀과 그 폭을 돌려준다.
// 값이 16개 이하면 한 픽셀(바이트)에 2·4·8개 인덱스를 묶는다.
func writeColorIndexing(bw *bitWriter, vals []uint8, w, h int) ([]uint32, int) {
	var seen [maxPaletteSize]bool
	for _, v := range vals {
		seen[v] = true
	}
	var pal []uint8
	var index [maxPaletteSize]uint32
	for v, ok := range seen {
		if ok {
			index[v] = uint32(len(pal))
			pal = append(pal, uint8(v))
		}
	}
	bw.put(1, 1)
	bw.put(transformColorIndexing, transformTypeBits)
	bw.put(uint32(len(pal)-1), paletteSizeBits)
	// 팔레트는 앞 항목과의 차이로 부속 이미지(폭 = 팔레트 크기, 높이 1)에 담는다
	delta := make([]uint32, len(pal))
	for i, v := range pal {
		d := v
		if i > 0 {
			d = v - pal[i-1]
		}
		delta[i] = uint32(d) << greenShift
	}
	writeEntropyImage(bw, delta, len(pal), false)
	return packIndices(vals, &index, w, h, bundleBits(len(pal)))
}

// bundleBits는 팔레트 크기에 따라 한 바이트에 묶을 인덱스 수의 log2다 (2색 3, 4색 2, 16색 1, 그 위 0).
func bundleBits(n int) int {
	switch {
	case n <= 2:
		return 3
	case n <= 4:
		return 2
	case n <= maxBundledPalette:
		return 1
	}
	return 0
}

func packIndices(vals []uint8, index *[maxPaletteSize]uint32, w, h, xbits int) ([]uint32, int) {
	per := 1 << xbits
	bpp := byteBits >> xbits
	pw := (w + per - 1) >> xbits
	out := make([]uint32, pw*h)
	for y := range h {
		for x := range w {
			out[y*pw+x>>xbits] |= index[vals[y*w+x]] << (bpp * (x & (per - 1)))
		}
	}
	for i := range out {
		out[i] <<= greenShift
	}
	return out, pw
}
