package main

import (
	"math"
	"testing"
)

func Test_bytesAtSSIM_betweenPoints_interpolatesInLogSpace(t *testing.T) {
	pts := []point{{bytes: 4000, ssim: 0.98}, {bytes: 1000, ssim: 0.90}}
	got := bytesAtSSIM(pts, 0.94)
	// 중간점이면 기하평균 2000
	if math.Abs(got-2000) > 1e-6 {
		t.Fatalf("got %v, want 2000", got)
	}
}

func Test_bytesAtSSIM_onPoint_returnsThatSize(t *testing.T) {
	pts := []point{{bytes: 1000, ssim: 0.90}, {bytes: 3000, ssim: 0.95}, {bytes: 9000, ssim: 0.99}}
	if got := bytesAtSSIM(pts, 0.95); math.Abs(got-3000) > 1e-6 {
		t.Fatalf("got %v, want 3000", got)
	}
}

func Test_bytesAtSSIM_outOfRange_returnsNaN(t *testing.T) {
	pts := []point{{bytes: 1000, ssim: 0.90}, {bytes: 3000, ssim: 0.95}}
	if got := bytesAtSSIM(pts, 0.97); !math.IsNaN(got) {
		t.Fatalf("got %v, want NaN", got)
	}
}
