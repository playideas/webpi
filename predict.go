package webpi

// 16×16 휘도·8×8 색차 인트라 예측 모드 — RFC 6386 12.2
const (
	predDC = iota
	predTM
	predVE
	predHE
	nPredModes
)

// 프레임 바깥 경계값 — 위쪽은 127, 왼쪽은 129 (RFC 6386 12.2)
const (
	edgeAbove = 0x7f
	edgeLeft  = 0x81
	dcNoEdge  = 0x80
)

// edges는 한 블록의 예측에 쓰는 이웃 픽셀이다. 필터 전 복원값에서 가져온다.
type edges struct {
	top, left       [16]uint8
	corner          uint8
	hasTop, hasLeft bool
}

// loadEdges는 평면 pl(stride s)에서 (x0,y0)에 놓인 n×n 블록의 이웃을 읽는다.
func loadEdges(pl []uint8, s, x0, y0, n int) edges {
	e := edges{hasTop: y0 > 0, hasLeft: x0 > 0}
	for i := range n {
		e.top[i], e.left[i] = edgeAbove, edgeLeft
		if e.hasTop {
			e.top[i] = pl[(y0-1)*s+x0+i]
		}
		if e.hasLeft {
			e.left[i] = pl[(y0+i)*s+x0-1]
		}
	}
	switch {
	case !e.hasTop:
		e.corner = edgeAbove
	case !e.hasLeft:
		e.corner = edgeLeft
	default:
		e.corner = pl[(y0-1)*s+x0-1]
	}
	return e
}

// predBuf는 가장 큰(16×16) 예측 블록을 담는 호출자 쪽 버퍼다 — 힙 할당을 피한다.
type predBuf [mbSize * mbSize]uint8

// predict는 모드 mode로 n×n 예측 블록(행 우선)을 buf에 만들어 그 앞부분을 돌려준다.
// 모드 분기는 블록마다 한 번만 하고 행 단위로 채운다.
func predict(mode int, e *edges, n int, buf *predBuf) []uint8 {
	out := buf[:n*n]
	switch mode {
	case predDC:
		dc := dcValue(e, n)
		for i := range out {
			out[i] = dc
		}
	case predTM:
		for j := range n {
			d := int32(e.left[j]) - int32(e.corner)
			row := out[j*n : j*n+n]
			for i := range row {
				row[i] = clip8(int32(e.top[i]) + d)
			}
		}
	case predVE:
		for j := range n {
			copy(out[j*n:j*n+n], e.top[:n])
		}
	case predHE:
		for j := range n {
			row := out[j*n : j*n+n]
			for i := range row {
				row[i] = e.left[j]
			}
		}
	}
	return out
}

// dcValue는 경계 유무에 따른 DC 예측값이다 (위·왼쪽 모두 없으면 128).
func dcValue(e *edges, n int) uint8 {
	var sum, cnt int
	if e.hasTop {
		for i := range n {
			sum += int(e.top[i])
		}
		cnt += n
	}
	if e.hasLeft {
		for i := range n {
			sum += int(e.left[i])
		}
		cnt += n
	}
	if cnt == 0 {
		return dcNoEdge
	}
	return uint8((sum + cnt/2) / cnt)
}
