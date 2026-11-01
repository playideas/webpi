package webpi

import "testing"

func countI4(e *encoder) int {
	n := 0
	for _, mb := range e.mbs {
		n += int(b2i(mb.i4))
	}
	return n
}

// i4 헤더 예산이 0이면 i4를 하나도 고르지 않고, 처음 예산에서는 고른다 (첫 파티션 초과 시 재인코딩의 바탕).
func Test_pickI4_zeroBudget_choosesNoI4(t *testing.T) {
	src := toYUV420(syntheticImage(96, 80), nil)
	full := newEncoder(src, maxQuality)
	full.encodeRows()
	none := newEncoder(src, maxQuality)
	none.i4Budget = 0
	none.encodeRows()
	if countI4(full) == 0 || countI4(none) != 0 {
		t.Fatalf("i4 매크로블록 수: 처음 예산 %d, 예산 0 %d", countI4(full), countI4(none))
	}
}
