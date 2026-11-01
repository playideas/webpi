package webpi

import "testing"

// 색차 AC 편향은 q95 이상 0, q85 초과 −1, q75 초과 −2, 그 아래(범위 밖 포함) −3이다.
func Test_uvACBiasAt_qualities_taperToZero(t *testing.T) {
	want := map[float32]int{-5: -3, 0: -3, 70: -3, 75: -3, 75.5: -2, 80: -2, 85: -2, 85.5: -1, 90: -1, 94.9: -1, 95: 0, 100: 0, 150: 0}
	for q, w := range want {
		if got := uvACBiasAt(q); got != w {
			t.Errorf("q=%v: %d, want %d", q, got, w)
		}
	}
}
