//go:build !purego

package webpi

// arm64 NEON 커널 — kernels_arm64.s (internal/asmgen/gen.py가 생성). 결과는 Go 구현과 비트 단위로 같다.
// 판단 PD-20261031-3d308163: arm64 NEON 커널을 시험 삼아 넣고 효과를 잰다
// 판단 PD-20261101-914e85d6: NEON이 기본, -tags purego면 순수 Go 구현(kernels_noasm.go)으로 빌드한다
// 판단 PD-20261101-f8b21aac: i16·색차 양자화, 분석 히스토그램, 텍스처 왜곡 변환도 NEON으로 옮긴다
//
// 커널은 4×4 블록을 stride 간격 4행으로 읽고 쓴다. 래퍼는 마지막 행 끝까지 슬라이스 안에 있는지 먼저 확인한다.

//go:noescape
func quantBlock4(src *uint8, stride int, pred *uint8, pstride int, qv *quantVecs, levels *[16]int16, rec *uint8, rstride int) uint32

//go:noescape
func quantDC4(coeffs *[16]int32, dc int, qv *quantVecs, levels *[16]int16, pred *uint8, pstride int, rec *uint8, rstride int)

//go:noescape
func fdctBlock4(src *uint8, stride int, pred *uint8, pstride int, k *[12]int32, out *[16]int32)

//go:noescape
func fdctBins4(src *uint8, stride int, pred *uint8, pstride int, k *[12]int32, bins *[16]uint8)

//go:noescape
func satd4NEON(src *uint8, stride int, pred *[16]uint8) uint32

//go:noescape
func ttransform4(in *uint8, stride int, w *[16]int32) uint32

func quantBlock(m *quantMatrix, src []uint8, s, x, y int, pred []uint8, ps int, levels *[16]int16, rec []uint8, rs int) int64 {
	_, _, _ = src[(y+3)*s+x+3], pred[3*ps+3], rec[3*rs+3]
	return int64(quantBlock4(&src[y*s+x], s, &pred[0], ps, &m.vec, levels, &rec[0], rs))
}

func quantBlockDC(m *quantMatrix, coeffs *[16]int32, dc int32, levels *[16]int16, pred []uint8, ps int, rec []uint8, rs int) {
	_, _ = pred[3*ps+3], rec[3*rs+3]
	quantDC4(coeffs, int(dc), &m.vec, levels, &pred[0], ps, &rec[0], rs)
}

func fdctBlock(src []uint8, s, x, y int, pred []uint8, ps int) [16]int32 {
	_, _ = src[(y+3)*s+x+3], pred[3*ps+3]
	var out [16]int32
	fdctBlock4(&src[y*s+x], s, &pred[0], ps, &transformConsts, &out)
	return out
}

func fdctBins(src []uint8, s, x, y int, pred []uint8, ps int) [16]uint8 {
	_, _ = src[(y+3)*s+x+3], pred[3*ps+3]
	var bins [16]uint8
	fdctBins4(&src[y*s+x], s, &pred[0], ps, &transformConsts, &bins)
	return bins
}

func satd4(src []uint8, s, x, y int, pred *[16]uint8) int64 {
	_ = src[(y+3)*s+x+3]
	return int64(satd4NEON(&src[y*s+x], s, pred))
}

func ttransform(in []uint8, s int) int32 {
	_ = in[3*s+3]
	return int32(ttransform4(&in[0], s, &tdistoWeight))
}
