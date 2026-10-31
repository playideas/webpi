package webpi

import "math"

// 확률 갱신 비용 — 새 확률값은 8비트로 보낸다
const (
	probValueBits = 8
	minProb       = 1
	maxProb       = 255
	probScale     = 256
)

// nzContext는 0이 아닌 계수 유무 문맥이다: 0~3 휘도 열/행, 4~5 U, 6~7 V, 8 Y2.
type nzContext [9]int

const nzY2 = 8

// writeTokens는 모든 매크로블록의 계수 토큰을 래스터 순서로 쓴다 (문맥은 디코더와 같게 유지).
func (e *encoder) writeTokens(w *tokenWriter) {
	top := make([]nzContext, e.src.mbw)
	for mby := range e.src.mbh {
		var left nzContext
		for mbx := range e.src.mbw {
			mb := &e.mbs[mby*e.src.mbw+mbx]
			if mb.skip && e.useSkip {
				skipContext(mb, &top[mbx], &left)
				continue
			}
			writeLumaTokens(w, mb, &top[mbx], &left)
			writeChromaTokens(w, mb, &top[mbx], &left)
		}
	}
}

// skipContext는 건너뛴 매크로블록의 문맥을 비운다. Y2가 없는 i4 블록은 Y2 문맥을 그대로 둔다.
func skipContext(mb *mbInfo, top, left *nzContext) {
	for i := range nzY2 {
		top[i], left[i] = 0, 0
	}
	if !mb.i4 {
		top[nzY2], left[nzY2] = 0, 0
	}
}

// writeLumaTokens는 (i16이면 Y2 다음) 휘도 블록 16개를 쓴다.
func writeLumaTokens(w *tokenWriter, mb *mbInfo, top, left *nzContext) {
	plane, first := planeY1SansY2, 0
	if !mb.i4 {
		nz := w.block(&mb.levels[y2Block], planeY2, top[nzY2]+left[nzY2], 0)
		top[nzY2], left[nzY2] = nz, nz
		plane, first = planeY1WithY2, 1
	}
	for y := range 4 {
		for x := range 4 {
			nz := w.block(&mb.levels[y*4+x], plane, top[x]+left[y], first)
			top[x], left[y] = nz, nz
		}
	}
}

func writeChromaTokens(w *tokenWriter, mb *mbInfo, top, left *nzContext) {
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
