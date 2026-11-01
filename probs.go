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

// writeTokens는 모든 매크로블록의 계수 토큰을 래스터 순서로 파티션 ws에 나눠 쓴다 (문맥은 디코더와 같게 유지).
func (e *encoder) writeTokens(ws []*tokenWriter) {
	top := make([]nzContext, e.src.mbw)
	var mb mbInfo
	for mby := range e.src.mbh {
		// 행마다 파티션을 돌아가며 쓴다 — 문맥(top·left)은 파티션과 상관없이 래스터 순서로 이어진다
		w := ws[mby%len(ws)]
		var left nzContext
		coefs := e.coefs[mby]
		for mbx := range e.src.mbw {
			mb.mbHeader = e.mbs[mby*e.src.mbw+mbx]
			// useSkip이 꺼져 있으면 건너뛸 매크로블록이 하나도 없다
			if mb.skip {
				skipContext(&mb.mbHeader, &top[mbx], &left)
				continue
			}
			coefs = unpackLevels(coefs, &mb.levels)
			writeLumaTokens(w, &mb, &top[mbx], &left)
			writeChromaTokens(w, &mb, &top[mbx], &left)
		}
	}
}

// skipContext는 건너뛴 매크로블록의 문맥을 비운다. Y2가 없는 i4 블록은 Y2 문맥을 그대로 둔다.
func skipContext(mb *mbHeader, top, left *nzContext) {
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
					np, doUpdate := probUpdate(upd, e.probs[i][j][k][l], stats[i][j][k][l])
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

// probUpdate는 횟수 c에 맞춘 새 확률과, 갱신 신호 비용을 치르고도 이득인지를 돌려준다.
func probUpdate(upd, old uint8, c [2]uint32) (uint8, bool) {
	np := probFromCounts(c[0], c[1])
	gain := treeCost(old, c) - treeCost(np, c) - (bitCost(upd, true) + probValueBits - bitCost(upd, false))
	return np, c[0]+c[1] > 0 && np != old && gain > 0
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
