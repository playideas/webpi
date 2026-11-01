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

// 작업 고루틴에서 난 패닉도 프로세스를 죽이지 않고 PanicError로 돌아와야 한다.
// 오류 문자열에는 패닉 값만, 원래 스택은 Stack에 있어야 한다.
func Test_Encode_panicInRowWorker_returnsPanicErrorWithStack(t *testing.T) {
	img := panicImage{image.Rect(0, 0, 64, 64), 37}
	err := Encode(&bytes.Buffer{}, img, nil)
	var pe *PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("패닉 오류가 아니다: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "행 읽기 실패") || strings.Contains(msg, "\n") {
		t.Fatalf("오류 문자열은 패닉 값만 담은 한 줄이어야 한다:\n%s", msg)
	}
	if !strings.Contains(string(pe.Stack), "panicImage.At") {
		t.Fatalf("Stack에 원래 패닉 위치가 없다:\n%s", pe.Stack)
	}
}

// 알파 고루틴의 패닉도 결과 채널로 PanicError가 돌아와야 한다.
func Test_startAlpha_encoderPanics_returnsPanicError(t *testing.T) {
	r := <-startAlpha(func() []byte { panic("알파 실패") })
	var pe *PanicError
	if !errors.As(r.err, &pe) || pe.Value != "알파 실패" || r.data != nil {
		t.Fatalf("결과 %+v", r)
	}
	if !strings.Contains(string(pe.Stack), "Test_startAlpha_encoderPanics_returnsPanicError") {
		t.Fatalf("Stack에 원래 패닉 위치가 없다:\n%s", pe.Stack)
	}
}

// 한 행이 중간에 패닉해도 그 행을 기다리는 아랫행들이 멈추지 않고, 패닉이 부른 쪽으로 올라와야 한다.
func Test_encodeRows_panicMidRow_doesNotDeadlock(t *testing.T) {
	e := newEncoder(toYUV420(syntheticImage(96, 96), nil), defaultQuality)
	// 셋째 행 둘째 매크로블록부터 헤더 자리가 없어 인덱스 패닉이 난다
	e.mbs = e.mbs[:2*e.src.mbw+1]
	defer func() {
		if _, ok := recover().(*PanicError); !ok {
			t.Fatal("encodeRows가 PanicError로 패닉하지 않았다")
		}
	}()
	e.encodeRows()
}
