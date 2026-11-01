// Package webpi는 순수 Go lossy WebP(VP8) 인코더다.
package webpi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
)

// 프레임 제약 — RFC 6386 9.1, 19.1
const (
	maxDimension     = 1<<14 - 1
	maxFirstPartSize = 1<<19 - 1
	defaultQuality   = 75
	maxQuality       = 100
	qIndexBits       = 7
	qDeltaBits       = 4
	filterLevelBits  = 6
)

// Options는 인코딩 설정이다.
type Options struct {
	// Quality는 0~100. 클수록 화질이 높고 파일이 크다.
	Quality float32
}

type encoder struct {
	src, rec *yuvPlanes
	seg      segmentation
	lambdas  [nSegments]rdLambdas
	// 인코딩 중 비용 계산에 쓰는 위쪽 문맥 (디코더가 보는 것과 같다). 왼쪽 문맥은 rowCoder가 가진다.
	nzTop    []nzContext
	modeTop  []uint8
	mbs      []mbHeader
	coefs    [][]int16 // 행별 압축 계수 — 건너뛰지 않는 매크로블록만, 래스터 순서
	probs    probTable
	costs    *levelCosts // probs에 맞춘 RD 비용 표
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
	src := toYUV420(m)
	e := &encoder{src: src, seg: newSegmentation(src, quality), probs: defaultTokenProb}
	e.rec = &yuvPlanes{w: e.src.w, h: e.src.h, mbw: e.src.mbw, mbh: e.src.mbh, ys: e.src.ys, cs: e.src.cs,
		y: make([]uint8, len(e.src.y)), u: make([]uint8, len(e.src.u)), v: make([]uint8, len(e.src.v))}
	for n := range nSegments {
		e.lambdas[n] = newRDLambdas(&e.seg.quant[n])
	}
	e.costs = defaultLevelCosts()
	e.nzTop, e.modeTop = make([]nzContext, src.mbw), make([]uint8, src.mbw*4)
	e.mbs, e.coefs = make([]mbHeader, src.mbw*src.mbh), make([][]int16, src.mbh)
	stats := e.encodeRows()
	vp8, err := e.bitstream(stats)
	return e, vp8, err
}

// 행 r의 매크로블록 x는 윗행이 x+1까지 끝나야 시작할 수 있다 — i4의 오른쪽 위 픽셀이 윗행 x+1에 있다.
const wavefrontLag = 2

// 행 계수 버퍼의 처음 크기(매크로블록당, 최대 25·17=425). COCO 8장의 평균은 q75 131, q90 217이고
// 가장 큰 행의 평균은 q75 282, q95 373이라 대부분의 행이 한 번도 늘지 않는다
const coefsPerMBHint = 256

// encodeRows는 매크로블록 행들을 대각선 물결(wavefront)로 동시에 인코딩하고 프레임의 토큰 통계를 돌려준다.
// 각 매크로블록은 왼쪽·위·오른쪽 위 이웃만 보므로, 윗행이 두 칸 앞서 있으면 결과가 순차 인코딩과 같다.
// 행마다 끝낸 매크로블록 수를 채널 토큰으로 아랫행에 알린다 — 채널이 메모리 순서도 보장한다.
func (e *encoder) encodeRows() *tokenStats {
	mbw, mbh := e.src.mbw, e.src.mbh
	done := make([]chan struct{}, mbh)
	for y := range done {
		done[y] = make(chan struct{}, mbw)
	}
	// 통계는 더하기라 작업자별로 모아 합쳐도 행 순서와 무관하게 같다
	stats := make([]tokenStats, rowWorkers(mbh))
	// 행을 위에서부터 차례로 집어 가므로 기다리는 윗행은 항상 이미 누군가 맡고 있다
	parallelRows(mbh, func(w, y int) { e.encodeRow(y, done, &stats[w]) })
	total := &stats[0]
	for w := 1; w < len(stats); w++ {
		addStats(total, &stats[w])
	}
	return total
}

func addStats(dst, src *tokenStats) {
	for i := range dst {
		for j := range dst[i] {
			for k := range dst[i][j] {
				for l := range dst[i][j][k] {
					dst[i][j][k][l][0] += src[i][j][k][l][0]
					dst[i][j][k][l][1] += src[i][j][k][l][1]
				}
			}
		}
	}
}

func (e *encoder) encodeRow(y int, done []chan struct{}, stats *tokenStats) {
	mbw := e.src.mbw
	e.coefs[y] = make([]int16, 0, mbw*coefsPerMBHint)
	r := &rowCoder{encoder: e, modes: modeContext{top: e.modeTop}, stats: stats, coefs: &e.coefs[y]}
	above := 0 // 윗행에서 끝났다고 확인한 매크로블록 수
	for x := range mbw {
		for y > 0 && above < min(x+wavefrontLag, mbw) {
			<-done[y-1]
			above++
		}
		r.encodeMB(x, y, &e.mbs[y*mbw+x])
		done[y] <- struct{}{}
	}
}

// bitstream은 확률을 정한 뒤 첫 파티션(헤더·모드)과 토큰 파티션을 써서 VP8 프레임을 만든다.
// stats는 인코딩 중 모은, 건너뛰지 않는 매크로블록들의 토큰 통계다.
func (e *encoder) bitstream(stats *tokenStats) ([]byte, error) {
	e.chooseSkipProb()
	hdr := newBoolEncoder()
	e.writeHeader(hdr, stats)
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
	e.writeSegmentHeader(h)
	h.putBit(uniformProb, false) // 일반(강한) 루프 필터
	// 세그먼트가 절대값으로 덮어쓰므로 프레임 레벨은 형식상 값이다
	h.putUint(uint32(e.seg.filter[0]), filterLevelBits)
	h.putUint(0, 3)              // 선명도
	h.putBit(uniformProb, false) // 모드/참조별 필터 델타 없음
	h.putUint(0, 2)              // 토큰 파티션 1개
	// 세그먼트가 절대값으로 덮어쓰므로 기본 인덱스는 형식상 값이다
	h.putUint(uint32(e.seg.quant[0].qi), qIndexBits)
	for range 3 {
		h.putSignedOptional(0, qDeltaBits) // y1dc, y2dc, y2ac 델타 없음
	}
	h.putSignedOptional(e.seg.dqUVDC, qDeltaBits)
	h.putSignedOptional(e.seg.dqUVAC, qDeltaBits)
	h.putBit(uniformProb, false) // refresh_entropy_probs
	e.writeProbUpdates(h, stats)
	h.putBit(uniformProb, e.useSkip)
	if e.useSkip {
		h.putUint(uint32(e.skipProb), 8)
	}
}

// writeSegmentHeader는 세그먼트 사용, 세그먼트별 절대 양자화 인덱스, id 트리 확률을 쓴다 (RFC 6386 9.3).
func (e *encoder) writeSegmentHeader(h *boolEncoder) {
	h.putBit(uniformProb, true) // 세그먼트 사용
	h.putBit(uniformProb, true) // 매크로블록 세그먼트 맵 갱신
	h.putBit(uniformProb, true) // 세그먼트 값 갱신
	h.putBit(uniformProb, true) // 절대값 모드
	for n := range nSegments {
		h.putSignedOptional(e.seg.quant[n].qi, qIndexBits)
	}
	for _, f := range e.seg.filter {
		h.putSignedOptional(f, filterLevelBits)
	}
	for _, p := range e.seg.probs {
		// 255는 갱신 생략으로 표현한다
		h.putBit(uniformProb, p != maxProb)
		if p != maxProb {
			h.putUint(uint32(p), probValueBits)
		}
	}
}

// writeSegmentID는 2단 트리로 세그먼트 id를 쓴다.
func (e *encoder) writeSegmentID(h *boolEncoder, id uint8) {
	h.putBit(e.seg.probs[0], id >= nSegments/2)
	if id < nSegments/2 {
		h.putBit(e.seg.probs[1], id == 1)
	} else {
		h.putBit(e.seg.probs[2], id == 3)
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
