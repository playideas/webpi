package webpi

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// 실제 사진에서의 인코딩 속도 — WEBPI_BENCH_DIR의 JPEG 앞쪽 몇 장을 쓴다 (없으면 건너뜀)
const benchImages = 8

func Benchmark_Encode_photos_q75(b *testing.B) {
	imgs := loadBenchImages(b)
	b.ResetTimer()
	for i := range b.N {
		var buf bytes.Buffer
		if err := Encode(&buf, imgs[i%len(imgs)], &Options{Quality: 75}); err != nil {
			b.Fatal(err)
		}
	}
}

func loadBenchImages(b *testing.B) []image.Image {
	b.Helper()
	dir := os.Getenv("WEBPI_BENCH_DIR")
	if dir == "" {
		b.Skip("WEBPI_BENCH_DIR 없음")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil || len(paths) == 0 {
		b.Skipf("%s 에 JPEG 없음", dir)
	}
	var imgs []image.Image
	for _, p := range paths[:min(benchImages, len(paths))] {
		f, err := os.Open(p)
		if err != nil {
			b.Fatal(err)
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			b.Fatal(err)
		}
		imgs = append(imgs, img)
	}
	return imgs
}
