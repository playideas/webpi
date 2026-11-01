package webpi

import "math/bits"

// VP8L 이미지 스트림 — LZ77 역참조와 접두 부호로 ARGB 픽셀을 압축한다 (WebP Lossless Bitstream Format 4~5).
// 색 캐시와 메타 접두 부호는 쓰지 않는다.
const (
	numLiteralCodes  = 256
	numLengthCodes   = 24
	numDistanceCodes = 40
	// 초록 부호는 리터럴 256개 뒤에 길이 접두 24개가 붙는다
	greenAlphabet = numLiteralCodes + numLengthCodes
	// 접두 부호 다섯 개: 초록(+길이), 빨강, 파랑, 알파, 거리
	numPrefixCodes = 5
	codeGreen      = 0
	codeRed        = 1
	codeBlue       = 2
	codeAlpha      = 3
	codeDistance   = 4
	// 거리 부호 1~120은 가까운 2차원 오프셋, 그 위는 (거리 + 120)
	numPlaneCodes  = 120
	planeMaxY      = 7
	planeXOrigin   = 8
	planeNibble    = 4
	planeNibbleMsk = 0xf
	// 역참조 길이·거리 한계와 탐색 설정
	lzMinLength   = 3
	lzMaxLength   = 4096
	lzMaxDistance = 1<<20 - numPlaneCodes
	lzHashBits    = 16
	lzMaxChain    = 48
	// 접두 값 부호화: 0~3은 그대로, 그 위는 (2·최상위 비트 + 다음 비트)와 추가 비트
	prefixDirect = 4
)

// planeTable은 거리 부호 1~120의 (y, 8−x) 오프셋이다 (명세의 distance map, 가까운 순).
var planeTable = [numPlaneCodes]uint8{
	0x18, 0x07, 0x17, 0x19, 0x28, 0x06, 0x27, 0x29, 0x16, 0x1a,
	0x26, 0x2a, 0x38, 0x05, 0x37, 0x39, 0x15, 0x1b, 0x36, 0x3a,
	0x25, 0x2b, 0x48, 0x04, 0x47, 0x49, 0x14, 0x1c, 0x35, 0x3b,
	0x46, 0x4a, 0x24, 0x2c, 0x58, 0x45, 0x4b, 0x34, 0x3c, 0x03,
	0x57, 0x59, 0x13, 0x1d, 0x56, 0x5a, 0x23, 0x2d, 0x44, 0x4c,
	0x55, 0x5b, 0x33, 0x3d, 0x68, 0x02, 0x67, 0x69, 0x12, 0x1e,
	0x66, 0x6a, 0x22, 0x2e, 0x54, 0x5c, 0x43, 0x4d, 0x65, 0x6b,
	0x32, 0x3e, 0x78, 0x01, 0x77, 0x79, 0x53, 0x5d, 0x11, 0x1f,
	0x64, 0x6c, 0x42, 0x4e, 0x76, 0x7a, 0x21, 0x2f, 0x75, 0x7b,
	0x31, 0x3f, 0x63, 0x6d, 0x52, 0x5e, 0x00, 0x74, 0x7c, 0x41,
	0x4f, 0x10, 0x20, 0x62, 0x6e, 0x30, 0x73, 0x7d, 0x51, 0x5f,
	0x40, 0x72, 0x7e, 0x61, 0x6f, 0x50, 0x71, 0x7f, 0x60, 0x70,
}

// lzToken은 리터럴 픽셀(length 0) 또는 역참조(길이, 거리 부호)다.
type lzToken struct {
	argb   uint32
	length uint16
	dist   uint32
}

// prefixEncode는 값 v(≥1)를 접두 기호와 추가 비트로 나눈다.
func prefixEncode(v int) (sym int, nbits uint, extra uint32) {
	d := v - 1
	if d < prefixDirect {
		return d, 0, 0
	}
	h := bits.Len(uint(d)) - 1
	nb := h - 1
	return 2*h + (d>>nb)&1, uint(nb), uint32(d & (1<<nb - 1))
}

// distanceCodes는 폭 w에서 거리 → 2차원 거리 부호 표를 만든다 (0이면 해당 없음). 같은 거리는 작은 부호를 쓴다.
func distanceCodes(w int) []uint8 {
	maxD := planeMaxY*w + planeXOrigin
	codes := make([]uint8, maxD+1)
	for c, t := range planeTable {
		d := int(t>>planeNibble)*w + planeXOrigin - int(t&planeNibbleMsk)
		if d >= 1 && codes[d] == 0 {
			codes[d] = uint8(c + 1)
		}
	}
	return codes
}

// tokenHistograms는 토큰을 다섯 접두 부호의 빈도로 센다.
func tokenHistograms(toks []lzToken) [numPrefixCodes][]uint32 {
	h := [numPrefixCodes][]uint32{
		make([]uint32, greenAlphabet), make([]uint32, numLiteralCodes), make([]uint32, numLiteralCodes),
		make([]uint32, numLiteralCodes), make([]uint32, numDistanceCodes),
	}
	for _, t := range toks {
		if t.length == 0 {
			a, r, g, b := argbBytes(t.argb)
			h[codeGreen][g]++
			h[codeRed][r]++
			h[codeBlue][b]++
			h[codeAlpha][a]++
			continue
		}
		ls, _, _ := prefixEncode(int(t.length))
		ds, _, _ := prefixEncode(int(t.dist))
		h[codeGreen][numLiteralCodes+ls]++
		h[codeDistance][ds]++
	}
	return h
}

func argbBytes(c uint32) (a, r, g, b uint8) {
	return uint8(c >> 24), uint8(c >> 16), uint8(c >> 8), uint8(c)
}

func writeTokens(w *bitWriter, toks []lzToken, codes *[numPrefixCodes]prefixCode) {
	for _, t := range toks {
		if t.length == 0 {
			a, r, g, b := argbBytes(t.argb)
			codes[codeGreen].write(w, int(g))
			codes[codeRed].write(w, int(r))
			codes[codeBlue].write(w, int(b))
			codes[codeAlpha].write(w, int(a))
			continue
		}
		ls, lnb, lx := prefixEncode(int(t.length))
		codes[codeGreen].write(w, numLiteralCodes+ls)
		w.put(lx, lnb)
		ds, dnb, dx := prefixEncode(int(t.dist))
		codes[codeDistance].write(w, ds)
		w.put(dx, dnb)
	}
}

// writeEntropyImage는 폭 width인 ARGB 이미지를 엔트로피 부호화해 쓴다.
// 주 이미지는 메타 접두 부호 여부 비트가 하나 더 있다 (변환의 부속 이미지에는 없다).
func writeEntropyImage(w *bitWriter, argb []uint32, width int, main bool) {
	toks := lz77(argb, width)
	h := tokenHistograms(toks)
	// 색 캐시 없음
	w.put(0, 1)
	if main {
		// 메타 접두 부호 없음 — 이미지 전체가 부호 한 벌
		w.put(0, 1)
	}
	var codes [numPrefixCodes]prefixCode
	for i := range h {
		codes[i] = writePrefixCode(w, h[i])
	}
	writeTokens(w, toks, &codes)
}
