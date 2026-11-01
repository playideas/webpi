package webpi

// 변환 상수 — RFC 6386 14.3 (역 DCT), 순 DCT는 참조 인코더의 정수 근사
const (
	idctC1 = 85627 // 65536 × cos(π/8) × √2
	idctC2 = 35468 // 65536 × sin(π/8) × √2
	fdctK1 = 2217
	fdctK2 = 5352
	// 순 DCT 반올림 상수 — 행 단계 출력 1·3, 열 단계 출력 0(·2)·4·12
	fdctRound1  = 1812
	fdctRound3  = 937
	fdctRound0  = 7
	fdctRound4  = 12000
	fdctRound12 = 51000
)

// fdct4는 4×4 잔차(src-pred)를 DCT 계수로 바꾼다. 결과는 행 우선 자연 순서.
func fdct4(res *[16]int32) [16]int32 {
	var tmp, out [16]int32
	for i := range 4 {
		d0, d1, d2, d3 := res[i*4], res[i*4+1], res[i*4+2], res[i*4+3]
		a0, a1, a2, a3 := d0+d3, d1+d2, d1-d2, d0-d3
		tmp[i*4+0] = (a0 + a1) * 8
		tmp[i*4+1] = (a2*fdctK1 + a3*fdctK2 + fdctRound1) >> 9
		tmp[i*4+2] = (a0 - a1) * 8
		tmp[i*4+3] = (a3*fdctK1 - a2*fdctK2 + fdctRound3) >> 9
	}
	for i := range 4 {
		a0, a1 := tmp[i]+tmp[12+i], tmp[4+i]+tmp[8+i]
		a2, a3 := tmp[4+i]-tmp[8+i], tmp[i]-tmp[12+i]
		out[i] = (a0 + a1 + fdctRound0) >> 4
		out[4+i] = (a2*fdctK1+a3*fdctK2+fdctRound4)>>16 + b2i(a3 != 0)
		out[8+i] = (a0 - a1 + fdctRound0) >> 4
		out[12+i] = (a3*fdctK1 - a2*fdctK2 + fdctRound12) >> 16
	}
	return out
}

// idct4Add는 디코더와 같은 정수 역 DCT로 잔차를 복원해 pred에 더한다 (0~255로 자른다).
// 낮은 화질에서는 DC만 남은 블록이 대부분이라 그 경우를 따로 푼다 — 역 DCT 결과가 (DC+4)>>3으로 균일하다.
func idct4Add(c *[16]int32, pred *[16]uint8) [16]uint8 {
	if acZero(c) {
		var out [16]uint8
		r := (c[0] + idctRound) >> idctShift
		for i, p := range pred {
			out[i] = clip8(int32(p) + r)
		}
		return out
	}
	var m [4][4]int32
	for i := range 4 {
		a := c[i] + c[8+i]
		b := c[i] - c[8+i]
		cc := (c[4+i]*idctC2)>>16 - (c[12+i]*idctC1)>>16
		d := (c[4+i]*idctC1)>>16 + (c[12+i]*idctC2)>>16
		m[i] = [4]int32{a + d, b + cc, b - cc, a - d}
	}
	var out [16]uint8
	for j := range 4 {
		dc := m[0][j] + 4
		a, b := dc+m[2][j], dc-m[2][j]
		cc := (m[1][j]*idctC2)>>16 - (m[3][j]*idctC1)>>16
		d := (m[1][j]*idctC1)>>16 + (m[3][j]*idctC2)>>16
		r := [4]int32{(a + d) >> 3, (b + cc) >> 3, (b - cc) >> 3, (a - d) >> 3}
		for i := range 4 {
			out[j*4+i] = clip8(int32(pred[j*4+i]) + r[i])
		}
	}
	return out
}

const (
	idctRound = 4
	idctShift = 3
)

func acZero(c *[16]int32) bool {
	for _, v := range c[1:] {
		if v != 0 {
			return false
		}
	}
	return true
}

// fwht는 16개 블록의 DC(블록 래스터 순서)를 Y2 계수로 바꾼다.
// 4점 아다마르 H는 대칭이고 H·H = 4I라, 역변환 (H·Y·H + 3) >> 3 과 짝이 되도록 >> 1 한다.
func fwht(dc *[16]int32) [16]int32 {
	var m, out [16]int32
	for i := range 4 {
		a0, a1 := dc[i]+dc[12+i], dc[4+i]+dc[8+i]
		a2, a3 := dc[4+i]-dc[8+i], dc[i]-dc[12+i]
		m[i], m[8+i], m[4+i], m[12+i] = a0+a1, a0-a1, a3+a2, a3-a2
	}
	for i := range 4 {
		a0, a1 := m[i*4]+m[i*4+3], m[i*4+1]+m[i*4+2]
		a2, a3 := m[i*4+1]-m[i*4+2], m[i*4]-m[i*4+3]
		out[i*4+0] = (a0 + a1) >> 1
		out[i*4+1] = (a3 + a2) >> 1
		out[i*4+2] = (a0 - a1) >> 1
		out[i*4+3] = (a3 - a2) >> 1
	}
	return out
}

// iwht는 디코더와 같은 역 WHT로 Y2 계수에서 16개 블록의 DC를 복원한다.
func iwht(c *[16]int32) [16]int32 {
	var m, out [16]int32
	for i := range 4 {
		a0, a1 := c[i]+c[12+i], c[4+i]+c[8+i]
		a2, a3 := c[4+i]-c[8+i], c[i]-c[12+i]
		m[i], m[8+i], m[4+i], m[12+i] = a0+a1, a0-a1, a3+a2, a3-a2
	}
	for i := range 4 {
		dc := m[i*4] + 3
		a0, a1 := dc+m[i*4+3], m[i*4+1]+m[i*4+2]
		a2, a3 := m[i*4+1]-m[i*4+2], dc-m[i*4+3]
		out[i*4+0] = (a0 + a1) >> 3
		out[i*4+1] = (a3 + a2) >> 3
		out[i*4+2] = (a0 - a1) >> 3
		out[i*4+3] = (a3 - a2) >> 3
	}
	return out
}

func clip8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
