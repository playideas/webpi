package main

import (
	"image"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

// 코어당 처리량 비교 — WEBPI_BENCH_DIR의 JPEG 앞쪽 몇 장을 원본 크기로 인코딩한다 (없으면 건너뜀).
// GOMAXPROCS=1로 돌리면 두 인코더의 행 병렬 여부와 무관하게 코어 하나의 작업량을 잰다.
const (
	speedImages  = 8
	speedQuality = 75
)

// 벤치 함수마다 디렉터리 전체를 다시 읽지 않도록 한 번만 읽는다
var speedSet = sync.OnceValues(func() ([]*image.RGBA, error) {
	imgs, err := loadImages(os.Getenv("WEBPI_BENCH_DIR"), 1)
	return imgs[:min(speedImages, len(imgs))], err
})

func Benchmark_encode_webpi_q75(b *testing.B)     { benchCodec(b, encodeWebpi) }
func Benchmark_encode_deepteams_q75(b *testing.B) { benchCodec(b, encodeDeepteams) }

// 모든 코어에 이미지를 동시에 돌린다 — ns/op가 곧 장당 처리 시간(처리량의 역수)이다
func Benchmark_encodeParallel_webpi_q75(b *testing.B)     { benchCodecParallel(b, encodeWebpi) }
func Benchmark_encodeParallel_deepteams_q75(b *testing.B) { benchCodecParallel(b, encodeDeepteams) }

func benchCodec(b *testing.B, enc func(*image.RGBA, int) ([]byte, error)) {
	imgs := speedImagesOrSkip(b)
	for i := range b.N {
		if _, err := enc(imgs[i%len(imgs)], speedQuality); err != nil {
			b.Fatal(err)
		}
	}
}

func benchCodecParallel(b *testing.B, enc func(*image.RGBA, int) ([]byte, error)) {
	imgs := speedImagesOrSkip(b)
	var next atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := enc(imgs[next.Add(1)%int64(len(imgs))], speedQuality); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func speedImagesOrSkip(b *testing.B) []*image.RGBA {
	if os.Getenv("WEBPI_BENCH_DIR") == "" {
		b.Skip("WEBPI_BENCH_DIR 없음")
	}
	imgs, err := speedSet()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	return imgs
}
