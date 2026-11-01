package webpi

import "math"

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

// mbHeader는 한 매크로블록의 모드 결정이다. 프레임 배열에는 이것만 남기고 계수는 행 버퍼에 압축해 둔다.
// i4가 아니면 bModes는 yMode로 채워 둔다 (이웃의 4×4 모드 문맥).
type mbHeader struct {
	yMode, uvMode int
	i4            bool
	bModes        [nYBlocks]uint8
	skip          bool
}

// mbInfo는 인코딩 중인 매크로블록의 결정과 양자화 레벨이다.
type mbInfo struct {
	mbHeader
	levels [nBlocks][16]int16 // 레벨은 int16 — 후보 작업 공간·복사량이 절반이다
}

// rowCoder는 한 매크로블록 행을 인코딩하는 작업자다. 행 안에서만 이어지는 문맥(왼쪽 이웃)과
// 현재 매크로블록의 원본 텍스처를 따로 가져, 여러 행을 동시에 인코딩할 수 있게 한다.
type rowCoder struct {
	*encoder
	nzLeft nzContext
	modes  modeContext // top은 프레임 공유, left는 행 전용
	srcT   [nYBlocks]int32
	ws     lumaWorkspace // i4 탐색용, 매크로블록마다 다시 채운다
	stats  *tokenStats   // 이 행의 토큰 통계 — 프레임 확률 갱신에 쓴다
	coefs  *[]int16      // 이 행의 압축 계수 (packLevels)
	// 휘도 후보 작업 공간 — 2KB짜리 결과를 값으로 주고받지 않도록 행마다 한 벌 둔다
	i16 [2]lumaResult
	i4  lumaResult
	mb  mbInfo
}

// lumaResult는 휘도 후보 하나(i16 또는 i4)의 결정, 복원 픽셀, RD 점수다.
type lumaResult struct {
	mb    mbInfo
	rec   [mbSize * mbSize]uint8
	score rdScore
}

// reset은 다음 후보를 위해 결정과 점수를 비운다. 레벨·복원 픽셀은 후보가 쓰는 칸을 모두 덮어쓰고
// 쓰지 않는 칸(i16의 색차, i4의 Y2·색차)은 처음부터 0이라 지우지 않는다 — 매크로블록마다 ~10KB를 아낀다.
func (c *lumaResult) reset() {
	c.mb.mbHeader = mbHeader{}
	c.score = rdScore{}
}

// encodeMB는 (mbx,mby) 매크로블록의 모드를 RD 점수로 고르고 양자화한 뒤 rec에 복원값을 쓴다.
// 복원은 디코더와 비트 단위로 같아야 다음 블록의 예측이 어긋나지 않는다.
// 판단 PD-20261031-873a974c: i4 예측과 RD(왜곡 + λ·비트) 모드 결정을 함께 넣는다
func (e *rowCoder) encodeMB(mbx, mby int, out *mbHeader) {
	seg := e.seg.ids[mby*e.src.mbw+mbx]
	q, lam := &e.seg.quant[seg], &e.lambdas[seg]
	// 원본 쪽 텍스처 변환은 모드와 무관하므로 매크로블록마다 한 번만 구한다
	for b := range nYBlocks {
		e.srcT[b] = ttransform(e.src.y[(mby*mbSize+(b/4)*4)*e.src.ys+mbx*mbSize+(b%4)*4:], e.src.ys)
	}
	luma := e.pickI16(q, lam, mbx, mby)
	if e.pickI4(q, lam, mbx, mby, luma.score.score) {
		luma = &e.i4
	}
	// 결정은 캐시에 있는 지역 변수에서 마무리하고 프레임 배열에는 한 번만 쓴다
	mb := &e.mb
	*mb = luma.mb
	copyBlock(e.rec.y, e.rec.ys, mbx*mbSize, mby*mbSize, luma.rec[:], mbSize)
	e.pickUV(mb, q, lam, mbx, mby)

	mb.skip = true
	for b := range mb.levels {
		if nonZero(&mb.levels[b]) {
			mb.skip = false
		}
	}
	// 다음 매크로블록의 비용 계산을 위해 문맥을 넘기면서 토큰 통계를 모은다.
	// 건너뛸 매크로블록은 토큰을 쓰지 않지만, 계수가 모두 0이라 문맥은 토큰을 썼을 때와 같다.
	var w tokenWriter
	if !mb.skip {
		w.stats = e.stats
	}
	writeLumaTokens(&w, mb, &e.nzTop[mbx], &e.nzLeft)
	writeChromaTokens(&w, mb, &e.nzTop[mbx], &e.nzLeft)
	e.modes.update(&mb.mbHeader, mbx)
	// 프레임 전체 레벨(매크로블록당 1.6KB)을 그대로 쓰면 메모리 쓰기가 인코딩 시간의 ~8%를 먹는다
	*out = mb.mbHeader
	if !mb.skip {
		*e.coefs = packLevels(*e.coefs, &mb.levels)
	}
}

// pickI16은 16×16 모드 4개 중 λ_i16 점수가 가장 낮은 것을 고르고, i4와 비교할 수 있게 λ_mode로 다시 매긴다.
// 결과는 rowCoder의 작업 공간을 가리킨다 (다음 매크로블록에서 덮어쓴다).
func (e *rowCoder) pickI16(q *quantizer, lam *rdLambdas, mbx, mby int) *lumaResult {
	x0, y0 := mbx*mbSize, mby*mbSize
	ye := loadEdges(e.rec.y, e.rec.ys, x0, y0, mbSize)
	flat := isFlatSource(e.src.y, e.src.ys, x0, y0)
	best, c := &e.i16[0], &e.i16[1]
	var buf predBuf
	for m := range nPredModes {
		if m == 0 {
			c = best
		}
		c.reset()
		c.mb.yMode = m
		e.quantLuma16(q, predict(m, &ye, mbSize, &buf), x0, y0, c)
		c.score.d = regionSSE(e.src.y, e.src.ys, x0, y0, c.rec[:], mbSize)
		c.score.h = i16ModeCost[m]
		// 원본이 완전히 평탄하면 왜곡을 더 무겁게 본다 — 평탄한 면의 얼룩이 가장 눈에 띈다.
		// 한 번 평탄하지 않은 모드가 나오면 이후 모드에도 적용하지 않는다 (libwebp와 같음)
		distoMul := int64(1)
		if flat = flat && isFlat(c.mb.levels[:nYBlocks], flatnessLimitI16); flat {
			distoMul = 2
		}
		c.score.d *= distoMul
		// 텍스처 왜곡과 토큰 비용은 0 이상이므로 여기서 이미 최선보다 나쁘면 셀 필요가 없다
		if c.score.setScore(lam.i16); m > 0 && c.score.score >= best.score.score {
			continue
		}
		c.score.sd = distoMul * regionTDisto(c.rec[:], mbSize, lam.t, e.srcT[:])
		c.score.r = e.lumaRate(&c.mb, mbx)
		c.score.setScore(lam.i16)
		if m == 0 {
			c = &e.i16[1]
		} else if c.score.score < best.score.score {
			best, c = c, best
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
	var dcs [16]int32
	for b := range nYBlocks {
		bx, by := (b%4)*4, (b/4)*4
		coeffs[b] = fdctBlock(e.src.y, e.src.ys, x0+bx, y0+by, pred[by*mbSize+bx:], mbSize)
		dcs[b] = coeffs[b][0]
	}
	y2 := fwht(&dcs)
	y2deq := q.y2.quantize(&y2, &c.mb.levels[y2Block], 0)
	dcRec := iwht(&y2deq)
	for b := range nYBlocks {
		off := (b/4)*4*mbSize + (b%4)*4
		quantBlockDC(&q.y1, &coeffs[b], dcRec[b], &c.mb.levels[b], pred[off:], mbSize, c.rec[off:], mbSize)
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
// 결과는 e.i4에 남는다.
func (e *rowCoder) pickI4(q *quantizer, lam *rdLambdas, mbx, mby int, limit int64) bool {
	ws := &e.ws
	ws.load(e.rec, mbx, mby)
	res := &e.i4
	res.reset()
	res.mb.i4 = true
	res.score.h = i4HeaderCost
	res.score.setScore(lam.mode)
	tnz, lnz := e.nzTop[mbx], e.nzLeft
	for b := range nYBlocks {
		bx, by := b%4, b/4
		above, left := e.modes.neighbors(&res.mb.mbHeader, mbx, b)
		blk := e.pickBlock4(q, lam, ws, mbx*mbSize+bx*4, mby*mbSize+by*4, bx, by, above, left, tnz[bx]+lnz[by])
		blk.score.setScore(lam.mode)
		res.score.add(&blk.score)
		if res.score.score >= limit {
			return false
		}
		res.mb.bModes[b], res.mb.levels[b] = blk.mode, blk.levels
		ws.store4(bx, by, &blk.rec)
		nz := int(b2i(nonZero(&blk.levels)))
		tnz[bx], lnz[by] = nz, nz
	}
	for j := range mbSize {
		copy(res.rec[j*mbSize:(j+1)*mbSize], ws[1+j][1:1+mbSize])
	}
	return true
}

type block4Result struct {
	mode   uint8
	levels [16]int16
	rec    [16]uint8
	score  rdScore
}

// i4 블록마다 끝까지 계산하는 모드 수. 나머지는 추정 점수만 보고 버린다.
const i4Candidates = 5

// pickBlock4는 한 4×4 블록에서 추정 점수가 낮은 모드 i4Candidates개만 λ_i4 점수로 비교해 고른다.
// 추정 점수 λ·(모드 비용 + SATD/q)는 예측 잔차의 하다마드 절대합을 양자화 단계로 나눈 값을 비트 수의
// 근사로 쓴다 — 실제 승자를 상위 5개 안에 넣지 못해 잃는 RD 점수가 SSE 기준의 ~40%다 (COCO 16장 측정).
// λ는 두 항에 같이 곱해지므로 순위에는 h·q + 256·SATD만 쓴다.
// 추정이 가장 좋은 모드를 먼저 끝까지 계산해 기준점으로 삼으면 나머지는 싼 항만 보고 조기 종료되기도 한다.
func (e *rowCoder) pickBlock4(q *quantizer, lam *rdLambdas, ws *lumaWorkspace, sx, sy, bx, by int,
	above, left uint8, ctx int) block4Result {
	edges := ws.edges4(bx, by)
	var preds [nBModes][16]uint8
	var order [nBModes]modeEst
	qac := int64(q.y1.q[1])
	for m := range nBModes {
		preds[m] = pred4(m, &edges)
		order[m] = modeEst{bmodeCost[above][left][m]*qac + costOne*satd4(e.src.y, e.src.ys, sx, sy, &preds[m]), uint8(m)}
	}
	selectLowest(&order, i4Candidates)
	m := int(order[0].m)
	best := e.tryBlock4(q, lam, &preds[m], m, sx, sy, bx, by, above, left, ctx, math.MaxInt64)
	for _, o := range order[1:i4Candidates] {
		m := int(o.m)
		if c := e.tryBlock4(q, lam, &preds[m], m, sx, sy, bx, by, above, left, ctx, best.score.score); c.score.score < best.score.score {
			best = c
		}
	}
	return best
}

type modeEst struct {
	est int64
	m   uint8
}

// selectLowest는 앞쪽 k칸에 추정값이 가장 낮은 k개를 오름차순으로 모은다 (같으면 모드 번호 순).
func selectLowest(o *[nBModes]modeEst, k int) {
	for i := range k {
		lo := i
		for j := i + 1; j < len(o); j++ {
			if o[j].est < o[lo].est {
				lo = j
			}
		}
		// 앞으로 당겨 순서를 유지한다 — 교환하면 같은 추정값끼리의 모드 순서가 바뀐다
		v := o[lo]
		copy(o[i+1:lo+1], o[i:lo])
		o[i] = v
	}
}

// tryBlock4는 모드 m의 RD 점수를 낸다. 점수의 항은 모두 0 이상이므로, 싼 항까지의 합이
// limit 이상이 되면 그 자리에서 멈추고 limit 이상인 점수를 돌려준다.
func (e *rowCoder) tryBlock4(q *quantizer, lam *rdLambdas, pred *[16]uint8, m, sx, sy, bx, by int,
	above, left uint8, ctx int, limit int64) block4Result {
	c := block4Result{mode: uint8(m)}
	c.score.h = bmodeCost[above][left][m]
	if c.score.setScore(lam.i4); c.score.score >= limit {
		return c
	}
	c.score.d = quantBlock(&q.y1, e.src.y, e.src.ys, sx, sy, pred[:], 4, &c.levels, c.rec[:], 4)
	if c.score.setScore(lam.i4); c.score.score >= limit {
		return c
	}
	c.score.sd = regionTDisto(c.rec[:], 4, lam.t, e.srcT[by*4+bx:])
	if m > 0 && isFlat([][16]int16{c.levels}, flatnessLimitI4) {
		c.score.r = flatnessPenalty
	}
	if c.score.setScore(lam.i4); c.score.score >= limit {
		return c
	}
	r, _ := e.costs.residual(&c.levels, planeY1SansY2, ctx, 0)
	c.score.r += r
	c.score.setScore(lam.i4)
	return c
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
		c := mbInfo{mbHeader: mbHeader{uvMode: m}}
		var rec [2][uvSize * uvSize]uint8
		var s rdScore
		for ch, pl := range [2][]uint8{e.src.u, e.src.v} {
			s.d += e.quantChroma(&c, q, pl, predict(m, &es[ch], uvSize, &buf), uBlockBase+ch*4, cx, cy, rec[ch][:])
		}
		s.h = uvModeCost[m]
		if m > 0 && isFlat(c.levels[uBlockBase:uBlockBase+nUVBlocks], flatnessLimitUV) {
			s.r = flatnessPenalty * nUVBlocks
		}
		if s.setScore(lam.uv); m > 0 && s.score >= best.score {
			continue
		}
		s.r += e.chromaRate(&c, mbx)
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
	var sse int64
	for b := range 4 {
		bx, by := (b%2)*4, (b/2)*4
		off := by*uvSize + bx
		sse += quantBlock(&q.uv, src, e.src.cs, cx+bx, cy+by, pred[off:], uvSize, &mb.levels[base+b], rec[off:], uvSize)
	}
	return sse
}

func (e *rowCoder) chromaRate(mb *mbInfo, mbx int) int64 {
	var r int64
	top, left := e.nzTop[mbx], e.nzLeft
	writeChromaTokens(&tokenWriter{rate: &r, costs: e.costs}, mb, &top, &left)
	return r
}

// quantBlockGo는 4×4 블록 하나를 변환·양자화(DC 포함)해 levels에, 복원을 rec(stride rs)에 쓰고
// 원본과 복원의 SSE를 돌려준다. 예측 pred는 stride ps로 읽는다.
// SIMD 커널(quantBlock)과 결과가 비트 단위로 같아야 한다.
func quantBlockGo(m *quantMatrix, src []uint8, s, x, y int, pred []uint8, ps int, levels *[16]int16, rec []uint8, rs int) int64 {
	p := sub4(pred, ps, 0, 0)
	res := residual4(src, s, x, y, &p)
	coeffs := fdct4(&res)
	deq := m.quantize(&coeffs, levels, 0)
	px := idct4Add(&deq, &p)
	copyBlock(rec, rs, 0, 0, px[:], 4)
	return regionSSE(src, s, x, y, px[:], 4)
}

// quantBlockDCGo는 i16 휘도 블록의 AC만 양자화하고, DC는 Y2에서 복원한 dc로 바꿔 rec에 복원한다.
func quantBlockDCGo(m *quantMatrix, coeffs *[16]int32, dc int32, levels *[16]int16, pred []uint8, ps int, rec []uint8, rs int) {
	p := sub4(pred, ps, 0, 0)
	deq := m.quantize(coeffs, levels, 1)
	levels[0] = 0
	deq[0] = dc
	px := idct4Add(&deq, &p)
	copyBlock(rec, rs, 0, 0, px[:], 4)
}

// fdctBlockGo는 원본 (x,y)의 4×4와 예측(stride ps)의 잔차를 순 DCT한다.
func fdctBlockGo(src []uint8, s, x, y int, pred []uint8, ps int) [16]int32 {
	p := sub4(pred, ps, 0, 0)
	res := residual4(src, s, x, y, &p)
	return fdct4(&res)
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

func nonZero(levels *[16]int16) bool {
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
