package webpi

// arm64 NEON 커널 — kernels_arm64.s (internal/asmgen/gen.py가 생성). 결과는 Go 구현과 비트 단위로 같다.
// 판단 PD-20261031-3d308163: arm64 NEON 커널을 시험 삼아 넣고 효과를 잰다

//go:noescape
func quantBlock4(src *uint8, stride int, pred *[16]uint8, qv *quantVecs, levels *[16]int16, rec *[16]uint8) uint32

//go:noescape
func satd4NEON(src *uint8, stride int, pred *[16]uint8) uint32

func quantBlock(m *quantMatrix, src []uint8, s, x, y int, pred *[16]uint8, levels *[16]int16, rec *[16]uint8) int64 {
	// 커널은 4행을 stride 간격으로 읽는다 — 마지막 행 끝까지 src 안에 있는지 여기서 확인한다
	_ = src[(y+3)*s+x+3]
	return int64(quantBlock4(&src[y*s+x], s, pred, &m.vec, levels, rec))
}

func satd4(src []uint8, s, x, y int, pred *[16]uint8) int64 {
	_ = src[(y+3)*s+x+3]
	return int64(satd4NEON(&src[y*s+x], s, pred))
}
