package main

// SSIM 상수 — Wang et al. 2004 표준값 (L=255, K1=0.01, K2=0.03)
const (
	ssimC1     = (0.01 * 255) * (0.01 * 255)
	ssimC2     = (0.03 * 255) * (0.03 * 255)
	ssimWindow = 8 // 8×8 창
	ssimStride = 4 // 창을 절반씩 겹쳐 민다
)

// planeSSIM은 두 평면의 평균 SSIM이다. 가우시안 대신 균등 창을 쓴 간이판 —
// 코덱끼리 상대 비교용이라 절대값의 정밀도는 필요 없다.
func planeSSIM(pa, pb []float64, w, h int) float64 {
	var total float64
	var count int
	for y := 0; y+ssimWindow <= h; y += ssimStride {
		for x := 0; x+ssimWindow <= w; x += ssimStride {
			total += windowSSIM(pa, pb, w, x, y)
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return total / float64(count)
}

func windowSSIM(ya, yb []float64, w, x0, y0 int) float64 {
	var sa, sb, saa, sbb, sab float64
	for y := y0; y < y0+ssimWindow; y++ {
		for x := x0; x < x0+ssimWindow; x++ {
			pa, pb := ya[y*w+x], yb[y*w+x]
			sa, sb = sa+pa, sb+pb
			saa, sbb, sab = saa+pa*pa, sbb+pb*pb, sab+pa*pb
		}
	}
	n := float64(ssimWindow * ssimWindow)
	ma, mb := sa/n, sb/n
	va, vb, cov := saa/n-ma*ma, sbb/n-mb*mb, sab/n-ma*mb
	return ((2*ma*mb + ssimC1) * (2*cov + ssimC2)) / ((ma*ma + mb*mb + ssimC1) * (va + vb + ssimC2))
}
