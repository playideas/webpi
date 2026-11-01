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
				// 알파가 곱해진 RGBA로도 넣어 본다 — 알파 값은 같아야 한다
				rgba := image.NewRGBA(nrgba.Bounds())
				draw.Draw(rgba, rgba.Bounds(), nrgba, image.Point{}, draw.Src)
				for _, img := range []image.Image{nrgba, rgba} {
					checkAlphaRoundTrip(t, img, dwebpErr == nil)
				}
			})
		}
	}
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
