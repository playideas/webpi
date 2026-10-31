package webpi

import "math"

// RD 점수 = (R + H)·λ + 256·(D + SD). R·H는 1/256비트 단위, D는 픽셀 SSE, SD는 텍스처 왜곡.
// 상수와 λ 공식은 libwebp quant_enc.c(method 4, RD_OPT_BASIC)를 따른다.
const (
	rdDistoMult = 256
	costOne     = 256 // 1비트의 비용
	// 평탄한 블록을 복잡한 모드로 잘못 예측하지 않게 하는 벌점 (블록당 약 1비트)
	flatnessPenalty  = 140
	flatnessLimitI4  = 3
	flatnessLimitUV  = 2
	flatnessLimitI16 = 0
	// TDisto 가중합의 정규화 시프트와 SD에 곱하는 tλ의 고정소수점 시프트
	tdistoShift  = 5
	tlambdaShift = 5
	mult8BShift  = 8
	// 행렬 평균 단계 = (DC + 15·AC + 8) >> 4
	expnShift = 4
)

// λ 계수: λ_i4 = 3q²>>7, λ_i16 = 3q², λ_uv = 3q²>>6, λ_mode = q²>>7
const (
	lambdaScale     = 3
	lambdaI4Shift   = 7
	lambdaUVShift   = 6
	lambdaModeShift = 7
)

// bitCostTab[p]는 확률 p/256인 사건(비트 0)의 비용(1/256비트)이다.
var bitCostTab = func() (t [probScale + 1]int64) {
	for p := 1; p <= probScale; p++ {
		t[p] = int64(math.Round(-math.Log2(float64(p)/probScale) * costOne))
	}
	return t
}()

// bitCost256은 확률 p(0일 확률)에서 bit를 쓰는 비용이다.
func bitCost256(p uint8, bit bool) int64 {
	if bit {
		return bitCostTab[probScale-int(p)]
	}
	return bitCostTab[p]
}

// rdScore는 한 후보의 율-왜곡 구성 요소와 점수다.
type rdScore struct {
	d, sd, h, r int64
	score       int64
}

func (s *rdScore) setScore(lambda int64) {
	s.score = (s.r+s.h)*lambda + rdDistoMult*(s.d+s.sd)
}

func (s *rdScore) add(o *rdScore) {
	s.d, s.sd, s.h, s.r = s.d+o.d, s.sd+o.sd, s.h+o.h, s.r+o.r
	s.score += o.score
}

// rdLambdas는 세그먼트 양자화 단계에서 나온 λ들이다.
type rdLambdas struct {
	i4, i16, uv, mode, t int64
}

func newRDLambdas(q *quantizer) rdLambdas {
	qi4, qi16, quv := q.y1.expn(), q.y2.expn(), q.uv.expn()
	return rdLambdas{
		i4:   max(lambdaScale*qi4*qi4>>lambdaI4Shift, 1),
		i16:  max(lambdaScale*qi16*qi16, 1),
		uv:   max(lambdaScale*quv*quv>>lambdaUVShift, 1),
		mode: max(qi4*qi4>>lambdaModeShift, 1),
		t:    snsStrength * qi4 >> tlambdaShift,
	}
}

func (m *quantMatrix) expn() int64 {
	const nAC = 15
	return int64(m.q[0]+nAC*m.q[1]+1<<(expnShift-1)) >> expnShift
}

// isFlat는 블록들의 0 아닌 AC 레벨 수가 limit 이하인지 본다.
func isFlat(levels [][16]int32, limit int) bool {
	n := 0
	for b := range levels {
		for _, l := range levels[b][1:] {
			if l != 0 {
				n++
				if n > limit {
					return false
				}
			}
		}
	}
	return true
}

// tdistoWeight는 저주파를 더 무겁게 보는 하다마드 계수 가중치다 (libwebp kWeightY).
var tdistoWeight = [16]int32{38, 32, 20, 9, 32, 28, 17, 7, 20, 17, 10, 4, 9, 7, 4, 2}

// tdisto4는 두 4×4 블록(stride sa, sb)의 가중 하다마드 에너지 차이 — 텍스처가 사라지면 커진다.
func tdisto4(a []uint8, sa int, b []uint8, sb int) int64 {
	return int64(abs(int(ttransform(a, sa)-ttransform(b, sb)))) >> tdistoShift
}

func ttransform(in []uint8, s int) int32 {
	var tmp [16]int32
	for i := range 4 {
		r := in[i*s:]
		a0, a1 := int32(r[0])+int32(r[2]), int32(r[1])+int32(r[3])
		a2, a3 := int32(r[1])-int32(r[3]), int32(r[0])-int32(r[2])
		tmp[i*4], tmp[i*4+1], tmp[i*4+2], tmp[i*4+3] = a0+a1, a3+a2, a3-a2, a0-a1
	}
	var sum int32
	for i := range 4 {
		a0, a1 := tmp[i]+tmp[8+i], tmp[4+i]+tmp[12+i]
		a2, a3 := tmp[4+i]-tmp[12+i], tmp[i]-tmp[8+i]
		sum += tdistoWeight[i]*abs32(a0+a1) + tdistoWeight[4+i]*abs32(a3+a2) +
			tdistoWeight[8+i]*abs32(a3-a2) + tdistoWeight[12+i]*abs32(a0-a1)
	}
	return sum
}

// regionDisto는 원본 평면 영역과 n×n 복원 블록의 SSE와 (tλ>0이면) 텍스처 왜곡을 구한다.
func regionDisto(src []uint8, s, x0, y0 int, rec []uint8, n int, tlambda int64) (d, sd int64) {
	for j := range n {
		for i := range n {
			v := int64(src[(y0+j)*s+x0+i]) - int64(rec[j*n+i])
			d += v * v
		}
	}
	if tlambda == 0 {
		return d, 0
	}
	var td int64
	for by := 0; by < n; by += 4 {
		for bx := 0; bx < n; bx += 4 {
			td += tdisto4(src[(y0+by)*s+x0+bx:], s, rec[by*n+bx:], n)
		}
	}
	return d, (tlambda*td + 1<<(mult8BShift-1)) >> mult8BShift
}
