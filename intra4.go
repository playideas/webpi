package webpi

// 4×4 휘도 예측 모드 — 순서는 bmodeProb와 같다. 앞의 넷은 16×16 모드와 값이 같아
// i16 매크로블록이 이웃의 모드 문맥이 될 때 그대로 쓴다 (RFC 6386 11.3).
const (
	bDC = iota
	bTM
	bVE
	bHE
	bRD
	bVR
	bLD
	bVL
	bHD
	bHU
	nBModes
)

// b4Edges는 4×4 블록 예측의 이웃이다. top[0]은 왼쪽 위 모서리, top[1:9]는 위 4개 + 오른쪽 위 4개.
type b4Edges struct {
	top  [9]int32
	left [4]int32
}

func avg2(a, b int32) uint8    { return uint8((a + b + 1) >> 1) }
func avg3(a, b, c int32) uint8 { return uint8((a + 2*b + c + 2) >> 2) }

// pred4는 모드별 예측기를 부른다. 공식은 RFC 6386 12.3 (x/image/vp8 predfunc.go와 같은 계산).
// 함수 표 대신 switch를 쓰는 것은 간접 호출이 결과 배열을 힙으로 밀어내기 때문이다.
func pred4(mode int, e *b4Edges) [16]uint8 {
	var p [16]uint8
	switch mode {
	case bDC:
		pred4DC(e, &p)
	case bTM:
		pred4TM(e, &p)
	case bVE:
		pred4VE(e, &p)
	case bHE:
		pred4HE(e, &p)
	case bRD:
		pred4RD(e, &p)
	case bVR:
		pred4VR(e, &p)
	case bLD:
		pred4LD(e, &p)
	case bVL:
		pred4VL(e, &p)
	case bHD:
		pred4HD(e, &p)
	default:
		pred4HU(e, &p)
	}
	return p
}

// set4는 (x,y)들의 목록에 같은 값을 쓴다 — 대각 모드는 같은 값이 대각선을 따라 반복된다.
func set4(p *[16]uint8, v uint8, xy ...int) {
	for i := 0; i < len(xy); i += 2 {
		p[xy[i+1]*4+xy[i]] = v
	}
}

func pred4DC(e *b4Edges, p *[16]uint8) {
	sum := int32(4)
	for i := range 4 {
		sum += e.top[1+i] + e.left[i]
	}
	for i := range p {
		p[i] = uint8(sum >> 3)
	}
}

func pred4TM(e *b4Edges, p *[16]uint8) {
	for y := range 4 {
		for x := range 4 {
			p[y*4+x] = clip8(e.left[y] + e.top[1+x] - e.top[0])
		}
	}
}

func pred4VE(e *b4Edges, p *[16]uint8) {
	t := &e.top
	for x := range 4 {
		v := avg3(t[x], t[x+1], t[x+2])
		for y := range 4 {
			p[y*4+x] = v
		}
	}
}

func pred4HE(e *b4Edges, p *[16]uint8) {
	X, I, J, K, L := e.top[0], e.left[0], e.left[1], e.left[2], e.left[3]
	rows := [4]uint8{avg3(X, I, J), avg3(I, J, K), avg3(J, K, L), avg3(K, L, L)}
	for y := range 4 {
		for x := range 4 {
			p[y*4+x] = rows[y]
		}
	}
}

func pred4RD(e *b4Edges, p *[16]uint8) {
	X, A, B, C, D := e.top[0], e.top[1], e.top[2], e.top[3], e.top[4]
	I, J, K, L := e.left[0], e.left[1], e.left[2], e.left[3]
	set4(p, avg3(J, K, L), 0, 3)
	set4(p, avg3(I, J, K), 1, 3, 0, 2)
	set4(p, avg3(X, I, J), 2, 3, 1, 2, 0, 1)
	set4(p, avg3(A, X, I), 3, 3, 2, 2, 1, 1, 0, 0)
	set4(p, avg3(B, A, X), 3, 2, 2, 1, 1, 0)
	set4(p, avg3(C, B, A), 3, 1, 2, 0)
	set4(p, avg3(D, C, B), 3, 0)
}

func pred4VR(e *b4Edges, p *[16]uint8) {
	X, A, B, C, D := e.top[0], e.top[1], e.top[2], e.top[3], e.top[4]
	I, J, K := e.left[0], e.left[1], e.left[2]
	set4(p, avg2(X, A), 0, 0, 1, 2)
	set4(p, avg2(A, B), 1, 0, 2, 2)
	set4(p, avg2(B, C), 2, 0, 3, 2)
	set4(p, avg2(C, D), 3, 0)
	set4(p, avg3(K, J, I), 0, 3)
	set4(p, avg3(J, I, X), 0, 2)
	set4(p, avg3(I, X, A), 0, 1, 1, 3)
	set4(p, avg3(X, A, B), 1, 1, 2, 3)
	set4(p, avg3(A, B, C), 2, 1, 3, 3)
	set4(p, avg3(B, C, D), 3, 1)
}

func pred4LD(e *b4Edges, p *[16]uint8) {
	A, B, C, D := e.top[1], e.top[2], e.top[3], e.top[4]
	E, F, G, H := e.top[5], e.top[6], e.top[7], e.top[8]
	set4(p, avg3(A, B, C), 0, 0)
	set4(p, avg3(B, C, D), 1, 0, 0, 1)
	set4(p, avg3(C, D, E), 2, 0, 1, 1, 0, 2)
	set4(p, avg3(D, E, F), 3, 0, 2, 1, 1, 2, 0, 3)
	set4(p, avg3(E, F, G), 3, 1, 2, 2, 1, 3)
	set4(p, avg3(F, G, H), 3, 2, 2, 3)
	set4(p, avg3(G, H, H), 3, 3)
}

func pred4VL(e *b4Edges, p *[16]uint8) {
	A, B, C, D := e.top[1], e.top[2], e.top[3], e.top[4]
	E, F, G, H := e.top[5], e.top[6], e.top[7], e.top[8]
	set4(p, avg2(A, B), 0, 0)
	set4(p, avg2(B, C), 1, 0, 0, 2)
	set4(p, avg2(C, D), 2, 0, 1, 2)
	set4(p, avg2(D, E), 3, 0, 2, 2)
	set4(p, avg3(A, B, C), 0, 1)
	set4(p, avg3(B, C, D), 1, 1, 0, 3)
	set4(p, avg3(C, D, E), 2, 1, 1, 3)
	set4(p, avg3(D, E, F), 3, 1, 2, 3)
	set4(p, avg3(E, F, G), 3, 2)
	set4(p, avg3(F, G, H), 3, 3)
}

func pred4HD(e *b4Edges, p *[16]uint8) {
	X, A, B, C := e.top[0], e.top[1], e.top[2], e.top[3]
	I, J, K, L := e.left[0], e.left[1], e.left[2], e.left[3]
	set4(p, avg2(I, X), 0, 0, 2, 1)
	set4(p, avg2(J, I), 0, 1, 2, 2)
	set4(p, avg2(K, J), 0, 2, 2, 3)
	set4(p, avg2(L, K), 0, 3)
	set4(p, avg3(A, B, C), 3, 0)
	set4(p, avg3(X, A, B), 2, 0)
	set4(p, avg3(I, X, A), 1, 0, 3, 1)
	set4(p, avg3(J, I, X), 1, 1, 3, 2)
	set4(p, avg3(K, J, I), 1, 2, 3, 3)
	set4(p, avg3(L, K, J), 1, 3)
}

func pred4HU(e *b4Edges, p *[16]uint8) {
	I, J, K, L := e.left[0], e.left[1], e.left[2], e.left[3]
	set4(p, avg2(I, J), 0, 0)
	set4(p, avg2(J, K), 2, 0, 0, 1)
	set4(p, avg2(K, L), 2, 1, 0, 2)
	set4(p, avg3(I, J, K), 1, 0)
	set4(p, avg3(J, K, L), 3, 0, 1, 1)
	set4(p, avg3(K, L, L), 3, 1, 1, 2)
	set4(p, uint8(L), 3, 2, 2, 2, 0, 3, 1, 3, 2, 3, 3, 3)
}

// lumaWorkspace는 i4 탐색 중 매크로블록의 복원 픽셀과 이웃을 담는다.
// 행 0은 위 이웃(모서리, 16개, 오른쪽 위 4개), 열 0은 왼쪽 이웃이다.
type lumaWorkspace [mbSize + 1][mbSize + 1 + 4]uint8

const wsTopRight = mbSize + 1

// load는 복원 평면에서 (mbx,mby) 매크로블록의 이웃을 채운다.
// 오른쪽 위 4픽셀은 블록 열 3의 아래 행들에도 복사한다 — 디코더도 MB 위쪽의 값을 그대로 쓴다.
func (ws *lumaWorkspace) load(rec *yuvPlanes, mbx, mby int) {
	x0, y0 := mbx*mbSize, mby*mbSize
	e := loadEdges(rec.y, rec.ys, x0, y0, mbSize)
	ws[0][0] = e.corner
	for i := range mbSize {
		ws[0][1+i], ws[1+i][0] = e.top[i], e.left[i]
	}
	for i := range 4 {
		var tr uint8 = edgeAbove
		switch {
		case mby == 0:
		case mbx == rec.mbw-1:
			tr = rec.y[(y0-1)*rec.ys+x0+mbSize-1]
		default:
			tr = rec.y[(y0-1)*rec.ys+x0+mbSize+i]
		}
		for by := 0; by < mbSize; by += 4 {
			ws[by][wsTopRight+i] = tr
		}
	}
}

// edges4는 블록 (bx,by)(4픽셀 단위)의 예측 이웃을 작업 공간에서 읽는다.
func (ws *lumaWorkspace) edges4(bx, by int) b4Edges {
	var e b4Edges
	r, c := by*4, bx*4
	for i := range 9 {
		e.top[i] = int32(ws[r][c+i])
	}
	for j := range 4 {
		e.left[j] = int32(ws[r+1+j][c])
	}
	return e
}

func (ws *lumaWorkspace) store4(bx, by int, px *[16]uint8) {
	for j := range 4 {
		copy(ws[by*4+1+j][bx*4+1:bx*4+5], px[j*4:j*4+4])
	}
}
