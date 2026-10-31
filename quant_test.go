package webpi

import (
	"math/rand/v2"
	"testing"
)

// SIMD 커널이 있는 아키텍처에서는 Go 구현과 비트 단위로 같아야 한다 (없으면 같은 함수끼리 비교).
func Test_quantBlock_randomBlocks_matchesGoImplementation(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	const stride = 7
	src := make([]uint8, stride*4+4)
	for qi := range maxQIndex + 1 {
		q := newQuantizer(qi, r.IntN(9)-4, r.IntN(9)-4)
		for _, m := range []*quantMatrix{&q.y1, &q.uv} {
			for trial := range 200 {
				var pred [16]uint8
				fillBlock(r, src, pred[:], trial)
				var lvA, lvB [16]int16
				var recA, recB [16]uint8
				sseA := quantBlock(m, src, stride, 2, 0, &pred, &lvA, &recA)
				sseB := quantBlockGo(m, src, stride, 2, 0, &pred, &lvB, &recB)
				if sseA != sseB || lvA != lvB || recA != recB {
					t.Fatalf("qi=%d trial=%d: sse %d/%d\nlevels %v\n  want %v\nrec %v\n  want %v", qi, trial, sseA, sseB, lvA, lvB, recA, recB)
				}
			}
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
