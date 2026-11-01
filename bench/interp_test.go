package main

import (
	"image"
	"math"
	"testing"
)

func pt(bytes, ssim float64) point {
	p := point{bytes: bytes}
	p.m[metricSSIMY] = ssim
	return p
}

func Test_bytesAt_betweenPoints_interpolatesInLogSpace(t *testing.T) {
	pts := []point{pt(4000, 0.98), pt(1000, 0.90)}
	got := bytesAt(pts, metricSSIMY, 0.94)
	// 중간점이면 기하평균 2000
	if math.Abs(got-2000) > 1e-6 {
		t.Fatalf("got %v, want 2000", got)
	}
}

func Test_bytesAt_onPoint_returnsThatSize(t *testing.T) {
	pts := []point{pt(1000, 0.90), pt(3000, 0.95), pt(9000, 0.99)}
	if got := bytesAt(pts, metricSSIMY, 0.95); math.Abs(got-3000) > 1e-6 {
		t.Fatalf("got %v, want 3000", got)
	}
}

func Test_bytesAt_outOfRange_returnsNaN(t *testing.T) {
	pts := []point{pt(1000, 0.90), pt(3000, 0.95)}
	if got := bytesAt(pts, metricSSIMY, 0.97); !math.IsNaN(got) {
		t.Fatalf("got %v, want NaN", got)
	}
}

func Test_allMetrics_identicalImages_returnsPerfectScores(t *testing.T) {
	img := testImage()
	m := allMetrics(img, img)
	if m[metricSSIMY] != 1 || m[metricSSIMUV] != 1 || m[metricPSNRYUV] != maxPSNR || m[metricPSNRRGB] != maxPSNR {
		t.Fatalf("got %v, want perfect", m)
	}
}

func Test_allMetrics_chromaOnlyChange_lowersChromaNotLuma(t *testing.T) {
	a, b := testImage(), testImage()
	// 휘도를 거의 유지하면서 색만 바꾼다: R을 올리고 G를 내린다 (0.299·Δ ≈ 0.587·Δ/2)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i] = uint8(min(int(b.Pix[i])+20, 255))
		b.Pix[i+1] = uint8(max(int(b.Pix[i+1])-10, 0))
	}
	m := allMetrics(a, b)
	if m[metricSSIMUV] >= m[metricSSIMY] {
		t.Fatalf("chroma ssim %v should be below luma ssim %v", m[metricSSIMUV], m[metricSSIMY])
	}
}

func testImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		k := i / 4
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(k*7), uint8(k*3+60), uint8(k*5+30), 255
	}
	return img
}

func Test_bdRate_uniformlyLargerCurve_returnsThatRatio(t *testing.T) {
	base := []point{pt(1000, 0.90), pt(2000, 0.95), pt(4000, 0.99)}
	test := []point{pt(1100, 0.90), pt(2200, 0.95), pt(4400, 0.99)}
	if got := bdRate(base, test, metricSSIMY); math.Abs(got-0.1) > 1e-9 {
		t.Fatalf("got %v, want 0.1", got)
	}
}

func Test_bdRate_disjointRanges_returnsNaN(t *testing.T) {
	base := []point{pt(1000, 0.90), pt(2000, 0.92)}
	test := []point{pt(1000, 0.95), pt(2000, 0.97)}
	if got := bdRate(base, test, metricSSIMY); !math.IsNaN(got) {
		t.Fatalf("got %v, want NaN", got)
	}
}
