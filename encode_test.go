package webpi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// 합격 하한 — q75에서 합성 이미지의 PSNR이 이보다 낮으면 복원이 어긋난 것이다
const minPSNRAtQ75 = 30

// syntheticImage는 그라디언트·경계·잡음이 섞인 테스트 이미지다. 크기는 16의 배수가 아니게 잡아 채움을 시험한다.
func syntheticImage(w, h int) *image.RGBA {
	r := rand.New(rand.NewPCG(7, 8))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x + y) % 256), 255}
			if (x/20+y/20)%2 == 0 {
				c.B = 255 - c.B
			}
			n := uint8(r.IntN(16))
			img.Set(x, y, color.RGBA{c.R/2 + n, c.G, c.B, 255})
		}
	}
	return img
}

// decodeWithDwebp는 libwebp 디코더로 푼다 — 합격 기준 2 (브라우저와 같은 디코더).
func decodeWithDwebp(t *testing.T, webp []byte) image.Image {
	t.Helper()
	if _, err := exec.LookPath("dwebp"); err != nil {
		t.Skip("dwebp 없음")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "a.webp"), filepath.Join(dir, "a.png")
	if err := os.WriteFile(in, webp, 0o600); err != nil {
		t.Fatal(err)
	}
	if msg, err := exec.Command("dwebp", in, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("dwebp 실패: %v\n%s", err, msg)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func rgbPSNR(a, b image.Image) float64 {
	var sse float64
	bd := a.Bounds()
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			for _, d := range []float64{float64(r1>>8) - float64(r2>>8), float64(g1>>8) - float64(g2>>8), float64(b1>>8) - float64(b2>>8)} {
				sse += d * d
			}
		}
	}
	mse := sse / float64(bd.Dx()*bd.Dy()*3)
	return 10 * math.Log10(255*255/mse)
}

func Test_Encode_syntheticImage_decodesWithDwebpAtReasonablePSNR(t *testing.T) {
	src := syntheticImage(99, 67)
	var buf bytes.Buffer
	if err := Encode(&buf, src, &Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	dec := decodeWithDwebp(t, buf.Bytes())
	if dec.Bounds().Size() != src.Bounds().Size() {
		t.Fatalf("크기 %v, want %v", dec.Bounds().Size(), src.Bounds().Size())
	}
	psnr := rgbPSNR(src, dec)
	t.Logf("bytes=%d psnr=%.2fdB", buf.Len(), psnr)
	if psnr < minPSNRAtQ75 {
		t.Fatalf("psnr %.2f < %d", psnr, minPSNRAtQ75)
	}
}

func Test_Encode_higherQuality_givesLargerFileAndHigherPSNR(t *testing.T) {
	src := syntheticImage(64, 48)
	var prevSize int
	prevPSNR := 0.0
	for _, q := range []float32{20, 60, 95} {
		var buf bytes.Buffer
		if err := Encode(&buf, src, &Options{Quality: q}); err != nil {
			t.Fatal(err)
		}
		psnr := rgbPSNR(src, decodeWithDwebp(t, buf.Bytes()))
		t.Logf("q=%v bytes=%d psnr=%.2f", q, buf.Len(), psnr)
		if buf.Len() <= prevSize || psnr <= prevPSNR {
			t.Fatalf("q=%v: 크기·화질이 품질에 따라 늘지 않음", q)
		}
		prevSize, prevPSNR = buf.Len(), psnr
	}
}

func Test_Encode_zeroSizeImage_returnsError(t *testing.T) {
	if err := Encode(&bytes.Buffer{}, image.NewRGBA(image.Rect(0, 0, 0, 5)), nil); err == nil {
		t.Fatal("에러가 나야 함")
	}
}

// 루프 필터가 꺼져 있으면 디코더 출력 YUV가 인코더의 복원 평면과 비트 단위로 같아야 한다.
// 다르면 예측·변환·토큰 중 어딘가가 디코더와 어긋난 것이다.
func Test_encodeFrame_dwebpUnfilteredYUV_matchesReconstructionExactly(t *testing.T) {
	if _, err := exec.LookPath("dwebp"); err != nil {
		t.Skip("dwebp 없음")
	}
	for _, q := range []float32{5, 50, 75, 100} {
		src := syntheticImage(99, 67)
		e, vp8, err := encodeFrame(src, q)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := writeRIFF(&buf, vp8); err != nil {
			t.Fatal(err)
		}
		got := dwebpYUV(t, buf.Bytes())
		want := flatYUV(e.rec)
		if !bytes.Equal(got, want) {
			t.Fatalf("q=%v: dwebp YUV가 복원 평면과 다름 (첫 차이 %d / %d바이트)", q, firstDiff(got, want), len(want))
		}
	}
}

func dwebpYUV(t *testing.T, webp []byte) []byte {
	t.Helper()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "a.webp"), filepath.Join(dir, "a.yuv")
	if err := os.WriteFile(in, webp, 0o600); err != nil {
		t.Fatal(err)
	}
	if msg, err := exec.Command("dwebp", "-nofilter", "-yuv", in, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("dwebp 실패: %v\n%s", err, msg)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// flatYUV는 dwebp -yuv와 같은 평면 배치(Y w×h, U, V 각 ⌈w/2⌉×⌈h/2⌉)로 잘라 이어 붙인다.
func flatYUV(p *yuvPlanes) []byte {
	var out []byte
	for y := range p.h {
		out = append(out, p.y[y*p.ys:y*p.ys+p.w]...)
	}
	cw, ch := (p.w+1)/2, (p.h+1)/2
	for _, pl := range [][]uint8{p.u, p.v} {
		for y := range ch {
			out = append(out, pl[y*p.cs:y*p.cs+cw]...)
		}
	}
	return out
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func Test_levelCosts_residual_matchesTokenWriterRate(t *testing.T) {
	costs := newLevelCosts(&defaultTokenProb)
	cases := [][16]int16{
		{},
		{1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -3},
		{5, -1, 0, 2, 0, 0, 7, 0, 0, 0, 12, 0, 0, 30, 0, 0},
		{100, -67, 68, 2047, -500, 1, 0, 0, 4, 0, 0, 0, 0, 0, 0, 1},
	}
	for i, lv := range cases {
		for plane := range nPlane {
			for ctx := range nContext {
				for _, first := range []int{0, 1} {
					var want int64
					w := tokenWriter{probs: &defaultTokenProb, rate: &want}
					wantNZ := w.block(&lv, plane, ctx, first)
					got, nz := costs.residual(&lv, plane, ctx, first)
					if got != want || nz != wantNZ {
						t.Fatalf("case %d plane %d ctx %d first %d: got (%d,%d), want (%d,%d)", i, plane, ctx, first, got, nz, want, wantNZ)
					}
				}
			}
		}
	}
}

func Test_Encode_parallelRows_matchesSingleThreadBytes(t *testing.T) {
	src := syntheticImage(260, 150) // 17×10 매크로블록 — 행보다 작업자가 적고 많은 경우를 모두 지난다
	encode := func(procs int) []byte {
		prev := runtime.GOMAXPROCS(procs)
		defer runtime.GOMAXPROCS(prev)
		var buf bytes.Buffer
		if err := Encode(&buf, src, &Options{Quality: 60}); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	want := encode(1)
	for _, procs := range []int{2, 3, 8} {
		if got := encode(procs); !bytes.Equal(got, want) {
			t.Fatalf("GOMAXPROCS=%d: 순차 인코딩과 다름 (%d vs %d바이트)", procs, len(got), len(want))
		}
	}
}
