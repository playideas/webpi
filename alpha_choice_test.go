package webpi

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"runtime"
	"sync/atomic"
	"testing"
)

// 압축해도 줄지 않는 잡음은 압축 방식 0(헤더 바이트 + 원본)으로 담는다.
func Test_encodeAlpha_uniformNoise_storesRaw(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	alpha := make([]uint8, 64*64)
	for i := range alpha {
		alpha[i] = uint8(r.Uint32())
	}
	got := encodeAlpha(alpha, 64, 64)
	if got[0] != alphaCompressionNone || !bytes.Equal(got[1:], alpha) {
		t.Fatalf("헤더 %#x, 길이 %d (원본 %d)", got[0], len(got), len(alpha))
	}
}

// 값 분포가 치우친 잡음은 짧은 일치를 고르면 리터럴만 쓰는 쪽보다 커진다 — 리터럴보다 크면 안 된다.
func Test_compressAlpha_skewedNoise_noLargerThanLiterals(t *testing.T) {
	const w, h, keepProb = 256, 256, 0.6
	r := rand.New(rand.NewPCG(3, 4))
	alpha := make([]uint8, w*h)
	argb := make([]uint32, w*h)
	for i := range alpha {
		a := opaqueAlpha
		for a > 0 && r.Float64() > keepProb {
			a--
		}
		alpha[i], argb[i] = uint8(a), uint32(a)<<greenShift
	}
	// 헤더 바이트, 변환·색 캐시·메타 부호 비트, 바이트 경계 여유
	const overheadBytes = 2
	got, limit := len(compressAlpha(alpha, w, h)), (literalBits(argb)+byteBits-1)/byteBits+overheadBytes
	if got > limit {
		t.Fatalf("%d바이트, 리터럴만 쓰면 %d바이트", got, limit)
	}
}

// 경계가 부드러운(17단계) 마스크는 엔트로피로 고른 필터보다 필터 없음이 작다 — 어느 필터보다도 크면 안 된다.
func Test_compressAlpha_softEdgeMask_noLargerThanAnyFilter(t *testing.T) {
	const w, h, sub = 192, 160, 4
	alpha := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			// 4×4 표본으로 덮은 원 — 경계에 0~16단계 값이 생긴다
			in := 0
			for sy := range sub {
				for sx := range sub {
					dx, dy := float64(x*sub+sx)-w*sub/2, float64(y*sub+sy)-h*sub/2
					in += int(b2i(dx*dx+dy*dy < (h*sub/3)*(h*sub/3)))
				}
			}
			alpha[y*w+x] = uint8(in * opaqueAlpha / (sub * sub))
		}
	}
	if best, _ := bestAlphaFilter(alpha, w, h); best == alphaFilterNone {
		t.Fatalf("엔트로피가 이미 필터 없음을 고름 — 시험 이미지가 문제를 재현하지 못함")
	}
	got := len(compressAlpha(alpha, w, h))
	for f := range numAlphaFilters {
		if c := len(encodeAlphaCandidate(alpha, w, h, f, false)); got > c {
			t.Errorf("필터 %d가 %d바이트로 더 작다 (고른 것 %d)", f, c, got)
		}
	}
}

// laterPanicImage는 알파 평면을 읽은 뒤(At 호출 limit번 뒤)부터 패닉한다 — VP8 쪽에서만 패닉이 난다.
type laterPanicImage struct {
	image.Rectangle
	calls *atomic.Int64
	limit int64
}

func (p laterPanicImage) ColorModel() color.Model { return color.NRGBAModel }
func (p laterPanicImage) Bounds() image.Rectangle { return p.Rectangle }
func (p laterPanicImage) At(x, y int) color.Color {
	if p.calls.Add(1) > p.limit {
		panic("변환 실패")
	}
	return color.NRGBA{uint8(x), uint8(y), 0, uint8(x * y)}
}

// VP8 쪽이 패닉해도 Encode는 알파 고루틴이 끝난 뒤 돌아온다 — 돌아온 뒤 남는 고루틴이 없다.
func Test_Encode_framePanicsWhileAlphaRuns_waitsForAlpha(t *testing.T) {
	const size = 512
	before := runtime.NumGoroutine()
	img := laterPanicImage{image.Rect(0, 0, size, size), &atomic.Int64{}, size * size}
	if err := Encode(&bytes.Buffer{}, img, nil); err == nil {
		t.Fatal("오류가 없다")
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("고루틴 %d → %d", before, after)
	}
}
