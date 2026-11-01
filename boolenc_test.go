package webpi

import (
	"math/rand/v2"
	"testing"
)

// boolDecoder는 RFC 6386 7.3의 참조 디코더를 옮긴 테스트용 복호기다.
type boolDecoder struct {
	buf      []byte
	pos      int
	value    uint32
	rng      uint32
	bitCount int
}

func newBoolDecoder(buf []byte) *boolDecoder {
	d := &boolDecoder{buf: buf, rng: 255}
	for range 2 {
		d.value = d.value<<8 | uint32(d.next())
	}
	return d
}

func (d *boolDecoder) next() byte {
	if d.pos >= len(d.buf) {
		return 0
	}
	b := d.buf[d.pos]
	d.pos++
	return b
}

func (d *boolDecoder) getBit(prob uint8) bool {
	split := 1 + ((d.rng-1)*uint32(prob))>>8
	bigSplit := split << 8
	var bit bool
	if d.value >= bigSplit {
		bit = true
		d.rng -= split
		d.value -= bigSplit
	} else {
		d.rng = split
	}
	for d.rng < 128 {
		d.value <<= 1
		d.rng <<= 1
		d.bitCount++
		if d.bitCount == 8 {
			d.bitCount = 0
			d.value |= uint32(d.next())
		}
	}
	return bit
}

func Test_boolEncoder_randomBits_roundTripsThroughReferenceDecoder(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	const n = 100000
	probs := make([]uint8, n)
	bits := make([]bool, n)
	e := newBoolEncoder()
	for i := range n {
		probs[i] = uint8(1 + r.IntN(255))
		// 확률에 맞게 치우친 비트 — 실제 부호화처럼 올림 전파가 자주 일어나게
		bits[i] = r.IntN(256) >= int(probs[i])
		e.putBit(probs[i], bits[i])
	}
	d := newBoolDecoder(e.finish())
	for i := range n {
		if got := d.getBit(probs[i]); got != bits[i] {
			t.Fatalf("bit %d: got %v want %v", i, got, bits[i])
		}
	}
}

func Test_boolEncoder_extremeProbs_roundTrip(t *testing.T) {
	e := newBoolEncoder()
	pattern := []bool{true, true, true, false, true, false, false, true}
	for i := range 5000 {
		e.putBit(1, pattern[i%len(pattern)])
		e.putBit(255, pattern[(i+3)%len(pattern)])
	}
	d := newBoolDecoder(e.finish())
	for i := range 5000 {
		if d.getBit(1) != pattern[i%len(pattern)] || d.getBit(255) != pattern[(i+3)%len(pattern)] {
			t.Fatalf("mismatch at %d", i)
		}
	}
}
