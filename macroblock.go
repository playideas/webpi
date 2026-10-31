package webpi

// 매크로블록 내 레벨 배열 배치: 0~15 휘도(래스터), 16~19 U, 20~23 V, 24 Y2
const (
	nYBlocks   = 16
	uBlockBase = 16
	vBlockBase = 20
	y2Block    = 24
	nBlocks    = 25
	mbSize     = 16
	uvSize     = 8
)

// mbInfo는 한 매크로블록의 결정과 양자화 레벨이다.
type mbInfo struct {
	yMode, uvMode int
	levels        [nBlocks][16]int32
	skip          bool
}

// encodeMB는 (mbx,mby) 매크로블록의 모드를 고르고 양자화한 뒤 rec에 복원값을 쓴다.
// 복원은 디코더와 비트 단위로 같아야 다음 블록의 예측이 어긋나지 않는다.
func (e *encoder) encodeMB(mbx, mby int) mbInfo {
	var mb mbInfo
	q := &e.seg.quant[e.seg.ids[mby*e.src.mbw+mbx]]
	x0, y0 := mbx*mbSize, mby*mbSize
	ye := loadEdges(e.rec.y, e.rec.ys, x0, y0, mbSize)
	mb.yMode = bestMode(e.src.y, e.src.ys, x0, y0, mbSize, &ye)
	e.encodeLuma(&mb, q, predict(mb.yMode, &ye, mbSize), x0, y0)

	cx, cy := mbx*uvSize, mby*uvSize
	ue := loadEdges(e.rec.u, e.rec.cs, cx, cy, uvSize)
	ve := loadEdges(e.rec.v, e.rec.cs, cx, cy, uvSize)
	mb.uvMode = bestChromaMode(e.src, cx, cy, &ue, &ve)
	e.encodeChroma(&mb, q, e.src.u, e.rec.u, predict(mb.uvMode, &ue, uvSize), uBlockBase, cx, cy)
	e.encodeChroma(&mb, q, e.src.v, e.rec.v, predict(mb.uvMode, &ve, uvSize), vBlockBase, cx, cy)

	mb.skip = true
	for b := range mb.levels {
		for _, l := range mb.levels[b] {
			if l != 0 {
				mb.skip = false
			}
		}
	}
	return mb
}

// encodeLuma는 16×16 예측 잔차를 블록 DCT + Y2(WHT)로 양자화하고 복원한다.
func (e *encoder) encodeLuma(mb *mbInfo, q *quantizer, pred []uint8, x0, y0 int) {
	var coeffs [nYBlocks][16]int32
	var dcs [16]int32
	for b := range nYBlocks {
		bx, by := (b%4)*4, (b/4)*4
		var res [16]int32
		for j := range 4 {
			for i := range 4 {
				s := e.src.y[(y0+by+j)*e.src.ys+x0+bx+i]
				res[j*4+i] = int32(s) - int32(pred[(by+j)*mbSize+bx+i])
			}
		}
		coeffs[b] = fdct4(&res)
		dcs[b] = coeffs[b][0]
	}
	y2 := fwht(&dcs)
	y2deq := q.y2.quantize(&y2, 0)
	mb.levels[y2Block] = y2
	dcRec := iwht(&y2deq)
	for b := range nYBlocks {
		deq := q.y1.quantize(&coeffs[b], 1)
		coeffs[b][0] = 0
		mb.levels[b] = coeffs[b]
		deq[0] = dcRec[b]
		bx, by := (b%4)*4, (b/4)*4
		storeBlock(e.rec.y, e.rec.ys, x0+bx, y0+by, &deq, pred, mbSize, bx, by)
	}
}

// encodeChroma는 한 색차 평면의 8×8 잔차를 4개 블록으로 양자화하고 복원한다.
func (e *encoder) encodeChroma(mb *mbInfo, q *quantizer, src, rec, pred []uint8, base, cx, cy int) {
	s := e.src.cs
	for b := range 4 {
		bx, by := (b%2)*4, (b/2)*4
		var res [16]int32
		for j := range 4 {
			for i := range 4 {
				res[j*4+i] = int32(src[(cy+by+j)*s+cx+bx+i]) - int32(pred[(by+j)*uvSize+bx+i])
			}
		}
		c := fdct4(&res)
		deq := q.uv.quantize(&c, 0)
		mb.levels[base+b] = c
		storeBlock(rec, s, cx+bx, cy+by, &deq, pred, uvSize, bx, by)
	}
}

// storeBlock은 역양자화 계수를 예측(pred, 폭 pn의 (px,py))에 더해 평면 pl의 (x,y)에 쓴다.
func storeBlock(pl []uint8, s, x, y int, deq *[16]int32, pred []uint8, pn, px, py int) {
	var p [16]uint8
	for j := range 4 {
		for i := range 4 {
			p[j*4+i] = pred[(py+j)*pn+px+i]
		}
	}
	out := idct4Add(deq, &p)
	for j := range 4 {
		copy(pl[(y+j)*s+x:(y+j)*s+x+4], out[j*4:j*4+4])
	}
}

// bestMode는 예측 오차(SSE)가 가장 작은 모드를 고른다. RD 기반 결정은 다음 단계.
func bestMode(pl []uint8, s, x0, y0, n int, e *edges) int {
	best, bestSSE := predDC, -1
	for m := range nPredModes {
		sse := blockSSE(pl, s, x0, y0, n, predict(m, e, n))
		if bestSSE < 0 || sse < bestSSE {
			best, bestSSE = m, sse
		}
	}
	return best
}

func bestChromaMode(src *yuvPlanes, cx, cy int, ue, ve *edges) int {
	best, bestSSE := predDC, -1
	for m := range nPredModes {
		sse := blockSSE(src.u, src.cs, cx, cy, uvSize, predict(m, ue, uvSize)) +
			blockSSE(src.v, src.cs, cx, cy, uvSize, predict(m, ve, uvSize))
		if bestSSE < 0 || sse < bestSSE {
			best, bestSSE = m, sse
		}
	}
	return best
}

func blockSSE(pl []uint8, s, x0, y0, n int, pred []uint8) int {
	sum := 0
	for j := range n {
		for i := range n {
			d := int(pl[(y0+j)*s+x0+i]) - int(pred[j*n+i])
			sum += d * d
		}
	}
	return sum
}
