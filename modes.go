package webpi

// 키 프레임 모드 트리 — RFC 6386 11.2. 한 경로를 (확률, 비트) 목록으로 적어 쓰기와 비용 계산에 같이 쓴다.
type probStep struct {
	prob uint8
	bit  bool
}

// 휘도 모드 첫 비트: 1이면 16×16, 0이면 4×4(B_PRED)
const probIsI16 = 145

// i16Path·uvPath는 predDC, predTM, predVE, predHE 순서다.
var i16Path = [nPredModes][]probStep{
	{{probIsI16, true}, {156, false}, {163, false}},
	{{probIsI16, true}, {156, true}, {128, true}},
	{{probIsI16, true}, {156, false}, {163, true}},
	{{probIsI16, true}, {156, true}, {128, false}},
}

var uvPath = [nPredModes][]probStep{
	{{142, false}},
	{{142, true}, {114, true}, {183, true}},
	{{142, true}, {114, false}},
	{{142, true}, {114, true}, {183, false}},
}

// bmodeStep은 4×4 모드 트리의 한 단계다. 확률은 문맥별 bmodeProb의 node번째 값이다.
type bmodeStep struct {
	node uint8
	bit  bool
}

var bmodePath = [nBModes][]bmodeStep{
	bDC: {{0, false}},
	bTM: {{0, true}, {1, false}},
	bVE: {{0, true}, {1, true}, {2, false}},
	bHE: {{0, true}, {1, true}, {2, true}, {3, false}, {4, false}},
	bRD: {{0, true}, {1, true}, {2, true}, {3, false}, {4, true}, {5, false}},
	bVR: {{0, true}, {1, true}, {2, true}, {3, false}, {4, true}, {5, true}},
	bLD: {{0, true}, {1, true}, {2, true}, {3, true}, {6, false}},
	bVL: {{0, true}, {1, true}, {2, true}, {3, true}, {6, true}, {7, false}},
	bHD: {{0, true}, {1, true}, {2, true}, {3, true}, {6, true}, {7, true}, {8, false}},
	bHU: {{0, true}, {1, true}, {2, true}, {3, true}, {6, true}, {7, true}, {8, true}},
}

func pathCost(path []probStep) int64 {
	var c int64
	for _, s := range path {
		c += bitCost256(s.prob, s.bit)
	}
	return c
}

// 모드 비용 표(1/256비트)
var (
	i16ModeCost  [nPredModes]int64
	uvModeCost   [nPredModes]int64
	bmodeCost    [nBModes][nBModes][nBModes]int64 // [위][왼쪽][모드]
	i4HeaderCost = bitCost256(probIsI16, false)
)

func init() {
	for m := range nPredModes {
		i16ModeCost[m], uvModeCost[m] = pathCost(i16Path[m]), pathCost(uvPath[m])
	}
	for a := range nBModes {
		for l := range nBModes {
			for m := range nBModes {
				for _, s := range bmodePath[m] {
					bmodeCost[a][l][m] += bitCost256(bmodeProb[a][l][s.node], s.bit)
				}
			}
		}
	}
}

// modeContext는 4×4 모드 문맥이다: 매크로블록 열마다 맨 아래 행 모드, 현재 행의 왼쪽 열 모드.
// 프레임 바깥은 B_DC_PRED로 본다.
type modeContext struct {
	top  []uint8
	left [4]uint8
}

func newModeContext(mbw int) *modeContext {
	return &modeContext{top: make([]uint8, mbw*4)}
}

// update는 매크로블록의 4×4 모드(i16이면 그 모드로 채운 값)로 문맥을 넘긴다.
func (c *modeContext) update(mb *mbHeader, mbx int) {
	for i := range 4 {
		c.top[mbx*4+i], c.left[i] = mb.bModes[12+i], mb.bModes[i*4+3]
	}
}

// writeModes는 매크로블록별 세그먼트 id, skip 플래그, 예측 모드를 쓴다.
func (e *encoder) writeModes(h *boolEncoder) {
	ctx := newModeContext(e.src.mbw)
	for i := range e.mbs {
		mb, mbx := &e.mbs[i], i%e.src.mbw
		if mbx == 0 {
			ctx.left = [4]uint8{}
		}
		e.writeSegmentID(h, e.seg.ids[i])
		if e.useSkip {
			h.putBit(e.skipProb, mb.skip)
		}
		if mb.i4 {
			h.putBit(probIsI16, false)
			writeBModes(h, mb, ctx, mbx)
		} else {
			putPath(h, i16Path[mb.yMode])
		}
		putPath(h, uvPath[mb.uvMode])
		ctx.update(mb, mbx)
	}
}

func writeBModes(h *boolEncoder, mb *mbHeader, ctx *modeContext, mbx int) {
	for b := range nYBlocks {
		above, left := ctx.neighbors(mb, mbx, b)
		for _, s := range bmodePath[mb.bModes[b]] {
			h.putBit(bmodeProb[above][left][s.node], s.bit)
		}
	}
}

// neighbors는 매크로블록 안 블록 b의 위·왼쪽 4×4 모드다 (mb.bModes의 앞선 블록은 이미 정해져 있어야 한다).
func (c *modeContext) neighbors(mb *mbHeader, mbx, b int) (above, left uint8) {
	bx, by := b%4, b/4
	above, left = c.top[mbx*4+bx], c.left[by]
	if by > 0 {
		above = mb.bModes[b-4]
	}
	if bx > 0 {
		left = mb.bModes[b-1]
	}
	return above, left
}

func putPath(h *boolEncoder, path []probStep) {
	for _, s := range path {
		h.putBit(s.prob, s.bit)
	}
}
