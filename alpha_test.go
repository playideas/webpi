package webpi

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"slices"
	"testing"
)

// 허프만 길이는 한계 이하이고, 기호가 둘 이상이면 Kraft 합이 정확히 1(완전한 부호)이어야 한다.
func Test_huffmanLengths_randomCounts_respectsLimitAndIsComplete(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for trial := range 500 {
		n := 2 + r.IntN(300)
		counts := make([]uint32, n)
		for i := range counts {
			// 지수적으로 치우친 빈도 — 깊은 트리를 만들어 길이 제한을 건드린다
			if r.IntN(3) > 0 {
				counts[i] = uint32(1) << r.IntN(28)
			}
		}
		counts[0], counts[1] = 1, 1
		for _, limit := range []int{maxCodeLengthCodeLength, maxCodeLength} {
			if limit == maxCodeLengthCodeLength && n > numCodeLengthCodes {
				continue
			}
			lengths := huffmanLengths(counts, limit)
			kraft := 0.0
			for s, l := range lengths {
				if (counts[s] > 0) != (l > 0) || int(l) > limit {
					t.Fatalf("trial %d: 기호 %d 빈도 %d 길이 %d (한계 %d)", trial, s, counts[s], l, limit)
				}
				if l > 0 {
					kraft += 1 / float64(uint(1)<<l)
				}
			}
			if kraft != 1 {
				t.Fatalf("trial %d: Kraft 합 %v", trial, kraft)
			}
		}
	}
}

// 접두 값 부호화는 명세의 디코딩 식으로 되돌아와야 한다.
func Test_prefixEncode_allValues_decodesBack(t *testing.T) {
	for v := 1; v <= lzMaxDistance+numPlaneCodes; v += 1 + v/1000 {
		sym, nbits, extra := prefixEncode(v)
		got := sym + 1
		if sym >= prefixDirect {
			eb := (sym - 2) >> 1
			got = (2+sym&1)<<eb + int(extra) + 1
			if uint(eb) != nbits {
				t.Fatalf("v=%d: 추가 비트 %d, 기대 %d", v, nbits, eb)
			}
		}
		if got != v || sym >= numDistanceCodes {
			t.Fatalf("v=%d: 기호 %d 추가 %d → %d", v, sym, extra, got)
		}
	}
}

// 길이 열 RLE를 디코더 규칙(16은 앞의 0 아닌 길이 반복, 처음 값 8)으로 풀면 원래 열이 나와야 한다.
func Test_rleCodeLengths_randomLengths_expandsToOriginal(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 2000 {
		lengths := make([]uint8, 1+r.IntN(400))
		for i := range lengths {
			if r.IntN(4) == 0 {
				lengths[i] = uint8(r.IntN(maxCodeLength + 1))
			} else if i > 0 {
				lengths[i] = lengths[i-1]
			}
		}
		var got []uint8
		prev := uint8(initialPrevCodeLength)
		for _, tk := range rleCodeLengths(lengths) {
			switch tk.sym {
			case codeRepeatPrev:
				for range int(tk.extra) + repeatPrevMin {
					got = append(got, prev)
				}
			case codeRepeatZeros:
				got = append(got, make([]uint8, int(tk.extra)+repeatZerosMin)...)
			case codeRepeatZerosL:
				got = append(got, make([]uint8, int(tk.extra)+repeatZerosLMin)...)
			default:
				got = append(got, tk.sym)
				if tk.sym != 0 {
					prev = tk.sym
				}
			}
		}
		if !slices.Equal(got, lengths) {
			t.Fatalf("got %v\nwant %v", got, lengths)
		}
	}
}

func Test_copyRGBUnpremultiplied_allValues_matchesStdlib(t *testing.T) {
	for a := range 256 {
		for v := 0; v <= a; v++ {
			var dst [rgbBytes]uint8
			copyRGBUnpremultiplied(dst[:], []uint8{uint8(v), uint8(v), uint8(v), uint8(a)}, 1)
			want := color.NRGBAModel.Convert(color.RGBA{uint8(v), uint8(v), uint8(v), uint8(a)}).(color.NRGBA)
			if dst[0] != want.R {
				t.Fatalf("a=%d v=%d: %d, want %d", a, v, dst[0], want.R)
			}
		}
	}
}

// 투명 정리는 보이는 픽셀을 건드리지 않고, 완전히 투명한 블록은 평탄하게 만든다.
func Test_cleanupTransparent_mixedBlocks_keepsVisiblePixels(t *testing.T) {
	const w, h = 24, 16
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(5, 6))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.IntN(256))
	}
	alpha := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			// 왼쪽 8열은 완전히 투명, 그다음 8열은 반만 보임, 나머지는 보임
			if x >= cleanupBlock && (x >= 2*cleanupBlock || y%2 == 0) {
				alpha[y*w+x] = uint8(1 + r.IntN(255))
			}
		}
	}
	before := toYUV420(img, nil)
	after := toYUV420(img, nil)
	cleanupTransparent(after, alpha)
	for y := range h {
		for x := range w {
			if alpha[y*w+x] != 0 && after.y[y*after.ys+x] != before.y[y*before.ys+x] {
				t.Fatalf("보이는 픽셀 (%d,%d)이 바뀌었다", x, y)
			}
		}
	}
	for y := range cleanupBlock {
		for x := range cleanupBlock {
			if after.y[y*after.ys+x] != after.y[0] {
				t.Fatalf("투명 블록이 평탄하지 않다: (%d,%d)", x, y)
			}
		}
	}
}

// 모든 픽셀이 불투명한 NRGBA는 확장 헤더 없이 기본 형식(VP8 청크 하나)으로 쓴다.
func Test_Encode_opaqueNRGBA_writesSimpleFormat(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 17, 9))
	for i := range img.Pix {
		img.Pix[i] = opaqueAlpha
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(buf.Bytes()[12:16]); got != fourccVP8 {
		t.Fatalf("첫 청크 %q, want %q", got, fourccVP8)
	}
}

// 알파가 있으면 VP8X(알파 플래그·캔버스 크기) → ALPH → VP8 순서로 쓴다.
func Test_Encode_translucentNRGBA_writesExtendedFormat(t *testing.T) {
	const w, h = 17, 9
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Pix[3] = 0
	var buf bytes.Buffer
	if err := Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if got := string(b[12:16]); got != fourccVP8X {
		t.Fatalf("첫 청크 %q, want %q", got, fourccVP8X)
	}
	hdr := b[20:30]
	cw := int(hdr[4]) | int(hdr[5])<<8 | int(hdr[6])<<16
	ch := int(hdr[7]) | int(hdr[8])<<8 | int(hdr[9])<<16
	if hdr[0] != vp8xAlphaFlag || cw != w-1 || ch != h-1 {
		t.Fatalf("VP8X %v", hdr)
	}
	if got := string(b[30:34]); got != fourccALPH {
		t.Fatalf("둘째 청크 %q, want %q", got, fourccALPH)
	}
}

// 단순 부호로 못 쓰는 기호(256 이상) 하나뿐인 부호는, 디코더가 0비트로 읽는 길이 1 부호 대신
// 쓰이지 않는 기호를 더해 두 기호 모두 1비트인 완전한 부호가 되어야 한다.
func Test_writePrefixCode_singleSymbolAbove255_writesTwoOneBitCodes(t *testing.T) {
	const sym = numLiteralCodes + 5
	counts := make([]uint32, greenAlphabet)
	counts[sym] = 9
	c := writePrefixCode(&bitWriter{}, counts)
	used := 0
	for s, l := range c.lengths {
		if l != 0 {
			used++
			if l != 1 {
				t.Fatalf("기호 %d 길이 %d", s, l)
			}
		}
	}
	if used != 2 || c.lengths[sym] != 1 || counts[0] != 0 {
		t.Fatalf("쓰인 기호 %d개, 대상 길이 %d, 입력 빈도가 바뀜 %v", used, c.lengths[sym], counts[0] != 0)
	}
}

// 최적 분할 표가 너무 커지는 평면은 탐욕 분할만 한다.
func Test_optimalPasses_aboveLimit_skipsOptimalParse(t *testing.T) {
	if got := optimalPasses(lzOptimalMaxPixels); got != lzOptimalPasses {
		t.Fatalf("한계 크기 %d회, want %d", got, lzOptimalPasses)
	}
	if got := optimalPasses(lzOptimalMaxPixels + 1); got != 0 {
		t.Fatalf("한계 초과 %d회, want 0", got)
	}
}
