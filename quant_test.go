package webpi

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// 블록을 읽고 쓰는 버퍼의 행 간격 — 4가 아닌 값으로 stride 처리를 확인한다
const (
	testSrcStride  = 7
	testPredStride = 6
	testRecStride  = 5
)

// SIMD 커널이 있는 아키텍처에서는 Go 구현과 비트 단위로 같아야 한다 (없으면 같은 함수끼리 비교).
// 복원 버퍼는 블록 밖 값까지 비교해 행 사이를 건드리지 않는지도 본다.
func Test_quantBlock_randomBlocks_matchesGoImplementation(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	src := make([]uint8, testSrcStride*4+4)
	pred := make([]uint8, testPredStride*3+4)
	for qi := range maxQIndex + 1 {
		q := newQuantizer(qi, r.IntN(9)-4, r.IntN(9)-4)
		for _, m := range []*quantMatrix{&q.y1, &q.uv} {
			for trial := range 200 {
				fillBlock(r, src, pred, trial)
				var lvA, lvB [16]int16
				recA, recB := make([]uint8, testRecStride*3+4), make([]uint8, testRecStride*3+4)
				sseA := quantBlock(m, src, testSrcStride, 2, 0, pred, testPredStride, &lvA, recA, testRecStride)
				sseB := quantBlockGo(m, src, testSrcStride, 2, 0, pred, testPredStride, &lvB, recB, testRecStride)
				if sseA != sseB || lvA != lvB || !slices.Equal(recA, recB) {
					t.Fatalf("qi=%d trial=%d: sse %d/%d\nlevels %v\n  want %v\nrec %v\n  want %v", qi, trial, sseA, sseB, lvA, lvB, recA, recB)
				}
			}
		}
	}
}

func Test_quantBlockDC_randomBlocks_matchesGoImplementation(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	src := make([]uint8, testSrcStride*4+4)
	pred := make([]uint8, testPredStride*3+4)
	for qi := range maxQIndex + 1 {
		q := newQuantizer(qi, 0, 0)
		for trial := range 200 {
			fillBlock(r, src, pred, trial)
			coeffs := fdctBlockGo(src, testSrcStride, 1, 0, pred, testPredStride)
			// Y2에서 복원한 DC는 블록 하나의 범위보다 넓을 수 있다
			dc := int32(r.IntN(1<<15) - 1<<14)
			lvA, lvB := [16]int16{0: 1}, [16]int16{0: 1}
			recA, recB := make([]uint8, testRecStride*3+4), make([]uint8, testRecStride*3+4)
			quantBlockDC(&q.y1, &coeffs, dc, &lvA, pred, testPredStride, recA, testRecStride)
			quantBlockDCGo(&q.y1, &coeffs, dc, &lvB, pred, testPredStride, recB, testRecStride)
			if lvA != lvB || !slices.Equal(recA, recB) {
				t.Fatalf("qi=%d trial=%d dc=%d:\nlevels %v\n  want %v\nrec %v\n  want %v", qi, trial, dc, lvA, lvB, recA, recB)
			}
		}
	}
}

func Test_fdctBlock_randomBlocks_matchesGoImplementation(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	src := make([]uint8, testSrcStride*4+4)
	pred := make([]uint8, testPredStride*3+4)
	for trial := range 4000 {
		fillBlock(r, src, pred, trial)
		if got, want := fdctBlock(src, testSrcStride, 3, 0, pred, testPredStride), fdctBlockGo(src, testSrcStride, 3, 0, pred, testPredStride); got != want {
			t.Fatalf("fdct trial %d: %v, want %v", trial, got, want)
		}
		if got, want := fdctBins(src, testSrcStride, 3, 0, pred, testPredStride), fdctBinsGo(src, testSrcStride, 3, 0, pred, testPredStride); got != want {
			t.Fatalf("bins trial %d: %v, want %v", trial, got, want)
		}
	}
}

func Test_ttransform_randomBlocks_matchesGoImplementation(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	src := make([]uint8, testSrcStride*4+4)
	for trial := range 4000 {
		fillBlock(r, src, nil, trial)
		if got, want := ttransform(src[2:], testSrcStride), ttransformGo(src[2:], testSrcStride); got != want {
			t.Fatalf("trial %d: %d, want %d", trial, got, want)
		}
	}
}

// fillBlock은 무작위·극단(0/255 교차)·평탄 패턴을 번갈아 채운다.
func fillBlock(r *rand.Rand, src, pred []uint8, trial int) {
	for i := range src {
		src[i] = uint8(r.IntN(256))
	}
	for i := range pred {
		pred[i] = uint8(r.IntN(256))
	}
	switch trial % 4 {
	case 1:
		for i := range src {
			src[i] = uint8(255 * (i % 2))
		}
		for i := range pred {
			pred[i] = uint8(255 * ((i + 1) % 2))
		}
	case 2:
		v := uint8(r.IntN(256))
		for i := range pred {
			pred[i] = v
		}
	}
}

func Test_satd4_randomBlocks_matchesGoImplementation(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	const stride = 9
	src := make([]uint8, stride*4+4)
	for trial := range 4000 {
		var pred [16]uint8
		fillBlock(r, src, pred[:], trial)
		if got, want := satd4(src, stride, 3, 0, &pred), satd4Go(src, stride, 3, 0, &pred); got != want {
			t.Fatalf("trial %d: %d, want %d", trial, got, want)
		}
	}
}
