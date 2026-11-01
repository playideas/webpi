package main

import (
	"bytes"
	"image"
	"math/rand/v2"
	"testing"

	"golang.org/x/image/webp"

	"github.com/playideas/webpi"
)

// 높은 품질의 큰 잡음 이미지는 i4를 많이 골라 첫 파티션(모드 헤더)이 규격 한도를 넘는다.
// 오류 대신 i4 헤더 예산을 줄여 다시 인코딩하고, 16MB가 넘는 토큰은 여러 파티션에 나눠
// x/image/webp로도 디코딩되는 파일이 나와야 한다 (수정 전에는 오류).
func Test_Encode_largeNoiseAtQ100_producesDecodableFile(t *testing.T) {
	if testing.Short() {
		t.Skip("큰 이미지 — -short에서는 건너뛴다")
	}
	const side = 4096
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		v := uint8(r.Uint32())
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v^0x55, v+7, 255
	}
	var buf bytes.Buffer
	if err := webpi.Encode(&buf, img, &webpi.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	dec, err := webp.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Bounds() != img.Bounds() {
		t.Fatalf("크기 %v, want %v", dec.Bounds(), img.Bounds())
	}
}
