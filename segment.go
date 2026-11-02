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
	// 색차 AC를 libwebp보다 3단계 곱게 한다. 색차는 세그먼트 양자화(휘도의 질감 가림 기준)를 그대로 따르므로
	// 질감 많은 세그먼트에서 지나치게 깎인다. COCO 150장에서 −3이 휘도 지표를 ~2% 내주고
	// 색차 SSIM을 ~6%, RGB PSNR을 ~1% 얻는 균형점이었다 (−2·−4, DC 이동과 비교, bench/results 참조)
	uvACBias = -3
	// 높은 품질에서는 편향을 0(libwebp)으로 줄인다. 색차 SSIM이 포화(두 인코더 모두 q96부터 0.964)해
	// 색차에 더 쓴 비트가 휘도 손해로만 남았다. 품질 uvBiasZeroQuality부터 0이고, 그 아래로 품질
	// uvBiasStepWidth마다 한 단계씩 커져 q75 이하는 uvACBias다. libwebp q90·q95 화질에서 휘도 SSIM 기준
	// +6%였던 격차가 +2.0%·+0.8%로 준다 (q95에서 −1은 색차 이득 없이 휘도만 +2.3%, bench/results/webpi-highq.txt)
	uvBiasZeroQuality = 95
	uvBiasStepWidth   = 10
)

// 루프 필터 세기 — libwebp filter_enc.c SetupFilterStrength (cwebp 기본 -f 60, 선명도 0)
const (
	filterStrength = 60
	filterLevelMul = 5 // level0 = 5·세기, 0~500
	betaBase       = 256
	// AC 양자화 단계를 4로 나눈 값이 블록 경계에 생기는 계단 크기의 척도다
	qstepShift = 2
	// 이보다 약한 필터는 효과가 없어 끈다
	minFilterLevel = 2
	maxFilterLevel = 63
)

// segmentation은 프레임의 세그먼트 배정과 세그먼트별 양자화다.
type segmentation struct {
	ids    []uint8 // 매크로블록별 세그먼트
	quant  [nSegments]quantizer
	probs  [nSegments - 1]uint8 // 세그먼트 id 트리 확률
	filter [nSegments]int       // 세그먼트별 루프 필터 레벨
	// 병합 뒤 남은 서로 다른 세그먼트 수. 1이면 세그먼트를 끄고 매크로블록별 id도 쓰지 않는다
	distinct int
	dqUVDC   int
	dqUVAC   int
}

// newSegmentation은 민감도 분석으로 세그먼트를 나누고, 평탄한 세그먼트일수록 작은 양자화 인덱스를 준다.
// 판단 PD-20261031-f7dbadde: 세그먼트 적응 양자화(4 세그먼트 + SNS)부터 얹는다
func newSegmentation(src *yuvPlanes, quality float32) segmentation {
	alphas, uvAlpha := mbAlphas(src)
	ids, segAlpha, segBeta := assignSegments(alphas)
	s := segmentation{ids: ids}
	s.dqUVAC = (uvAlpha - midUVAlpha) * (maxDQUV - minDQUV) / (maxUVAlpha - minUVAlpha)
	s.dqUVAC = min(max(s.dqUVAC*snsStrength/percent, minDQUV), maxDQUV) + uvACBiasAt(quality)
	s.dqUVDC = uvDCBoost * snsStrength / percent
	cBase := qualityToCompression(quality)
	amp := snsToDQ * snsStrength / percent / (maxSegAlpha + 1)
	for n := range nSegments {
		c := math.Pow(cBase, 1-amp*float64(segAlpha[n]))
		s.quant[n] = newQuantizer(int(maxQIndex*(1-c)), s.dqUVDC, s.dqUVAC)
		s.filter[n] = filterLevel(s.quant[n].qi, segBeta[n])
	}
	s.mergeDuplicates()
	s.probs = segmentProbs(ids)
	return s
}

// mergeDuplicates는 양자화 인덱스와 필터 레벨이 같은 세그먼트를 앞쪽 세그먼트로 합친다 (libwebp
// SimplifySegments). 높은 품질에서는 세그먼트 인덱스가 0에 몰려 같아지는데, 따로 두면 매크로블록마다
// 정보 없는 id 비트를 쓴다. 합친 세그먼트는 확률 255로 거의 공짜가 되고, 하나만 남으면 맵 자체를 뺀다.
// qi가 같으면 색차 델타도 프레임 공통이라 양자화기와 λ가 같아 인코딩 결정은 그대로다.
func (s *segmentation) mergeDuplicates() {
	var remap [nSegments]uint8
	s.distinct = 0
	for n := range nSegments {
		remap[n] = uint8(n)
		for m := range n {
			if remap[m] == uint8(m) && s.quant[m].qi == s.quant[n].qi && s.filter[m] == s.filter[n] {
				remap[n] = uint8(m)
				break
			}
		}
		if remap[n] == uint8(n) {
			s.distinct++
		}
	}
	for i, id := range s.ids {
		s.ids[i] = remap[id]
	}
}

// uvACBiasAt은 품질 quality에서의 색차 AC 편향이다: q95 이상 0, q85 초과 −1, q75 초과 −2, 그 아래 uvACBias.
// 판단 PD-20261101-853f635a: 색차 AC 편향은 높은 품질로 갈수록 libwebp 설정으로 줄인다
func uvACBiasAt(quality float32) int {
	if quality >= uvBiasZeroQuality {
		return 0
	}
	steps := int(math.Floor(float64(uvBiasZeroQuality-quality)/uvBiasStepWidth)) + 1
	return -min(steps, -uvACBias)
}

// filterLevel은 양자화가 거칠수록(경계 계단이 클수록) 세게, 평탄한 세그먼트일수록 약하게 거른다.
// 선명도 0에서는 계단 크기가 곧 그 계단을 거르는 최소 레벨이다.
func filterLevel(qi, beta int) int {
	qstep := min(int(dequantTableAC[qi])>>qstepShift, maxFilterLevel)
	f := qstep * filterLevelMul * filterStrength / (betaBase + beta)
	if f < minFilterLevel {
		return 0
	}
	return min(f, maxFilterLevel)
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
