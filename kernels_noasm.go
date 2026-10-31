//go:build !arm64

package webpi

func quantBlock(m *quantMatrix, src []uint8, s, x, y int, pred *[16]uint8, levels *[16]int16, rec *[16]uint8) int64 {
	return quantBlockGo(m, src, s, x, y, pred, levels, rec)
}

func satd4(src []uint8, s, x, y int, pred *[16]uint8) int64 { return satd4Go(src, s, x, y, pred) }
