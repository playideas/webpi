// Package webpi는 순수 Go lossy WebP(VP8) 인코더다.
package webpi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
)

// 프레임 제약 — RFC 6386 9.1, 19.1
const (
	maxDimension     = 1<<14 - 1
	maxFirstPartSize = 1<<19 - 1
	defaultQuality   = 75
	maxQuality       = 100
)

// 휘도 16×16 모드 선택 비트 확률 — RFC 6386 11.2 (키 프레임 고정값)
const (
	probIsI16    = 145
	probYModeA   = 156
	probYModeDC  = 163
	probYModeHE  = 128
	probUVModeA  = 142
	probUVModeVE = 114
	probUVModeHE = 183
)

// Options는 인코딩 설정이다.
type Options struct {
	// Quality는 0~100. 클수록 화질이 높고 파일이 크다.
	Quality float32
}

type encoder struct {
	src, rec *yuvPlanes
	q        quantizer
	mbs      []mbInfo
	probs    probTable
	useSkip  bool
	skipProb uint8
}

// Encode는 m을 lossy WebP로 w에 쓴다. 알파는 버린다.
// 판단 PD-20261030-51b0e794: 최소 골격(i16·고정 양자화)부터, 적응 양자화·RD 결정은 그 위에 얹는다
func Encode(w io.Writer, m image.Image, o *Options) error {
	b := m.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || b.Dx() > maxDimension || b.Dy() > maxDimension {
		return fmt.Errorf("webpi: 지원하지 않는 크기 %dx%d", b.Dx(), b.Dy())
	}
	quality := float32(defaultQuality)
	if o != nil {
		quality = o.Quality
	}
	_, vp8, err := encodeFrame(m, quality)
	if err != nil {
		return err
	}
	return writeRIFF(w, vp8)
}

// encodeFrame은 VP8 프레임을 만들고, 검증용으로 인코더 상태(복원 평면 포함)도 돌려준다.
func encodeFrame(m image.Image, quality float32) (*encoder, []byte, error) {
	e := &encoder{src: toYUV420(m), q: newQuantizer(qualityToQIndex(quality)), probs: defaultTokenProb}
	e.rec = &yuvPlanes{w: e.src.w, h: e.src.h, mbw: e.src.mbw, mbh: e.src.mbh, ys: e.src.ys, cs: e.src.cs,
		y: make([]uint8, len(e.src.y)), u: make([]uint8, len(e.src.u)), v: make([]uint8, len(e.src.v))}
	for mby := range e.src.mbh {
		for mbx := range e.src.mbw {
			e.mbs = append(e.mbs, e.encodeMB(mbx, mby))
		}
	}
	vp8, err := e.bitstream()
	return e, vp8, err
}

// qualityToQIndex는 cwebp와 비슷한 곡선으로 품질을 양자화 인덱스로 바꾼다 (같은 숫자가 비슷한 화질이 되게).
func qualityToQIndex(quality float32) int {
	c := float64(min(max(quality, 0), maxQuality)) / maxQuality
	const knee = 0.75
	linear := c * 2 / 3
	if c >= knee {
		linear = 2*c - 1
	}
	return int(math.Round(maxQIndex * (1 - math.Cbrt(linear))))
}

// bitstream은 확률을 정한 뒤 첫 파티션(헤더·모드)과 토큰 파티션을 써서 VP8 프레임을 만든다.
func (e *encoder) bitstream() ([]byte, error) {
	// skip 여부가 토큰 문맥을 바꾸므로 통계보다 먼저 정한다
	e.chooseSkipProb()
	var stats tokenStats
	e.writeTokens(&tokenWriter{stats: &stats})

	hdr := newBoolEncoder()
	e.writeHeader(hdr, &stats)
	e.writeModes(hdr)
	first := hdr.finish()
	if len(first) > maxFirstPartSize {
		return nil, errors.New("webpi: 첫 파티션이 너무 큼")
	}
	tok := newBoolEncoder()
	e.writeTokens(&tokenWriter{enc: tok, probs: &e.probs})
	tokens := tok.finish()

	out := make([]byte, 0, 10+len(first)+len(tokens))
	// 프레임 태그: 키 프레임(0), 버전 0, show_frame 1, 첫 파티션 크기 19비트
	tag := uint32(1<<4) | uint32(len(first))<<5
	out = append(out, byte(tag), byte(tag>>8), byte(tag>>16))
	out = append(out, 0x9d, 0x01, 0x2a) // 시작 코드
	out = binary.LittleEndian.AppendUint16(out, uint16(e.src.w))
	out = binary.LittleEndian.AppendUint16(out, uint16(e.src.h))
	out = append(out, first...)
	return append(out, tokens...), nil
}

func (e *encoder) chooseSkipProb() {
	skips := 0
	for i := range e.mbs {
		if e.mbs[i].skip {
			skips++
		}
	}
	e.useSkip = skips > 0
	e.skipProb = probFromCounts(uint32(len(e.mbs)-skips), uint32(skips))
}

// writeHeader는 키 프레임 헤더 필드를 RFC 6386 9.2~9.11 순서로 쓴다.
func (e *encoder) writeHeader(h *boolEncoder, stats *tokenStats) {
	h.putBit(uniformProb, false) // 색 공간
	h.putBit(uniformProb, false) // 클램핑 필요
	h.putBit(uniformProb, false) // 세그먼트 없음
	h.putBit(uniformProb, false) // 일반 루프 필터
	h.putUint(0, 6)              // 필터 레벨 — 골격 단계는 0
	h.putUint(0, 3)              // 선명도
	h.putBit(uniformProb, false) // 모드/참조별 필터 델타 없음
	h.putUint(0, 2)              // 토큰 파티션 1개
	h.putUint(uint32(e.q.qi), 7)
	for range 5 {
		h.putSignedOptional(0, 4) // y1dc, y2dc, y2ac, uvdc, uvac 델타 없음
	}
	h.putBit(uniformProb, false) // refresh_entropy_probs
	e.writeProbUpdates(h, stats)
	h.putBit(uniformProb, e.useSkip)
	if e.useSkip {
		h.putUint(uint32(e.skipProb), 8)
	}
}

// writeModes는 매크로블록별 skip 플래그와 예측 모드를 쓴다.
func (e *encoder) writeModes(h *boolEncoder) {
	for i := range e.mbs {
		mb := &e.mbs[i]
		if e.useSkip {
			h.putBit(e.skipProb, mb.skip)
		}
		h.putBit(probIsI16, true)
		h.putBit(probYModeA, mb.yMode == predHE || mb.yMode == predTM)
		if mb.yMode == predDC || mb.yMode == predVE {
			h.putBit(probYModeDC, mb.yMode == predVE)
		} else {
			h.putBit(probYModeHE, mb.yMode == predTM)
		}
		h.putBit(probUVModeA, mb.uvMode != predDC)
		if mb.uvMode != predDC {
			h.putBit(probUVModeVE, mb.uvMode != predVE)
			if mb.uvMode != predVE {
				h.putBit(probUVModeHE, mb.uvMode == predTM)
			}
		}
	}
}

func writeRIFF(w io.Writer, vp8 []byte) error {
	pad := len(vp8) & 1
	var hdr []byte
	hdr = append(hdr, "RIFF"...)
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(4+8+len(vp8)+pad))
	hdr = append(hdr, "WEBPVP8 "...)
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(vp8)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if _, err := w.Write(vp8); err != nil {
		return err
	}
	if pad == 1 {
		_, err := w.Write([]byte{0})
		return err
	}
	return nil
}
