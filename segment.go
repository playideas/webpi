package webpi

import "math"

// SNS(공간 잡음 민감도) 상수 — libwebp quant_enc.c의 VP8SetSegmentParams와 같은 값
const (
	// 세기 0~100. cwebp 기본값과 같은 50
	snsStrength = 50
	snsToDQ     = 0.9
	percent     = 100
	// 색차 민감도 평균은 보통 60 근처에 있고 30(나쁨)~100(색차를 더 깎아도 됨) 범위를 쓴다
	midUVAlpha = 64
	minUVAlpha = 30
	maxUVAlpha = 100
	minDQUV    = -4
	maxDQUV    = 6
	// 색차 DC는 높은 양자화에서 평탄한 얼룩이 눈에 잘 띄어 SNS 세기만큼 조금 곱게 한다
	uvDCBoost = -4
)

// segmentation은 프레임의 세그먼트 배정과 세그먼트별 양자화다.
type segmentation struct {
	ids    []uint8 // 매크로블록별 세그먼트
	quant  [nSegments]quantizer
	probs  [nSegments - 1]uint8 // 세그먼트 id 트리 확률
	dqUVDC int
	dqUVAC int
}

// newSegmentation은 민감도 분석으로 세그먼트를 나누고, 평탄한 세그먼트일수록 작은 양자화 인덱스를 준다.
// 판단 PD-20261031-f7dbadde: 세그먼트 적응 양자화(4 세그먼트 + SNS)부터 얹는다
func newSegmentation(src *yuvPlanes, quality float32) segmentation {
	alphas, uvAlpha := mbAlphas(src)
	ids, segAlpha := assignSegments(alphas)
	s := segmentation{ids: ids}
	s.dqUVAC = (uvAlpha - midUVAlpha) * (maxDQUV - minDQUV) / (maxUVAlpha - minUVAlpha)
	s.dqUVAC = min(max(s.dqUVAC*snsStrength/percent, minDQUV), maxDQUV)
	s.dqUVDC = uvDCBoost * snsStrength / percent
	cBase := qualityToCompression(quality)
	amp := snsToDQ * snsStrength / percent / (maxSegAlpha + 1)
	for n := range nSegments {
		c := math.Pow(cBase, 1-amp*float64(segAlpha[n]))
		s.quant[n] = newQuantizer(int(maxQIndex*(1-c)), s.dqUVDC, s.dqUVAC)
	}
	s.probs = segmentProbs(ids)
	return s
}

// segmentProbs는 id 트리(첫 비트: {0,1} 대 {2,3})의 확률을 실제 빈도로 정한다.
func segmentProbs(ids []uint8) [nSegments - 1]uint8 {
	var c [nSegments]uint32
	for _, id := range ids {
		c[id]++
	}
	return [nSegments - 1]uint8{
		segProb(c[0]+c[1], c[2]+c[3]),
		segProb(c[0], c[1]),
		segProb(c[2], c[3]),
	}
}

// segProb는 0쪽 확률을 돌려준다. 한쪽이 비면 255 — 헤더에서 갱신을 생략할 수 있는 값이다.
func segProb(zeros, ones uint32) uint8 {
	total := zeros + ones
	if total == 0 {
		return maxProb
	}
	return uint8((uint64(zeros)*maxProb + uint64(total)/2) / uint64(total))
}

// qualityToCompression은 cwebp와 같은 곡선으로 품질(0~100)을 압축 계수 c(0~1)로 바꾼다.
// 양자화 인덱스는 127·(1−c)이다.
func qualityToCompression(quality float32) float64 {
	c := float64(min(max(quality, 0), maxQuality)) / maxQuality
	const knee = 0.75
	linear := c * 2 / 3
	if c >= knee {
		linear = 2*c - 1
	}
	return math.Cbrt(linear)
}
