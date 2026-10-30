package webpi

import "math"

// 확률 갱신 비용 — 새 확률값은 8비트로 보낸다
const (
	probValueBits = 8
	minProb       = 1
	maxProb       = 255
	probScale     = 256
)

// writeTokens는 모든 매크로블록의 계수 토큰을 래스터 순서로 쓴다 (문맥은 디코더와 같게 유지).
func (e *encoder) writeTokens(w *tokenWriter) {
	// 문맥 배열: 0~3 휘도 열/행, 4~5 U, 6~7 V, 8 Y2
	top := make([][9]int, e.src.mbw)
	for mby := range e.src.mbh {
		var left [9]int
		for mbx := range e.src.mbw {
			mb := &e.mbs[mby*e.src.mbw+mbx]
			if mb.skip && e.useSkip {
				top[mbx], left = [9]int{}, [9]int{}
				continue
			}
			writeMBTokens(w, mb, &top[mbx], &left)
		}
	}
}

func writeMBTokens(w *tokenWriter, mb *mbInfo, top, left *[9]int) {
	nz := w.block(&mb.levels[y2Block], planeY2, top[8]+left[8], 0)
	top[8], left[8] = nz, nz
	for y := range 4 {
		for x := range 4 {
			nz := w.block(&mb.levels[y*4+x], planeY1WithY2, top[x]+left[y], 1)
			top[x], left[y] = nz, nz
		}
	}
	for ch, base := range [2]int{uBlockBase, vBlockBase} {
		for y := range 2 {
			for x := range 2 {
				ti, li := 4+ch*2+x, 4+ch*2+y
				nz := w.block(&mb.levels[base+y*2+x], planeUV, top[ti]+left[li], 0)
				top[ti], left[li] = nz, nz
			}
		}
	}
}

// writeProbUpdates는 노드마다 갱신이 이득이면 새 확률을 보내고 e.probs에 반영한다.
func (e *encoder) writeProbUpdates(h *boolEncoder, stats *tokenStats) {
	for i := range nPlane {
		for j := range nBand {
			for k := range nContext {
				for l := range nProb {
					upd := tokenProbUpdateProb[i][j][k][l]
					old := e.probs[i][j][k][l]
					c := stats[i][j][k][l]
					np := probFromCounts(c[0], c[1])
					gain := treeCost(old, c) - treeCost(np, c) -
						(bitCost(upd, true) + probValueBits - bitCost(upd, false))
					doUpdate := c[0]+c[1] > 0 && np != old && gain > 0
					h.putBit(upd, doUpdate)
					if doUpdate {
						h.putUint(uint32(np), probValueBits)
						e.probs[i][j][k][l] = np
					}
				}
			}
		}
	}
}

// probFromCounts는 0이 나온 비율로 확률(0일 확률 × 256)을 정한다.
func probFromCounts(zeros, ones uint32) uint8 {
	total := zeros + ones
	if total == 0 {
		return probScale / 2
	}
	p := (uint64(zeros)*probScale + uint64(total)/2) / uint64(total)
	return uint8(min(max(p, minProb), maxProb))
}

// bitCost는 확률 p에서 bit를 쓰는 비용(비트)이다.
func bitCost(p uint8, bit bool) float64 {
	q := float64(p) / probScale
	if bit {
		q = 1 - q
	}
	return -math.Log2(q)
}

func treeCost(p uint8, c [2]uint32) float64 {
	return float64(c[0])*bitCost(p, false) + float64(c[1])*bitCost(p, true)
}
