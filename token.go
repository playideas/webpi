package webpi

// 토큰 트리 고정 확률 — RFC 6386 13.2 (범주 1·2의 추가 비트)
const (
	cat1Prob  = 159
	cat2Prob0 = 165
	cat2Prob1 = 145
)

// 토큰 크기 경계 — 범주 n의 시작값은 3 + (8 << (n-3)), 범주 1·2는 5·7
const (
	dct2Max    = 4
	cat1Start  = 5
	cat2Start  = 7
	cat2Max    = 10
	catBigBase = 3
	catBigUnit = 8
	nBigCats   = 4
)

type probTable = [nPlane][nBand][nContext][nProb]uint8

// tokenStats는 확률 노드별 0/1 횟수 — 프레임 확률 갱신을 정할 때 쓴다.
type tokenStats = [nPlane][nBand][nContext][nProb][2]uint32

// tokenWriter는 계수 토큰을 쓴다. enc·stats·rate 중 nil이 아닌 것에만 반영한다
// (비트 쓰기, 노드별 횟수, 비용 누적). 모두 nil이면 문맥 계산만 한다.
type tokenWriter struct {
	enc   *boolEncoder
	probs *probTable
	stats *tokenStats
	rate  *int64
}

func (w *tokenWriter) node(plane, band, ctx, i int, bit bool) {
	if w.stats != nil {
		w.stats[plane][band][ctx][i][b2i(bit)]++
	}
	if w.enc != nil {
		w.enc.putBit(w.probs[plane][band][ctx][i], bit)
	}
	if w.rate != nil {
		*w.rate += bitCost256(w.probs[plane][band][ctx][i], bit)
	}
}

func (w *tokenWriter) fixed(prob uint8, bit bool) {
	if w.enc != nil {
		w.enc.putBit(prob, bit)
	}
	if w.rate != nil {
		*w.rate += bitCost256(prob, bit)
	}
}

// block은 한 4×4 블록의 레벨(자연 순서)을 first번째 지그재그 계수부터 쓰고,
// 0이 아닌 계수가 있었는지(다음 블록의 문맥)를 돌려준다.
func (w *tokenWriter) block(levels *[16]int32, plane, ctx, first int) int {
	last := -1
	for n := 15; n >= first; n-- {
		if levels[zigzag[n]] != 0 {
			last = n
			break
		}
	}
	band := int(bands[first])
	w.node(plane, band, ctx, 0, last >= 0)
	if last < 0 {
		return 0
	}
	for n := first; ; {
		v := levels[zigzag[n]]
		mag := v
		if mag < 0 {
			mag = -mag
		}
		w.node(plane, band, ctx, 1, mag != 0)
		n++
		if mag == 0 {
			// 0 토큰 다음에는 EOB가 올 수 없어 EOB 확인 없이 다음 계수로 간다
			band, ctx = int(bands[n]), 0
			continue
		}
		w.magnitude(plane, band, ctx, mag)
		w.fixed(uniformProb, v < 0)
		band, ctx = int(bands[n]), min(int(mag), 2)
		if n == 16 {
			return 1
		}
		w.node(plane, band, ctx, 0, n <= last)
		if n > last {
			return 1
		}
	}
}

// magnitude는 0이 아닌 계수 크기의 토큰(트리 노드 2 이후)과 추가 비트를 쓴다.
func (w *tokenWriter) magnitude(plane, band, ctx int, mag int32) {
	w.node(plane, band, ctx, 2, mag > 1)
	if mag == 1 {
		return
	}
	w.node(plane, band, ctx, 3, mag > dct2Max)
	if mag <= dct2Max {
		w.node(plane, band, ctx, 4, mag > 2)
		if mag > 2 {
			w.node(plane, band, ctx, 5, mag == dct2Max)
		}
		return
	}
	w.node(plane, band, ctx, 6, mag > cat2Max)
	if mag <= cat2Max {
		w.node(plane, band, ctx, 7, mag >= cat2Start)
		if mag < cat2Start {
			w.fixed(cat1Prob, mag == cat1Start+1)
		} else {
			w.fixed(cat2Prob0, (mag-cat2Start)>>1 == 1)
			w.fixed(cat2Prob1, (mag-cat2Start)&1 == 1)
		}
		return
	}
	w.bigCategory(plane, band, ctx, mag)
}

// bigCategory는 범주 3~6(크기 11 이상)을 쓴다.
func (w *tokenWriter) bigCategory(plane, band, ctx int, mag int32) {
	cat := nBigCats - 1
	for c := range nBigCats - 1 {
		if mag < catBigBase+catBigUnit<<(c+1) {
			cat = c
			break
		}
	}
	b1 := cat >> 1
	w.node(plane, band, ctx, 8, b1 == 1)
	w.node(plane, band, ctx, 9+b1, cat&1 == 1)
	rem := mag - (catBigBase + catBigUnit<<cat)
	tab := &cat3456[cat]
	nbits := 0
	for tab[nbits] != 0 {
		nbits++
	}
	for i := range nbits {
		w.fixed(tab[i], rem>>(nbits-1-i)&1 == 1)
	}
}
