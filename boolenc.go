package webpi

// 불리언 인코더 상수 — RFC 6386 7.3
const (
	boolInitRange    = 255
	boolInitBitCount = 24
	boolRenormRange  = 128
	boolTopBit       = 1 << 31
	boolLowMask      = 1<<24 - 1
	boolFlushBits    = 32
	uniformProb      = 128
)

// boolEncoder는 VP8 불리언(이진 산술) 부호기다. prob는 비트가 0일 확률 × 256.
type boolEncoder struct {
	out      []byte
	rng      uint32
	bottom   uint32
	bitCount int
}

func newBoolEncoder() *boolEncoder {
	return &boolEncoder{rng: boolInitRange, bitCount: boolInitBitCount}
}

func (e *boolEncoder) putBit(prob uint8, bit bool) {
	split := 1 + ((e.rng-1)*uint32(prob))>>8
	if bit {
		e.bottom += split
		e.rng -= split
	} else {
		e.rng = split
	}
	for e.rng < boolRenormRange {
		e.rng <<= 1
		if e.bottom&boolTopBit != 0 {
			e.carry()
		}
		e.bottom <<= 1
		e.bitCount--
		if e.bitCount == 0 {
			e.out = append(e.out, byte(e.bottom>>24))
			e.bottom &= boolLowMask
			e.bitCount = 8
		}
	}
}

// carry는 이미 내보낸 바이트로 올림을 전파한다.
func (e *boolEncoder) carry() {
	i := len(e.out) - 1
	for ; i >= 0 && e.out[i] == 0xff; i-- {
		e.out[i] = 0
	}
	if i >= 0 {
		e.out[i]++
	}
}

// putUint는 n비트 부호 없는 정수를 MSB부터 균등 확률로 쓴다.
func (e *boolEncoder) putUint(v uint32, n int) {
	for n > 0 {
		n--
		e.putBit(uniformProb, v>>uint(n)&1 == 1)
	}
}

// putSignedOptional은 "있음" 플래그 + 크기 n비트 + 부호를 쓴다 (0이면 플래그만).
func (e *boolEncoder) putSignedOptional(v int, n int) {
	e.putBit(uniformProb, v != 0)
	if v == 0 {
		return
	}
	mag := v
	if mag < 0 {
		mag = -mag
	}
	e.putUint(uint32(mag), n)
	e.putBit(uniformProb, v < 0)
}

// finish는 남은 상태를 밀어내고 결과 바이트를 돌려준다 (libvpx와 같이 0 비트 32개로 비운다).
func (e *boolEncoder) finish() []byte {
	for range boolFlushBits {
		e.putBit(uniformProb, false)
	}
	return e.out
}
