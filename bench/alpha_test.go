package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/rand/v2"
	"os/exec"
	"testing"

	"golang.org/x/image/webp"

	"github.com/playideas/webpi"
)

// alphaPattern은 (x, y, 폭, 높이, 난수)로 알파 값을 만든다.
type alphaPattern struct {
	name string
	fn   func(x, y, w, h int, r *rand.Rand) uint8
}

var alphaPatterns = []alphaPattern{
	{"disc", func(x, y, w, h int, _ *rand.Rand) uint8 {
		dx, dy := 2*x-w, 2*y-h
		return uint8(255 * b2u(dx*dx+dy*dy < w*h/2))
	}},
	{"gradient", func(x, _, w, _ int, _ *rand.Rand) uint8 { return uint8(255 * x / max(w-1, 1)) }},
	{"noise", func(_, _, _, _ int, r *rand.Rand) uint8 { return uint8(r.IntN(256)) }},
	{"levels3", func(x, y, _, _ int, _ *rand.Rand) uint8 { return uint8(127 * ((x/3 + y/2) % 3)) }},
	{"levels5", func(x, y, _, _ int, r *rand.Rand) uint8 { return uint8(60 * ((x + y + r.IntN(2)) % 5)) }},
	{"levels17", func(x, y, _, _ int, _ *rand.Rand) uint8 { return uint8(15 * ((x*7 + y*3) % 17)) }},
	{"transparent", func(_, _, _, _ int, _ *rand.Rand) uint8 { return 0 }},
	{"onehole", func(x, y, _, _ int, _ *rand.Rand) uint8 { return uint8(255 - 255*b2u(x == 1 && y == 0)) }},
	// 값 분포가 치우친 잡음 — LZ77보다 리터럴만 쓰는 쪽이 작다
	{"skewed", func(_, _, _, _ int, r *rand.Rand) uint8 { return uint8(255 - min(int(r.ExpFloat64()*2), 255)) }},
	// 경계가 17단계로 부드러운 원 — 엔트로피로 고른 필터보다 필터 없음이 작다
	{"softdisc", func(x, y, w, h int, _ *rand.Rand) uint8 {
		dx, dy := 2*x-w, 2*y-h
		d := dx*dx + dy*dy - w*h/2
		return uint8(15 * min(max(8-d/max(w, 1), 0), 17))
	}},
}

func b2u(b bool) int {
	if b {
		return 1
	}
	return 0
}

// alphaTestSizes는 묶기 폭(2·4·8픽셀)에 맞지 않는 폭과 1픽셀 경계를 포함한다.
var alphaTestSizes = [][2]int{{2, 1}, {1, 3}, {3, 3}, {7, 5}, {13, 9}, {33, 17}, {100, 37}, {257, 64}}

func makeAlphaImage(p alphaPattern, w, h int, r *rand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 9), uint8(y * 5), uint8(x ^ y), p.fn(x, y, w, h, r)})
		}
	}
	return img
}

func alphaOf(m image.Image) []uint8 {
	b := m.Bounds()
	out := make([]uint8, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := m.At(x, y).RGBA()
			out = append(out, uint8(a>>8))
		}
	}
	return out
}

// 알파는 무손실이므로 두 디코더(x/image/webp, libwebp dwebp) 모두에서 원본과 정확히 같아야 한다.
func Test_Encode_alphaImages_decodeToExactAlpha(t *testing.T) {
	_, dwebpErr := exec.LookPath("dwebp")
	r := rand.New(rand.NewPCG(1, 2))
	for _, p := range alphaPatterns {
		for _, sz := range alphaTestSizes {
			t.Run(fmt.Sprintf("%s_%dx%d", p.name, sz[0], sz[1]), func(t *testing.T) {
				nrgba := makeAlphaImage(p, sz[0], sz[1], r)
				for _, img := range alphaInputs(nrgba) {
					checkAlphaRoundTrip(t, img, dwebpErr == nil)
				}
			})
		}
	}
}

// alphaInputs는 같은 알파를 여러 이미지 타입으로 만든다. Pix를 직접 읽는 NRGBA·RGBA(알파가 곱해진 색)와,
// At으로 읽는 일반 경로인 NRGBA64·Paletted(투명색이 있는 PNG를 디코딩하면 나오는 타입)다.
func alphaInputs(nrgba *image.NRGBA) []image.Image {
	b := nrgba.Bounds()
	rgba, n64 := image.NewRGBA(b), image.NewNRGBA64(b)
	draw.Draw(rgba, b, nrgba, image.Point{}, draw.Src)
	draw.Draw(n64, b, nrgba, image.Point{}, draw.Src)
	// 팔레트 항목 v는 알파 v — 픽셀의 알파 값을 그대로 인덱스로 쓴다
	pal := make(color.Palette, 256)
	for v := range pal {
		pal[v] = color.NRGBA{uint8(v), 255 - uint8(v), 77, uint8(v)}
	}
	paletted := image.NewPaletted(b, pal)
	for i := range paletted.Pix {
		paletted.Pix[i] = nrgba.Pix[4*i+3]
	}
	return []image.Image{nrgba, rgba, n64, paletted}
}

func checkAlphaRoundTrip(t *testing.T, img image.Image, useDwebp bool) {
	t.Helper()
	var buf bytes.Buffer
	if err := webpi.Encode(&buf, img, &webpi.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	want := alphaOf(img)
	dec, err := webp.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("%T x/image/webp: %v", img, err)
	}
	if got := alphaOf(dec); !bytes.Equal(got, want) {
		t.Fatalf("%T x/image/webp 알파 불일치\n got %v\nwant %v", img, got, want)
	}
	if !useDwebp {
		return
	}
	dec, err = decodeDwebp(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := alphaOf(dec); !bytes.Equal(got, want) {
		t.Fatalf("%T dwebp 알파 불일치\n got %v\nwant %v", img, got, want)
	}
}
