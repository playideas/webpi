package webpi

import (
	"math/rand/v2"
	"testing"
)

func Test_packLevels_roundTrip_restoresEveryMacroblock(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var mbs [3][nBlocks][16]int16
	for i := range mbs {
		for b := range nBlocks {
			// 블록마다 0이 아닌 계수 수를 0~16으로 달리하고 끝 쪽 계수도 섞는다
			for k := range r.IntN(17) {
				mbs[i][b][zigzag[k]] = int16(r.IntN(2*maxLevel+1) - maxLevel)
			}
		}
	}
	var buf []int16
	for i := range mbs {
		buf = packLevels(buf, &mbs[i])
	}
	for i := range mbs {
		var got [nBlocks][16]int16
		got[0][0] = 99 // 풀 때 이전 값이 남지 않아야 한다
		buf = unpackLevels(buf, &got)
		if got != mbs[i] {
			t.Fatalf("매크로블록 %d가 다르게 풀림", i)
		}
	}
	if len(buf) != 0 {
		t.Fatalf("남은 값 %d개", len(buf))
	}
}

func Test_selectLowest_withTies_keepsModeOrder(t *testing.T) {
	ests := [nBModes]int64{5, 3, 9, 3, 1, 7, 3, 8, 0, 2}
	var o [nBModes]modeEst
	for m, e := range ests {
		o[m] = modeEst{e, uint8(m)}
	}
	selectLowest(&o, i4Candidates)
	want := []uint8{8, 4, 9, 1, 3}
	for i, m := range want {
		if o[i].m != m {
			t.Fatalf("순서 %v, want %v", o[:i4Candidates], want)
		}
	}
}
