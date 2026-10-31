package main

import (
	"image"
	"math"
)

// 다중 지표: 휘도 SSIM 하나만 맞추면 그 지표에 과적합된 인코더를 구분할 수 없다.
// 색차 구조, 채널 가중 오차, RGB 오차를 함께 봐서 한 지표에서만 이기는 경우를 걸러낸다.
const (
	metricSSIMY   = iota // 휘도 SSIM (기존 합격 기준)
	metricSSIMUV         // Cb·Cr SSIM 평균 — 색차 품질
	metricPSNRYUV        // (6·Y + Cb + Cr)/8 가중 MSE의 PSNR — 비디오 코덱 비교 관행
	metricPSNRRGB        // RGB 세 채널 MSE의 PSNR — 표시 결과 그대로의 오차
	nMetrics
)

var metricNames = [nMetrics]string{"ssim-y", "ssim-uv", "psnr-yuv", "psnr-rgb"}

// YUV PSNR의 채널 가중치 (Y:Cb:Cr = 6:1:1)
const (
	psnrWeightY   = 6
	psnrWeightUV  = 1
	psnrWeightSum = psnrWeightY + 2*psnrWeightUV
	// 8비트 최대값과, 오차가 0일 때 PSNR이 무한대가 되지 않게 하는 상한
	pixelMax = 255
	maxPSNR  = 99
)

// BT.601 full range RGB→YCbCr 계수 (JPEG와 같은 정의)
const (
	cbR, cbG, cbB = -0.168736, -0.331264, 0.5
	crR, crG, crB = 0.5, -0.418688, -0.081312
	chromaOffset  = 128
)

// planes는 한 이미지의 지표 계산용 평면들이다.
type planes struct {
	w, h      int
	y, cb, cr []float64
	r, g, b   []float64
}

func toPlanes(img *image.RGBA) planes {
	bd := img.Bounds()
	w, h := bd.Dx(), bd.Dy()
	p := planes{w: w, h: h}
	for _, s := range []*[]float64{&p.y, &p.cb, &p.cr, &p.r, &p.g, &p.b} {
		*s = make([]float64, w*h)
	}
	for y := range h {
		for x := range w {
			i := img.PixOffset(bd.Min.X+x, bd.Min.Y+y)
			r, g, b := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2])
			k := y*w + x
			p.r[k], p.g[k], p.b[k] = r, g, b
			p.y[k] = 0.299*r + 0.587*g + 0.114*b
			p.cb[k] = cbR*r + cbG*g + cbB*b + chromaOffset
			p.cr[k] = crR*r + crG*g + crB*b + chromaOffset
		}
	}
	return p
}

// allMetrics는 원본 a와 복원 b의 모든 지표를 계산한다.
func allMetrics(a, b *image.RGBA) [nMetrics]float64 {
	pa, pb := toPlanes(a), toPlanes(b)
	var m [nMetrics]float64
	m[metricSSIMY] = planeSSIM(pa.y, pb.y, pa.w, pa.h)
	m[metricSSIMUV] = (planeSSIM(pa.cb, pb.cb, pa.w, pa.h) + planeSSIM(pa.cr, pb.cr, pa.w, pa.h)) / 2
	yuv := (psnrWeightY*mse(pa.y, pb.y) + psnrWeightUV*(mse(pa.cb, pb.cb)+mse(pa.cr, pb.cr))) / psnrWeightSum
	m[metricPSNRYUV] = psnr(yuv)
	m[metricPSNRRGB] = psnr((mse(pa.r, pb.r) + mse(pa.g, pb.g) + mse(pa.b, pb.b)) / 3)
	return m
}

func mse(a, b []float64) float64 {
	var s float64
	for i := range a {
		d := a[i] - b[i]
		s += d * d
	}
	return s / float64(len(a))
}

func psnr(mse float64) float64 {
	if mse == 0 {
		return maxPSNR
	}
	return min(10*math.Log10(pixelMax*pixelMax/mse), maxPSNR)
}
