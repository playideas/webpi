package main

import (
	"math"
	"sort"
)

type point struct {
	bytes, ssim, encMs float64
}

// bytesAtSSIM은 품질 스윕 곡선에서 목표 SSIM일 때의 크기를 보간한다.
// 크기는 SSIM에 대해 대략 지수적으로 늘어나므로 log(크기)를 선형 보간한다.
// 목표가 곡선 범위 밖이면 외삽하지 않고 NaN을 낸다 — 외삽값은 비교를 오염시킨다.
func bytesAtSSIM(pts []point, target float64) float64 {
	s := append([]point(nil), pts...)
	sort.Slice(s, func(i, j int) bool { return s[i].ssim < s[j].ssim })
	for i := 1; i < len(s); i++ {
		lo, hi := s[i-1], s[i]
		if target < lo.ssim || target > hi.ssim {
			continue
		}
		if hi.ssim == lo.ssim {
			return lo.bytes
		}
		t := (target - lo.ssim) / (hi.ssim - lo.ssim)
		return math.Exp(math.Log(lo.bytes) + t*(math.Log(hi.bytes)-math.Log(lo.bytes)))
	}
	return math.NaN()
}
