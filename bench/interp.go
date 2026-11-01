package main

import (
	"math"
	"sort"
)

type point struct {
	bytes, encMs float64
	m            [nMetrics]float64
}

// bytesAt은 품질 스윕 곡선에서 지표 k가 target일 때의 크기를 보간한다.
// 크기는 품질 지표에 대해 대략 지수적으로 늘어나므로 log(크기)를 선형 보간한다.
// 목표가 곡선 범위 밖이면 외삽하지 않고 NaN을 낸다 — 외삽값은 비교를 오염시킨다.
func bytesAt(pts []point, k int, target float64) float64 {
	s := append([]point(nil), pts...)
	sort.Slice(s, func(i, j int) bool { return s[i].m[k] < s[j].m[k] })
	for i := 1; i < len(s); i++ {
		lo, hi := s[i-1], s[i]
		if target < lo.m[k] || target > hi.m[k] {
			continue
		}
		if hi.m[k] == lo.m[k] {
			return lo.bytes
		}
		t := (target - lo.m[k]) / (hi.m[k] - lo.m[k])
		return math.Exp(math.Log(lo.bytes) + t*(math.Log(hi.bytes)-math.Log(lo.bytes)))
	}
	return math.NaN()
}

// BD-rate 적분 구간의 표본 수 — 곡선이 9점이라 이 정도면 구간별 선형 보간을 충분히 따라간다
const bdSamples = 64

// bdRate는 두 곡선이 겹치는 품질 구간 전체에서 크기 비율의 기하평균 − 1이다 (Bjøntegaard delta rate).
// 기준값 하나에서의 비교는 곡선의 꺾임에 흔들리므로, 구간 평균을 함께 본다.
func bdRate(base, test []point, k int) float64 {
	lo, hi := math.Max(minMetric(base, k), minMetric(test, k)), math.Min(maxMetric(base, k), maxMetric(test, k))
	if !(lo < hi) {
		return math.NaN()
	}
	var sum float64
	for i := range bdSamples {
		t := lo + (hi-lo)*(float64(i)+0.5)/bdSamples
		sum += math.Log(bytesAt(test, k, t)) - math.Log(bytesAt(base, k, t))
	}
	return math.Exp(sum/bdSamples) - 1
}

func minMetric(pts []point, k int) float64 {
	v := math.Inf(1)
	for _, p := range pts {
		v = math.Min(v, p.m[k])
	}
	return v
}

func maxMetric(pts []point, k int) float64 {
	v := math.Inf(-1)
	for _, p := range pts {
		v = math.Max(v, p.m[k])
	}
	return v
}
