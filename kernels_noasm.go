//go:build !arm64 || purego

// 순수 Go 커널 — arm64가 아니거나 purego 태그로 빌드할 때 쓴다.
package webpi

func quantBlock(m *quantMatrix, src []uint8, s, x, y int, pred []uint8, ps int, levels *[16]int16, rec []uint8, rs int) int64 {
	return quantBlockGo(m, src, s, x, y, pred, ps, levels, rec, rs)
}

func quantBlockDC(m *quantMatrix, coeffs *[16]int32, dc int32, levels *[16]int16, pred []uint8, ps int, rec []uint8, rs int) {
	quantBlockDCGo(m, coeffs, dc, levels, pred, ps, rec, rs)
}

func fdctBlock(src []uint8, s, x, y int, pred []uint8, ps int) [16]int32 {
	return fdctBlockGo(src, s, x, y, pred, ps)
}

func fdctBins(src []uint8, s, x, y int, pred []uint8, ps int) [16]uint8 {
	return fdctBinsGo(src, s, x, y, pred, ps)
}

func satd4(src []uint8, s, x, y int, pred *[16]uint8) int64 { return satd4Go(src, s, x, y, pred) }

func ttransform(in []uint8, s int) int32 { return ttransformGo(in, s) }
