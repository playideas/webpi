package webpi

// 계수 비용 표 — RD에서 블록마다 토큰 트리를 따라가지 않고 표를 더해 비용을 낸다 (libwebp cost_enc.c와 같은 구조).
// 확률에 따라 달라지는 토큰 경로는 범주 6이 시작하는 레벨까지만 다르고, 그 위는 고정 확률의 추가 비트만 늘어난다.
const (
	maxCostLevel = catBigBase + catBigUnit<<(nBigCats-1) // 범주 6 시작값 67
	signCost     = costOne                               // 부호 비트는 확률 1/2
)

// levelCosts는 확률표 probs에서 만든 비용 표다.
// lv[plane][band][ctx][v]는 그 문맥에서 크기 v인 계수의 비용(1/256비트)이다.
// ctx>0이면 앞 계수가 0이 아니었으므로 "EOB 아님" 비트 비용도 포함한다 — 0 다음에는 그 비트가 없다.
type levelCosts struct {
	probs probTable
	lv    [nPlane][nBand][nContext][maxCostLevel + 1]int32
}

// cat6Extra[v-67]은 범주 6 추가 비트의 비용이다.
var cat6Extra = func() (t [maxLevel - maxCostLevel + 1]int32) {
	var r int64
	w := tokenWriter{rate: &r}
	for i := range t {
		r = 0
		w.extraBits(nBigCats-1, int32(i))
		t[i] = int32(r)
	}
	return t
}()

func newLevelCosts(p *probTable) *levelCosts {
	c := &levelCosts{probs: *p}
	var r int64
	w := tokenWriter{probs: p, rate: &r}
	for plane := range nPlane {
		for band := range nBand {
			for ctx := range nContext {
				var eob int64
				if ctx > 0 {
					eob = bitCost256(p[plane][band][ctx][0], true)
				}
				for v := range maxCostLevel + 1 {
					r = eob
					w.node(plane, band, ctx, 1, v != 0)
					if v != 0 {
						w.magnitude(plane, band, ctx, int32(v))
						r += signCost
					}
					c.lv[plane][band][ctx][v] = int32(r)
				}
			}
		}
	}
	return c
}

// level은 크기 v(부호 없음) 계수의 비용이다.
func (c *levelCosts) level(plane, band, ctx int, v int32) int64 {
	if v <= maxCostLevel {
		return int64(c.lv[plane][band][ctx][v])
	}
	return int64(c.lv[plane][band][ctx][maxCostLevel] + cat6Extra[v-maxCostLevel] - cat6Extra[0])
}

// residual은 tokenWriter.block과 같은 토큰열의 비용과, 0이 아닌 계수 유무를 돌려준다.
func (c *levelCosts) residual(levels *[16]int32, plane, ctx0, first int) (int64, int) {
	p := &c.probs
	last := -1
	for n := 15; n >= first; n-- {
		if levels[zigzag[n]] != 0 {
			last = n
			break
		}
	}
	p0 := p[plane][bands[first]][ctx0][0]
	if last < 0 {
		return bitCost256(p0, false), 0
	}
	var cost int64
	if ctx0 == 0 {
		cost = bitCost256(p0, true)
	}
	ctx := ctx0
	for n := first; n <= last; n++ {
		v := abs32(levels[zigzag[n]])
		cost += c.level(plane, int(bands[n]), ctx, v)
		ctx = int(min(v, 2))
	}
	if last < 15 {
		cost += bitCost256(p[plane][bands[last+1]][ctx][0], false)
	}
	return cost, 1
}
