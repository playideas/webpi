package webpi

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"
)

// 투명 픽셀에 숨은 색은 보이는 이웃의 색차로 번지면 안 된다 — 보이는 빨강과 투명 초록이 엇갈리는
// 체커보드의 색차는 온통 빨강인 이미지의 색차와 같아야 한다.
func Test_toYUV420_transparentNeighbours_doNotBleedIntoChroma(t *testing.T) {
	const w, h = 21, 13
	checker := image.NewNRGBA(image.Rect(0, 0, w, h))
	red := image.NewNRGBA(image.Rect(0, 0, w, h))
	alpha := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			red.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
			if (x+y)%2 == 0 {
				checker.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
				alpha[y*w+x] = opaqueAlpha
			} else {
				checker.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 0})
			}
		}
	}
	got, want := toYUV420(checker, alpha), toYUV420(red, nil)
	if !bytes.Equal(got.u, want.u) || !bytes.Equal(got.v, want.v) {
		t.Fatalf("색차가 번졌다\n u %v\nwant %v", got.u[:8], want.u[:8])
	}
}

// 넷 다 불투명한 2×2는 알파 없이 변환한 것과 같아야 한다 (불투명 경로와 결과가 갈리지 않게).
func Test_toYUV420_opaqueAlphaPlane_matchesNoAlpha(t *testing.T) {
	img := syntheticImage(37, 23)
	alpha := bytes.Repeat([]byte{opaqueAlpha}, 37*23)
	got, want := toYUV420(img, alpha), toYUV420(img, nil)
	if !bytes.Equal(got.u, want.u) || !bytes.Equal(got.v, want.v) || !bytes.Equal(got.y, want.y) {
		t.Fatal("불투명 알파 평면에서 결과가 달라졌다")
	}
}

func Test_Encode_nanQuality_returnsError(t *testing.T) {
	err := Encode(&bytes.Buffer{}, syntheticImage(8, 8), &Options{Quality: float32(math.NaN())})
	if err == nil {
		t.Fatal("NaN 품질에 오류가 없다")
	}
}

// 범위 밖 품질은 가까운 끝값으로 자른다.
func Test_Encode_outOfRangeQuality_clampsToEnds(t *testing.T) {
	img := syntheticImage(40, 24)
	enc := func(q float32) []byte {
		var b bytes.Buffer
		if err := Encode(&b, img, &Options{Quality: q}); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	if !bytes.Equal(enc(-5), enc(0)) || !bytes.Equal(enc(150), enc(maxQuality)) {
		t.Fatal("범위 밖 품질이 끝값과 다르게 인코딩됐다")
	}
}
