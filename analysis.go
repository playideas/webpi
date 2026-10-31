package webpi

// 매크로블록 민감도(alpha) 분석과 세그먼트 배정.
// 텍스처가 많은 블록은 양자화 오차가 눈에 덜 띄므로 거칠게, 평탄한 블록은 곱게 양자화하기 위한 척도다.
// 절차와 상수는 libwebp analysis.c를 따른다.
const (
	maxAlpha       = 255
	alphaScale     = 2 * maxAlpha
	maxCoeffThresh = 31
	coeffBinShift  = 3
	nSegments      = 4
	maxKMeansIters = 6
	// 중심 이동 합이 이보다 작으면 k-means를 멈춘다
	kMeansMinDisplacement = 5
	// 분석에는 DC·TM 예측만 쓴다 — 나머지 모드를 더 봐도 분류는 거의 바뀌지 않는다
	nAnalysisModes = 2
	// 최종 민감도 = (3·휘도 + 1·색차) / 4
	lumaAlphaWeight = 3
	alphaMixShift   = 2
)

// histogram은 DCT 계수 크기(>>3, 31에서 자름)의 분포다.
type histogram [maxCoeffThresh + 1]int

// add는 평면 pl의 (x0,y0)에서 n×n 영역을 4×4 블록으로 나눠 예측 잔차의 DCT 계수를 누적한다.
func (h *histogram) add(pl []uint8, s, x0, y0 int, pred []uint8, n int) {
	for by := 0; by < n; by += 4 {
		for bx := 0; bx < n; bx += 4 {
			var res [16]int32
			for j := range 4 {
				for i := range 4 {
					res[j*4+i] = int32(pl[(y0+by+j)*s+x0+bx+i]) - int32(pred[(by+j)*n+bx+i])
				}
			}
			for _, c := range fdct4(&res) {
				h[min(abs32(c)>>coeffBinShift, maxCoeffThresh)]++
			}
		}
	}
}

// alpha는 분포가 넓게 퍼질수록(예측이 어려울수록) 커지는 값이다.
func (h *histogram) alpha() int {
	maxValue, lastNonZero := 0, 1
	for k, v := range h {
		if v > 0 {
			maxValue = max(maxValue, v)
			lastNonZero = k
		}
	}
	if maxValue <= 1 {
		return 0
	}
	return alphaScale * lastNonZero / maxValue
}

// mbAlphas는 원본 평면만으로 매크로블록별 최종 민감도(클수록 평탄)와 색차 민감도 평균을 구한다.
// 매크로블록끼리 독립이라 행 단위로 나눠 동시에 계산한다.
func mbAlphas(src *yuvPlanes) (alphas []int, uvAlphaAvg int) {
	alphas = make([]int, src.mbw*src.mbh)
	uvRows := make([]int, src.mbh)
	parallelRows(src.mbh, func(_, mby int) {
		for mbx := range src.mbw {
			ya, uva := mbRawAlphas(src, mbx, mby)
			a := (lumaAlphaWeight*ya + uva + 1<<(alphaMixShift-1)) >> alphaMixShift
			alphas[mby*src.mbw+mbx] = min(max(maxAlpha-a, 0), maxAlpha)
			uvRows[mby] += uva
		}
	})
	uvSum := 0
	for _, v := range uvRows {
		uvSum += v
	}
	return alphas, uvSum / len(alphas)
}

// mbRawAlphas는 분석 모드 중 가장 큰 휘도·색차 alpha를 돌려준다 (libwebp와 같은 선택).
func mbRawAlphas(src *yuvPlanes, mbx, mby int) (yAlpha, uvAlpha int) {
	x0, y0 := mbx*mbSize, mby*mbSize
	cx, cy := mbx*uvSize, mby*uvSize
	ye := loadEdges(src.y, src.ys, x0, y0, mbSize)
	ue := loadEdges(src.u, src.cs, cx, cy, uvSize)
	ve := loadEdges(src.v, src.cs, cx, cy, uvSize)
	yAlpha, uvAlpha = -1, -1
	var buf predBuf
	for m := range nAnalysisModes {
		var hy, huv histogram
		hy.add(src.y, src.ys, x0, y0, predict(m, &ye, mbSize, &buf), mbSize)
		huv.add(src.u, src.cs, cx, cy, predict(m, &ue, uvSize, &buf), uvSize)
		huv.add(src.v, src.cs, cx, cy, predict(m, &ve, uvSize, &buf), uvSize)
		yAlpha, uvAlpha = max(yAlpha, hy.alpha()), max(uvAlpha, huv.alpha())
	}
	return yAlpha, uvAlpha
}

// assignSegments는 민감도를 k-means로 4개 무리로 나눠 매크로블록별 세그먼트,
// 세그먼트별 정규화 민감도(-127~127, 0이 전체 가중 평균)와 복잡도 순위(0~255, 0이 가장 복잡)를 돌려준다.
func assignSegments(alphas []int) (segs []uint8, segAlpha, segBeta [nSegments]int) {
	var counts [maxAlpha + 1]int
	for _, a := range alphas {
		counts[a]++
	}
	centers, mapping, mid := kMeans(&counts)
	segs = make([]uint8, len(alphas))
	for i, a := range alphas {
		segs[i] = mapping[a]
	}
	lo, hi := centers[0], centers[0]
	for _, c := range centers {
		lo, hi = min(lo, c), max(hi, c)
	}
	if hi == lo {
		hi = lo + 1
	}
	for n, c := range centers {
		segAlpha[n] = min(max(maxAlpha*(c-mid)/(hi-lo), -maxSegAlpha), maxSegAlpha)
		segBeta[n] = min(max(maxAlpha*(c-lo)/(hi-lo), 0), maxAlpha)
	}
	return segs, segAlpha, segBeta
}

const maxSegAlpha = 127

// kMeans는 민감도 분포 counts를 무리 짓고 중심, 값→세그먼트 표, 가중 평균 중심을 돌려준다.
func kMeans(counts *[maxAlpha + 1]int) (centers [nSegments]int, mapping [maxAlpha + 1]uint8, mid int) {
	lo, hi := 0, maxAlpha
	for lo < maxAlpha && counts[lo] == 0 {
		lo++
	}
	for hi > lo && counts[hi] == 0 {
		hi--
	}
	for k := range nSegments {
		centers[k] = lo + (2*k+1)*(hi-lo)/(2*nSegments)
	}
	for range maxKMeansIters {
		var sum, weight [nSegments]int
		n := 0
		for a := lo; a <= hi; a++ {
			if counts[a] == 0 {
				continue
			}
			// 중심은 오름차순이므로 a가 커질수록 가까운 중심도 앞으로만 움직인다
			for n+1 < nSegments && abs(a-centers[n+1]) < abs(a-centers[n]) {
				n++
			}
			mapping[a] = uint8(n)
			sum[n] += a * counts[a]
			weight[n] += counts[a]
		}
		displaced, total := 0, 0
		mid = 0
		for k := range nSegments {
			if weight[k] > 0 {
				c := (sum[k] + weight[k]/2) / weight[k]
				displaced += abs(centers[k] - c)
				centers[k] = c
				mid += c * weight[k]
				total += weight[k]
			}
		}
		mid = (mid + total/2) / total
		if displaced < kMeansMinDisplacement {
			break
		}
	}
	return centers, mapping, mid
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
