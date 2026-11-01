package webpi

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
)

// panicImage는 행 panicRow를 읽으면 패닉하는 이미지다 — 작업 고루틴 안에서 패닉을 일으킨다.
type panicImage struct {
	image.Rectangle
	panicRow int
}

func (p panicImage) ColorModel() color.Model { return color.NRGBAModel }
func (p panicImage) Bounds() image.Rectangle { return p.Rectangle }
func (p panicImage) Opaque() bool            { return true }
func (p panicImage) At(x, y int) color.Color {
	if y == p.panicRow {
		panic("행 읽기 실패")
	}
	return color.NRGBA{uint8(x), uint8(y), 0, 255}
}

// 작업 고루틴에서 난 패닉도 프로세스를 죽이지 않고, 패닉 값과 원래 스택이 담긴 오류로 돌아와야 한다.
func Test_Encode_panicInRowWorker_returnsErrorWithStack(t *testing.T) {
	img := panicImage{image.Rect(0, 0, 64, 64), 37}
	err := Encode(&bytes.Buffer{}, img, nil)
	var pe *panicError
	if !errors.As(err, &pe) {
		t.Fatalf("패닉 오류가 아니다: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "행 읽기 실패") || !strings.Contains(msg, "panicImage.At") {
		t.Fatalf("패닉 값이나 원래 스택이 빠졌다:\n%s", msg)
	}
}

// 한 행이 중간에 패닉해도 그 행을 기다리는 아랫행들이 멈추지 않고, 패닉이 부른 쪽으로 올라와야 한다.
func Test_encodeRows_panicMidRow_doesNotDeadlock(t *testing.T) {
	e := newEncoder(toYUV420(syntheticImage(96, 96)), defaultQuality)
	// 셋째 행 둘째 매크로블록부터 헤더 자리가 없어 인덱스 패닉이 난다
	e.mbs = e.mbs[:2*e.src.mbw+1]
	defer func() {
		if _, ok := recover().(*panicError); !ok {
			t.Fatal("encodeRows가 panicError로 패닉하지 않았다")
		}
	}()
	e.encodeRows()
}
