package webpi

// 양자화 상수
const (
	maxQIndex = 127
	// 색차 DC는 인덱스 117(값 132)에서 자른다 — RFC 6386 9.6 참조 디코더의 dequant_init
	maxUVDCIndex = 117
	y2ACScaleNum = 155
	y2ACScaleDen = 100
	minY2AC      = 8
	// 반올림 편향(/256): 0.5보다 작게 잡아 0 근처 계수를 0으로 보내는 데드존 — libwebp의 기본값과 같은 수준
	biasDen  = 256
	maxLevel = 2047 // 범주 6이 표현하는 최대 크기 근처에서 자른다
)

// quantMatrix는 한 계수 평면의 DC/AC 양자화 단계와 편향이다.
type quantMatrix struct {
	q    [2]int32
	bias [2]int32
}

// quantizer는 한 세그먼트(또는 프레임 전체)의 양자화 설정이다.
type quantizer struct {
	qi         int
	y1, y2, uv quantMatrix
}

// newQuantizer는 기본 인덱스 qi와 색차 DC/AC 델타에서 디코더와 같은 규칙으로 양자화 단계를 만든다.
func newQuantizer(qi, dqUVDC, dqUVAC int) quantizer {
	qi = min(max(qi, 0), maxQIndex)
	uvdc := min(max(qi+dqUVDC, 0), maxUVDCIndex)
	uvac := min(max(qi+dqUVAC, 0), maxQIndex)
	y2ac := int32(dequantTableAC[qi]) * y2ACScaleNum / y2ACScaleDen
	return quantizer{
		qi: qi,
		y1: quantMatrix{q: [2]int32{int32(dequantTableDC[qi]), int32(dequantTableAC[qi])}, bias: [2]int32{96, 110}},
		y2: quantMatrix{q: [2]int32{int32(dequantTableDC[qi]) * 2, max(y2ac, minY2AC)}, bias: [2]int32{96, 108}},
		uv: quantMatrix{q: [2]int32{int32(dequantTableDC[uvdc]), int32(dequantTableAC[uvac])}, bias: [2]int32{110, 115}},
	}
}

// quantize는 자연 순서 계수 c를 양자화해 레벨을 c에 덮어쓰고, 복원(역양자화) 계수를 돌려준다.
// first부터 양자화한다 (Y2가 있는 휘도 블록은 DC를 따로 다루므로 1).
func (m *quantMatrix) quantize(c *[16]int32, first int) [16]int32 {
	var deq [16]int32
	for pos := first; pos < 16; pos++ {
		k := b2i(pos > 0)
		v := c[pos]
		mag := v
		if mag < 0 {
			mag = -mag
		}
		level := min((mag*biasDen+m.bias[k]*m.q[k])/(m.q[k]*biasDen), maxLevel)
		if v < 0 {
			level = -level
		}
		c[pos] = level
		deq[pos] = level * m.q[k]
	}
	return deq
}
