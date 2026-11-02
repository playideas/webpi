package webpi

import (
	"bytes"
	"math"
	"os/exec"
	"testing"
)

// 같은 세그먼트를 합쳐도(q98 일부, q100 전부) dwebp 출력이 인코더의 복원 평면과 비트 단위로 같아야 한다.
func Test_mergeDuplicates_highQuality_dwebpMatchesReconstruction(t *testing.T) {
	if _, err := exec.LookPath("dwebp"); err != nil {
		t.Skip("dwebp 없음")
	}
	for _, q := range []float32{98, maxQuality} {
		e := newEncoder(toYUV420(syntheticImage(83, 141), nil), q)
		if e.seg.distinct == nSegments {
			t.Fatalf("q%v: 합친 세그먼트가 없음", q)
		}
		if q == maxQuality && e.seg.distinct != 1 {
			t.Fatalf("q100: 세그먼트 %d개, want 1", e.seg.distinct)
		}
		vp8, err := e.bitstream(e.encodeRows())
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := writeRIFF(&buf, riffChunk{fourccVP8, vp8}); err != nil {
			t.Fatal(err)
		}
		got, want := dwebpYUV(t, buf.Bytes()), flatYUV(e.rec)
		if !bytes.Equal(got, want) {
			t.Fatalf("q%v: dwebp YUV가 복원 평면과 다름 (첫 차이 %d / %d바이트)", q, firstDiff(got, want), len(want))
		}
	}
}

// 합친 세그먼트의 id는 남은 세그먼트를 가리키고, 남은 세그먼트끼리는 양자화나 필터가 다르다.
func Test_mergeDuplicates_partialCollision_remapsToFirst(t *testing.T) {
	s := segmentation{ids: []uint8{0, 1, 2, 3, 1}}
	for n, qi := range []int{2, 2, 1, 0} {
		s.quant[n].qi = qi
	}
	s.mergeDuplicates()
	if s.distinct != 3 || !bytes.Equal(s.ids, []uint8{0, 0, 2, 3, 0}) {
		t.Fatalf("distinct %d, ids %v", s.distinct, s.ids)
	}
}

// 범위 밖 품질은 경계값과 같은 출력을 낸다 — -Inf나 아주 큰 음수도 0과 같다.
func Test_Encode_extremeQuality_matchesClampedBound(t *testing.T) {
	m := syntheticImage(48, 32)
	encode := func(q float32) []byte {
		var buf bytes.Buffer
		if err := Encode(&buf, m, &Options{Quality: q}); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	low, high := encode(0), encode(maxQuality)
	for _, q := range []float32{-1e30, float32(math.Inf(-1))} {
		if !bytes.Equal(encode(q), low) {
			t.Errorf("q=%v: q0과 다름", q)
		}
	}
	for _, q := range []float32{1e30, float32(math.Inf(1))} {
		if !bytes.Equal(encode(q), high) {
			t.Errorf("q=%v: q100과 다름", q)
		}
	}
}

// n개 파티션이 rows행만에 넘쳤다면 행 크기가 고르다고 보고 n·mbh/rows개 이상으로 건너뛴다.
func Test_neededPartsLog2_overflowAtRow_estimatesCount(t *testing.T) {
	cases := []struct{ n, rows, mbh, want int }{
		{1, 100, 100, 0}, // 끝까지 썼는데 마무리에서 넘침 — 호출자가 하나 늘린다
		{1, 50, 100, 1},
		{1, 30, 100, 2},
		{2, 40, 100, 3},
		{1, 1, 1000, 10},
	}
	for _, c := range cases {
		if got := neededPartsLog2(c.n, c.rows, c.mbh); got != c.want {
			t.Errorf("%+v: %d", c, got)
		}
	}
}
