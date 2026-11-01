//go:build !arm64 || purego

// 순수 Go 커널 — arm64가 아니거나 purego 태그로 빌드할 때 쓴다.

package webpi

func quantBlock(m *quantMatrix, src []uint8, s, x, y int, pred *[16]uint8, levels *[16]int16, rec *[16]uint8) int64 {
	return quantBlockGo(m, src, s, x, y, pred, levels, rec)
}

func satd4(src []uint8, s, x, y int, pred *[16]uint8) int64 { return satd4Go(src, s, x, y, pred) }
