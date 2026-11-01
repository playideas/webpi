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
	// 나눗셈 대신 역수 곱셈으로 양자화하는 고정소수점 정밀도 (libwebp QFIX와 같음)
	quantFix = 17
)

// quantMatrix는 한 계수 평면의 DC/AC 양자화 단계와 편향이다.
// iq·rbias는 q·bias에서 만든 역수 곱셈용 값이다 (setReciprocals).
type quantMatrix struct {
	q     [2]int32
	bias  [2]int32
	iq    [2]uint32
	rbias [2]uint32
	vec   quantVecs
}

// quantVecs는 SIMD 커널이 읽는 양자화 값과 변환 상수다. 계수 위치(자연 순서)마다 펼쳐 둔다.
// k의 순서는 internal/asmgen/gen.py의 상수 벡터 정의와 같아야 한다.
type quantVecs struct {
	k  [12]int32
	iq [16]uint32
	rb [16]uint32
	q  [16]int32
}

// transformConsts는 SIMD 커널이 읽는 변환·양자화 상수다 (gen.py의 v12..v14 배치와 같은 순서).
var transformConsts = [12]int32{fdctK1, fdctK2, fdctRound1, fdctRound3, fdctRound4, fdctRound12, fdctRound0, maxLevel,
	idctC2, idctC1, idctRound, 0}

func (m *quantMatrix) setReciprocals() {
	for k := range m.q {
		m.iq[k] = (1 << quantFix) / uint32(m.q[k])
		m.rbias[k] = uint32(m.bias[k]) << (quantFix - 8)
	}
	m.vec.k = transformConsts
	for pos := range 16 {
		k := b2i(pos > 0)
		m.vec.iq[pos], m.vec.rb[pos], m.vec.q[pos] = m.iq[k], m.rbias[k], m.q[k]
	}
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
	q := quantizer{
		qi: qi,
		y1: quantMatrix{q: [2]int32{int32(dequantTableDC[qi]), int32(dequantTableAC[qi])}, bias: [2]int32{96, 110}},
		y2: quantMatrix{q: [2]int32{int32(dequantTableDC[qi]) * 2, max(y2ac, minY2AC)}, bias: [2]int32{96, 108}},
		uv: quantMatrix{q: [2]int32{int32(dequantTableDC[uvdc]), int32(dequantTableAC[uvac])}, bias: [2]int32{110, 115}},
	}
	q.y1.setReciprocals()
	q.y2.setReciprocals()
	q.uv.setReciprocals()
	return q
}

// quantize는 자연 순서 계수 c를 양자화해 레벨을 lv에 쓰고, 복원(역양자화) 계수를 돌려준다.
// first부터 양자화한다 (Y2가 있는 휘도 블록은 DC를 따로 다루므로 1 — lv[0]은 건드리지 않는다).
// 레벨은 maxLevel(2047)로 잘리므로 int16에 들어간다.
func (m *quantMatrix) quantize(c *[16]int32, lv *[16]int16, first int) [16]int32 {
	var deq [16]int32
	for pos := first; pos < 16; pos++ {
		k := b2i(pos > 0)
		v := c[pos]
		mag := v
		if mag < 0 {
			mag = -mag
		}
		level := min(int32((uint32(mag)*m.iq[k]+m.rbias[k])>>quantFix), maxLevel)
		if v < 0 {
			level = -level
		}
		lv[pos] = int16(level)
		deq[pos] = level * m.q[k]
	}
	return deq
}
