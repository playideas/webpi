package webpi

import "math"

// VP8L LZ77 — 탐욕 분할로 기호 비용을 추정한 뒤, 그 비용으로 총비트가 가장 적은 토큰 열을 고른다.
const (
	// 최적 분할을 비용 추정 → 분할로 반복하는 횟수, 후보로 보는 2차원 거리 수(가까운 순).
	// DIS5K 마스크(이진·흐림)에서 거리 120개·2회와 같은 크기를 3배 넘게 빨리 낸다 — 반복이 거리 수보다 효과가 크다
	lzOptimalPasses   = 3
	lzPlaneCandidates = 16
	// 한 번도 나오지 않은 기호의 추정 빈도 (0이면 비용이 무한대가 된다)
	unseenSymbolCount = 0.5
	// 최적 분할은 픽셀마다 12바이트 표를 쓰므로 이보다 큰 평면은 탐욕 분할만 한다 (16383² 평면이면 표만 3GB)
	lzOptimalMaxPixels = 1 << 24
	// 첫 최적 분할에서 모은 일치 후보를 다음 회차에 다시 쓴다. 후보가 픽셀 수의 이 배수를 넘으면
	// (후보 하나 12바이트) 모으기를 그만두고 회차마다 다시 찾는다
	lzMaxCandidatesPerPixel = 1
	// 탐욕 분할의 일치가 픽셀의 1/lzMinCoverageDivisor보다 적게 덮으면 최적 분할을 하지 않는다 — 고칠 일치가
	// 거의 없다. 실제 마스크(DIS5K)는 99.9% 이상, 잡음 알파는 8%를 덮는다
	lzMinCoverageDivisor = 4
)

// matchFinder는 두 픽셀 해시 사슬과 2차원 거리 부호 표를 들고 일치를 찾는다.
type matchFinder struct {
	argb   []uint32
	head   []int32
	prev   []int32
	dcodes []uint8
	planes []int
}

func newMatchFinder(argb []uint32, w int) *matchFinder {
	f := &matchFinder{argb: argb, head: make([]int32, 1<<lzHashBits), prev: make([]int32, len(argb))}
	for i := range f.head {
		f.head[i] = -1
	}
	f.dcodes = distanceCodes(w)
	f.planes = planeDistances(f.dcodes)
	f.planes = f.planes[:min(len(f.planes), lzPlaneCandidates)]
	return f
}

// reset은 해시 사슬을 비워 같은 이미지를 처음부터 다시 훑을 수 있게 한다.
func (f *matchFinder) reset() {
	for i := range f.head {
		f.head[i] = -1
	}
}

func (f *matchFinder) insert(i int) {
	if i+1 < len(f.argb) {
		hv := lzHash(f.argb[i], f.argb[i+1])
		f.prev[i], f.head[hv] = f.head[hv], int32(i)
	}
}

// distCode는 거리 d의 부호 값이다 — 2차원 부호가 있으면 1~120, 없으면 d+120.
func (f *matchFinder) distCode(d int) uint32 {
	if d < len(f.dcodes) && f.dcodes[d] != 0 {
		return uint32(f.dcodes[d])
	}
	return uint32(d + numPlaneCodes)
}

// chainMatch는 해시 사슬에서 가장 긴 일치를 찾는다.
func (f *matchFinder) chainMatch(i int) (int, int) {
	bestLen, bestDist := 0, 0
	if i+1 >= len(f.argb) {
		return 0, 0
	}
	c := f.head[lzHash(f.argb[i], f.argb[i+1])]
	for k := 0; c >= 0 && k < lzMaxChain && bestLen < lzMaxLength; k++ {
		// 지금 최장 길이 바로 다음 픽셀부터 다르면 더 길 수 없으므로 끝까지 비교하지 않는다
		if d := i - int(c); d <= lzMaxDistance && (i+bestLen >= len(f.argb) || f.argb[i+bestLen] == f.argb[int(c)+bestLen]) {
			if l := matchLength(f.argb, i, d); l > bestLen {
				bestLen, bestDist = l, d
			}
		}
		c = f.prev[c]
	}
	return bestLen, bestDist
}

// planeDistances는 2차원 거리 부호가 있는 거리를 부호 순서(가까운 순)로 늘어놓는다.
func planeDistances(dcodes []uint8) []int {
	var byCode [numPlaneCodes + 1]int
	for d, c := range dcodes {
		if c != 0 {
			byCode[c] = d
		}
	}
	var out []int
	for _, d := range byCode[1:] {
		if d != 0 {
			out = append(out, d)
		}
	}
	return out
}

func lzHash(a, b uint32) uint32 {
	const mul = 0x9e3779b1
	return ((a*mul ^ b) * mul) >> (32 - lzHashBits)
}

func matchLength(argb []uint32, i, d int) int {
	limit := min(len(argb)-i, lzMaxLength)
	l := 0
	for l < limit && argb[i+l] == argb[i+l-d] {
		l++
	}
	return l
}

// lz77은 픽셀을 토큰으로 나눈다: 탐욕 분할 한 번 뒤 비용 기반 최적 분할을 반복한다.
// 일치 후보는 비용과 무관하게 정해지므로 첫 회차에서 모아 두고 다음 회차는 비용만 바꿔 다시 고른다 —
// 결과는 회차마다 다시 찾는 것과 같고, 해시 사슬을 훑는 시간(잡음 알파에서 대부분)이 한 번으로 준다.
// 판단 PD-20261101-860bf3be: 압축이 안 되는 알파의 최악 속도를 줄인다 (실제 마스크 출력은 그대로)
func lz77(argb []uint32, w int) []lzToken {
	return lz77WithCacheLimit(argb, w, lzMaxCandidatesPerPixel*len(argb))
}

// lz77WithCacheLimit은 후보를 limit개까지 모아 다시 쓰는 lz77이다 (0이면 회차마다 다시 찾는다).
func lz77WithCacheLimit(argb []uint32, w, limit int) []lzToken {
	f := newMatchFinder(argb, w)
	toks := lzGreedy(f)
	cache := candidateCache{limit: limit}
	for range optimalPasses(len(argb), matchedPixels(toks)) {
		costs := newSymbolCosts(tokenHistograms(toks))
		toks = lzOptimal(argb, &costs, cache.source(f))
	}
	return toks
}

// lzCandidate는 위치 pos에서 끝까지 이어지는 역참조 후보다.
type lzCandidate struct {
	pos    int32
	length uint16
	dcode  uint32
}

// candidateSource는 후보를 위치 순서로 yield에 넘긴다.
type candidateSource func(yield func(c lzCandidate))

// candidateCache는 첫 회차에 찾은 후보를 담는다. 한도를 넘으면 버리고 회차마다 다시 찾는다.
type candidateCache struct {
	cands        []lzCandidate
	filled, over bool
	limit        int
}

func (cc *candidateCache) source(f *matchFinder) candidateSource {
	if cc.filled && !cc.over {
		return func(yield func(c lzCandidate)) {
			for _, c := range cc.cands {
				yield(c)
			}
		}
	}
	record := !cc.filled
	cc.filled = true
	return func(yield func(c lzCandidate)) {
		f.reset()
		findCandidates(f, func(c lzCandidate) {
			if record && !cc.over {
				if cc.cands = append(cc.cands, c); len(cc.cands) > cc.limit {
					cc.cands, cc.over = nil, true
				}
			}
			yield(c)
		})
	}
}

// optimalPasses는 탐욕 분할의 일치가 matched픽셀을 덮은 n픽셀 평면에 돌릴 최적 분할 횟수다.
// 큰 평면은 표 메모리 때문에, 일치가 거의 없는 평면은 얻을 것이 없어 돌리지 않는다.
func optimalPasses(n, matched int) int {
	if n > lzOptimalMaxPixels || matched*lzMinCoverageDivisor < n {
		return 0
	}
	return lzOptimalPasses
}

// matchedPixels는 토큰 열에서 역참조가 덮는 픽셀 수다.
func matchedPixels(toks []lzToken) int {
	m := 0
	for _, t := range toks {
		m += int(t.length)
	}
	return m
}

// lzGreedy는 위치마다 가장 긴 일치(2차원 거리 → 해시 사슬 순, 같으면 먼저 찾은 것)를 고른다.
func lzGreedy(f *matchFinder) []lzToken {
	argb := f.argb
	toks := make([]lzToken, 0, len(argb)/2)
	for i := 0; i < len(argb); {
		length, dist := f.chainMatch(i)
		for _, d := range f.planes {
			if d <= i {
				if l := matchLength(argb, i, d); l > length {
					length, dist = l, d
				}
			}
		}
		if length < lzMinLength {
			toks = append(toks, lzToken{argb: argb[i]})
			f.insert(i)
			i++
			continue
		}
		toks = append(toks, lzToken{length: uint16(length), dist: f.distCode(dist)})
		for j := range length {
			f.insert(i + j)
		}
		i += length
	}
	return toks
}

// symbolCosts는 다섯 접두 부호의 기호별 추정 비트 수다.
type symbolCosts [numPrefixCodes][]float32

func newSymbolCosts(h [numPrefixCodes][]uint32) symbolCosts {
	var c symbolCosts
	for k, counts := range h {
		var total float64
		for _, v := range counts {
			total += float64(v)
		}
		c[k] = make([]float32, len(counts))
		for s, v := range counts {
			c[k][s] = float32(math.Log2(max(total, 1)) - math.Log2(max(float64(v), unseenSymbolCount)))
		}
	}
	return c
}

func (c *symbolCosts) literal(p uint32) float32 {
	a, r, g, b := argbBytes(p)
	return c[codeGreen][g] + c[codeRed][r] + c[codeBlue][b] + c[codeAlpha][a]
}

func (c *symbolCosts) match(length int, dcode uint32) float32 {
	ls, lnb, _ := prefixEncode(length)
	ds, dnb, _ := prefixEncode(int(dcode))
	return c[codeGreen][numLiteralCodes+ls] + float32(lnb) + c[codeDistance][ds] + float32(dnb)
}

// parseNode는 그 위치까지의 최소 비용과, 그 위치에 도달한 마지막 토큰이다 (거리 부호 0이면 리터럴).
// 끝점 갱신이 멀리 떨어진 메모리를 건드리므로 세 값을 한 구조체에 둬 캐시 미스를 한 번으로 줄인다.
type parseNode struct {
	cost   float32
	dist   uint32
	length uint16
}

// findCandidates는 위치마다 2차원 거리·해시 사슬 후보의 끝까지 일치를 위치 순서로 넘긴다.
// 이어지는 일치는 시작 위치에서 이미 같은 끝점으로 넘겼으므로, 거리마다 일치가 끝나 다시 재야 하는
// 위치만 기억한다. 평탄한 영역에서는 모든 거리가 길게 이어져 대부분의 위치에서 할 일이 없다.
func findCandidates(f *matchFinder, yield func(c lzCandidate)) {
	argb := f.argb
	n := len(argb)
	emit := func(i, l, d int) {
		if l >= lzMinLength {
			yield(lzCandidate{int32(i), uint16(l), f.distCode(d)})
		}
	}
	next := make([]int, len(f.planes))
	minNext, chainNext := 0, 0
	for i := range n {
		if i >= minNext {
			minNext = n
			for k, d := range f.planes {
				if next[k] <= i {
					next[k] = max(d, i+1)
					if d <= i {
						l := matchLength(argb, i, d)
						emit(i, l, d)
						next[k] = i + max(l, 1)
					}
				}
				minNext = min(minNext, next[k])
			}
		}
		if i >= chainNext {
			l, d := f.chainMatch(i)
			emit(i, l, d)
			chainNext = i + max(l, 1)
		}
		f.insert(i)
	}
}

// lzOptimal은 위치마다 리터럴과 후보 일치를 놓고 앞에서부터 최소 비용을 전파한 뒤
// 거꾸로 따라가 토큰 열을 만든다. 위치 i의 후보를 볼 때는 i보다 앞의 리터럴까지 모두 반영돼 있다.
func lzOptimal(argb []uint32, c *symbolCosts, cands candidateSource) []lzToken {
	n := len(argb)
	nodes := make([]parseNode, n+1)
	for i := range nodes[1:] {
		nodes[i+1].cost = math.MaxFloat32
	}
	relax := func(i, length int, dcode uint32, v float32) {
		if nd := &nodes[i+length]; v < nd.cost {
			*nd = parseNode{v, dcode, uint16(length)}
		}
	}
	done := 0 // 리터럴을 반영한 위치 수
	literalsUpTo := func(end int) {
		for ; done < end; done++ {
			relax(done, 1, 0, nodes[done].cost+c.literal(argb[done]))
		}
	}
	cands(func(cd lzCandidate) {
		i := int(cd.pos)
		literalsUpTo(i)
		relax(i, int(cd.length), cd.dcode, nodes[i].cost+c.match(int(cd.length), cd.dcode))
	})
	literalsUpTo(n)
	return traceTokens(argb, nodes)
}

// traceTokens는 끝에서부터 각 위치에 도달한 토큰을 따라가 앞에서부터의 토큰 열로 돌려준다.
func traceTokens(argb []uint32, nodes []parseNode) []lzToken {
	var rev []lzToken
	for i := len(argb); i > 0; {
		nd := nodes[i]
		if nd.dist == 0 {
			rev = append(rev, lzToken{argb: argb[i-1]})
		} else {
			rev = append(rev, lzToken{length: nd.length, dist: nd.dist})
		}
		i -= int(nd.length)
	}
	for a, b := 0, len(rev)-1; a < b; a, b = a+1, b-1 {
		rev[a], rev[b] = rev[b], rev[a]
	}
	return rev
}
