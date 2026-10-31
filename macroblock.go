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
	nUVBlocks  = 8
)

// mbInfo는 한 매크로블록의 결정과 양자화 레벨이다.
// i4가 아니면 bModes는 yMode로 채워 둔다 (이웃의 4×4 모드 문맥).
type mbInfo struct {
	yMode, uvMode int
	i4            bool
	bModes        [nYBlocks]uint8
	levels        [nBlocks][16]int32
	skip          bool
}

// rowCoder는 한 매크로블록 행을 인코딩하는 작업자다. 행 안에서만 이어지는 문맥(왼쪽 이웃)과
// 현재 매크로블록의 원본 텍스처를 따로 가져, 여러 행을 동시에 인코딩할 수 있게 한다.
type rowCoder struct {
	*encoder
	nzLeft nzContext
	modes  modeContext // top은 프레임 공유, left는 행 전용
	srcT   [nYBlocks]int32
	ws     lumaWorkspace // i4 탐색용, 매크로블록마다 다시 채운다
}

// lumaResult는 휘도 후보 하나(i16 또는 i4)의 결정, 복원 픽셀, RD 점수다.
type lumaResult struct {
	mb    mbInfo
	rec   [mbSize * mbSize]uint8
	score rdScore
}

// encodeMB는 (mbx,mby) 매크로블록의 모드를 RD 점수로 고르고 양자화한 뒤 rec에 복원값을 쓴다.
// 복원은 디코더와 비트 단위로 같아야 다음 블록의 예측이 어긋나지 않는다.
// 판단 PD-20261031-873a974c: i4 예측과 RD(왜곡 + λ·비트) 모드 결정을 함께 넣는다
func (e *rowCoder) encodeMB(mbx, mby int) mbInfo {
	seg := e.seg.ids[mby*e.src.mbw+mbx]
	q, lam := &e.seg.quant[seg], &e.lambdas[seg]
	// 원본 쪽 텍스처 변환은 모드와 무관하므로 매크로블록마다 한 번만 구한다
	for b := range nYBlocks {
		e.srcT[b] = ttransform(e.src.y[(mby*mbSize+(b/4)*4)*e.src.ys+mbx*mbSize+(b%4)*4:], e.src.ys)
	}
	luma := e.pickI16(q, lam, mbx, mby)
	if i4, ok := e.pickI4(q, lam, mbx, mby, luma.score.score); ok {
		luma = i4
	}
	mb := luma.mb
	copyBlock(e.rec.y, e.rec.ys, mbx*mbSize, mby*mbSize, luma.rec[:], mbSize)
	e.pickUV(&mb, q, lam, mbx, mby)

	mb.skip = true
	for b := range mb.levels {
		if nonZero(&mb.levels[b]) {
			mb.skip = false
		}
	}
	// 다음 매크로블록의 비용 계산을 위해 문맥을 넘긴다 (쓰기 없이 문맥만 계산)
	var w tokenWriter
	writeLumaTokens(&w, &mb, &e.nzTop[mbx], &e.nzLeft)
	writeChromaTokens(&w, &mb, &e.nzTop[mbx], &e.nzLeft)
	e.modes.update(&mb, mbx)
	return mb
}

// pickI16은 16×16 모드 4개 중 λ_i16 점수가 가장 낮은 것을 고르고, i4와 비교할 수 있게 λ_mode로 다시 매긴다.
func (e *rowCoder) pickI16(q *quantizer, lam *rdLambdas, mbx, mby int) lumaResult {
	x0, y0 := mbx*mbSize, mby*mbSize
	ye := loadEdges(e.rec.y, e.rec.ys, x0, y0, mbSize)
	flat := isFlatSource(e.src.y, e.src.ys, x0, y0)
	var best lumaResult
	var buf predBuf
	for m := range nPredModes {
		var c lumaResult
		c.mb.yMode = m
		e.quantLuma16(q, predict(m, &ye, mbSize, &buf), x0, y0, &c)
		c.score.d, c.score.sd = regionDisto(e.src.y, e.src.ys, x0, y0, c.rec[:], mbSize, lam.t, e.srcT[:])
		c.score.h = i16ModeCost[m]
		c.score.r = e.lumaRate(&c.mb, mbx)
		// 원본이 완전히 평탄하면 왜곡을 더 무겁게 본다 — 평탄한 면의 얼룩이 가장 눈에 띈다
		if flat = flat && isFlat(c.mb.levels[:nYBlocks], flatnessLimitI16); flat {
			c.score.d, c.score.sd = 2*c.score.d, 2*c.score.sd
		}
		c.score.setScore(lam.i16)
		if m == 0 || c.score.score < best.score.score {
			best = c
		}
	}
	best.score.setScore(lam.mode)
	for i := range best.mb.bModes {
		best.mb.bModes[i] = uint8(best.mb.yMode)
	}
	return best
}

// quantLuma16은 16×16 예측 잔차를 블록 DCT + Y2(WHT)로 양자화하고 c.rec에 복원한다.
func (e *rowCoder) quantLuma16(q *quantizer, pred []uint8, x0, y0 int, c *lumaResult) {
	var coeffs [nYBlocks][16]int32
	var preds [nYBlocks][16]uint8
	var dcs [16]int32
	for b := range nYBlocks {
		bx, by := (b%4)*4, (b/4)*4
		preds[b] = sub4(pred, mbSize, bx, by)
		res := residual4(e.src.y, e.src.ys, x0+bx, y0+by, &preds[b])
		coeffs[b] = fdct4(&res)
		dcs[b] = coeffs[b][0]
	}
	y2 := fwht(&dcs)
	y2deq := q.y2.quantize(&y2, 0)
	c.mb.levels[y2Block] = y2
	dcRec := iwht(&y2deq)
	for b := range nYBlocks {
		deq := q.y1.quantize(&coeffs[b], 1)
		coeffs[b][0] = 0
		c.mb.levels[b] = coeffs[b]
		deq[0] = dcRec[b]
		px := idct4Add(&deq, &preds[b])
		copyBlock(c.rec[:], mbSize, (b%4)*4, (b/4)*4, px[:], 4)
	}
}

// lumaRate는 현재 문맥에서 휘도 토큰의 비용이다.
func (e *rowCoder) lumaRate(mb *mbInfo, mbx int) int64 {
	var r int64
	top, left := e.nzTop[mbx], e.nzLeft
	writeLumaTokens(&tokenWriter{rate: &r, costs: e.costs}, mb, &top, &left)
	return r
}

// pickI4는 4×4 블록마다 10개 모드 중 가장 좋은 것을 고른다. 누적 점수가 limit(i16 점수)을
// 넘는 순간 포기한다.
func (e *rowCoder) pickI4(q *quantizer, lam *rdLambdas, mbx, mby int, limit int64) (lumaResult, bool) {
	ws := &e.ws
	ws.load(e.rec, mbx, mby)
	var res lumaResult
	res.mb.i4 = true
	res.score.h = i4HeaderCost
	res.score.setScore(lam.mode)
	tnz, lnz := e.nzTop[mbx], e.nzLeft
	for b := range nYBlocks {
		bx, by := b%4, b/4
		above, left := e.modes.neighbors(&res.mb, mbx, b)
		blk := e.pickBlock4(q, lam, ws, mbx*mbSize+bx*4, mby*mbSize+by*4, bx, by, above, left, tnz[bx]+lnz[by])
		blk.score.setScore(lam.mode)
		res.score.add(&blk.score)
		if res.score.score >= limit {
			return res, false
		}
		res.mb.bModes[b], res.mb.levels[b] = blk.mode, blk.levels
		ws.store4(bx, by, &blk.rec)
		nz := int(b2i(nonZero(&blk.levels)))
		tnz[bx], lnz[by] = nz, nz
	}
	for j := range mbSize {
		copy(res.rec[j*mbSize:(j+1)*mbSize], ws[1+j][1:1+mbSize])
	}
	return res, true
}

// i4Candidates는 예측 오차로 추린 뒤 RD로 끝까지 비교하는 4×4 모드 수다.
// 전부 비교한다 — 줄이면 빨라지지만(5개에서 ~20%) 모든 지표가 ~0.4% 나빠졌다.
// 예측 오차 순서로 비교하는 것만으로도 앞선 후보가 기준점이 되어 조기 종료가 잘 걸린다.
const i4Candidates = nBModes

type block4Result struct {
	mode   uint8
	levels [16]int32
	rec    [16]uint8
	score  rdScore
}

// pickBlock4는 한 4×4 블록의 모드를 λ_i4 점수로 고른다.
// 예측 오차(SSE)와 모드 비용으로 후보를 먼저 줄인 뒤, 남은 후보만 양자화·복원해 RD 점수로 비교한다.
func (e *rowCoder) pickBlock4(q *quantizer, lam *rdLambdas, ws *lumaWorkspace, sx, sy, bx, by int,
	above, left uint8, ctx int) block4Result {
	edges := ws.edges4(bx, by)
	var preds [nBModes][16]uint8
	var order [nBModes]int
	var est [nBModes]int64
	for m := range nBModes {
		preds[m] = pred4(m, &edges)
		est[m] = rdDistoMult*predSSE4(e.src.y, e.src.ys, sx, sy, &preds[m]) + lam.i4*bmodeCost[above][left][m]
		// 삽입 정렬 — 원소가 10개뿐이다
		k := m
		for ; k > 0 && est[order[k-1]] > est[m]; k-- {
			order[k] = order[k-1]
		}
		order[k] = m
	}
	var best block4Result
	for i, m := range order[:i4Candidates] {
		c := block4Result{mode: uint8(m)}
		c.levels, c.rec = quantBlock(&q.y1, e.src.y, e.src.ys, sx, sy, &preds[m])
		// 점수의 항은 모두 0 이상이므로, 싼 항만으로 이미 최선보다 나쁘면 나머지는 셀 필요가 없다
		c.score.d = regionSSE(e.src.y, e.src.ys, sx, sy, c.rec[:], 4)
		c.score.h = bmodeCost[above][left][m]
		c.score.setScore(lam.i4)
		if i > 0 && c.score.score >= best.score.score {
			continue
		}
		c.score.sd = regionTDisto(c.rec[:], 4, lam.t, e.srcT[by*4+bx:])
		if m > 0 && isFlat([][16]int32{c.levels}, flatnessLimitI4) {
			c.score.r = flatnessPenalty
		}
		c.score.setScore(lam.i4)
		if i > 0 && c.score.score >= best.score.score {
			continue
		}
		r, _ := e.costs.residual(&c.levels, planeY1SansY2, ctx, 0)
		c.score.r += r
		c.score.setScore(lam.i4)
		if i == 0 || c.score.score < best.score.score {
			best = c
		}
	}
	return best
}

// predSSE4는 원본 4×4와 예측의 제곱 오차 합이다.
func predSSE4(src []uint8, s, x, y int, pred *[16]uint8) int64 {
	var d int64
	for j := range 4 {
		row := src[(y+j)*s+x : (y+j)*s+x+4]
		for i, v := range row {
			e := int64(v) - int64(pred[j*4+i])
			d += e * e
		}
	}
	return d
}

// pickUV는 색차 모드 4개 중 λ_uv 점수가 가장 낮은 것을 골라 mb와 rec에 반영한다.
// 색차에는 텍스처 왜곡을 쓰지 않는다 — 면을 평탄하게 만드는 경향이 있다 (libwebp와 같음).
func (e *rowCoder) pickUV(mb *mbInfo, q *quantizer, lam *rdLambdas, mbx, mby int) {
	cx, cy := mbx*uvSize, mby*uvSize
	es := [2]edges{
		loadEdges(e.rec.u, e.rec.cs, cx, cy, uvSize),
		loadEdges(e.rec.v, e.rec.cs, cx, cy, uvSize),
	}
	var best rdScore
	var bestMB mbInfo
	var bestRec [2][uvSize * uvSize]uint8
	var buf predBuf
	for m := range nPredModes {
		c := mbInfo{uvMode: m}
		var rec [2][uvSize * uvSize]uint8
		var s rdScore
		for ch, pl := range [2][]uint8{e.src.u, e.src.v} {
			s.d += e.quantChroma(&c, q, pl, predict(m, &es[ch], uvSize, &buf), uBlockBase+ch*4, cx, cy, rec[ch][:])
		}
		s.h = uvModeCost[m]
		s.r = e.chromaRate(&c, mbx)
		if m > 0 && isFlat(c.levels[uBlockBase:uBlockBase+nUVBlocks], flatnessLimitUV) {
			s.r += flatnessPenalty * nUVBlocks
		}
		s.setScore(lam.uv)
		if m == 0 || s.score < best.score {
			best, bestMB, bestRec = s, c, rec
		}
	}
	mb.uvMode = bestMB.uvMode
	copy(mb.levels[uBlockBase:uBlockBase+nUVBlocks], bestMB.levels[uBlockBase:uBlockBase+nUVBlocks])
	copyBlock(e.rec.u, e.rec.cs, cx, cy, bestRec[0][:], uvSize)
	copyBlock(e.rec.v, e.rec.cs, cx, cy, bestRec[1][:], uvSize)
}

// quantChroma는 한 색차 평면의 8×8 잔차를 4개 블록으로 양자화해 rec(8×8)에 복원하고 SSE를 돌려준다.
func (e *rowCoder) quantChroma(mb *mbInfo, q *quantizer, src, pred []uint8, base, cx, cy int, rec []uint8) int64 {
	for b := range 4 {
		bx, by := (b%2)*4, (b/2)*4
		p := sub4(pred, uvSize, bx, by)
		var px [16]uint8
		mb.levels[base+b], px = quantBlock(&q.uv, src, e.src.cs, cx+bx, cy+by, &p)
		copyBlock(rec, uvSize, bx, by, px[:], 4)
	}
	d, _ := regionDisto(src, e.src.cs, cx, cy, rec, uvSize, 0, nil)
	return d
}

func (e *rowCoder) chromaRate(mb *mbInfo, mbx int) int64 {
	var r int64
	top, left := e.nzTop[mbx], e.nzLeft
	writeChromaTokens(&tokenWriter{rate: &r, costs: e.costs}, mb, &top, &left)
	return r
}

// quantBlock은 4×4 블록 하나를 변환·양자화(DC 포함)하고 레벨과 복원 픽셀을 돌려준다.
func quantBlock(m *quantMatrix, src []uint8, s, x, y int, pred *[16]uint8) ([16]int32, [16]uint8) {
	res := residual4(src, s, x, y, pred)
	levels := fdct4(&res)
	deq := m.quantize(&levels, 0)
	return levels, idct4Add(&deq, pred)
}

func residual4(src []uint8, s, x, y int, pred *[16]uint8) [16]int32 {
	var res [16]int32
	for j := range 4 {
		for i := range 4 {
			res[j*4+i] = int32(src[(y+j)*s+x+i]) - int32(pred[j*4+i])
		}
	}
	return res
}

// sub4는 폭 n인 블록 p에서 (x,y)의 4×4를 잘라 낸다.
func sub4(p []uint8, n, x, y int) [16]uint8 {
	var out [16]uint8
	for j := range 4 {
		copy(out[j*4:j*4+4], p[(y+j)*n+x:])
	}
	return out
}

// copyBlock은 폭 n인 블록 src를 평면 dst(stride s)의 (x,y)에 복사한다.
func copyBlock(dst []uint8, s, x, y int, src []uint8, n int) {
	for j := range n {
		copy(dst[(y+j)*s+x:(y+j)*s+x+n], src[j*n:(j+1)*n])
	}
}

func nonZero(levels *[16]int32) bool {
	for _, l := range levels {
		if l != 0 {
			return true
		}
	}
	return false
}

// isFlatSource는 16×16 원본이 한 값으로만 이루어졌는지 본다.
func isFlatSource(pl []uint8, s, x0, y0 int) bool {
	v := pl[y0*s+x0]
	for j := range mbSize {
		for _, p := range pl[(y0+j)*s+x0 : (y0+j)*s+x0+mbSize] {
			if p != v {
				return false
			}
		}
	}
	return true
}
