// bench는 webpi의 합격 기준을 재는 하네스다.
//
// 질문: 같은 품질에서 각 인코더의 크기가 libwebp 대비 몇 %인가. 품질은 여러 지표로 따로 잰다(metrics.go).
// 지표별 기준값은 JPEG q85의 값이다. 품질을 스윕해 곡선을 얻고, 그 기준값에서의 크기를 보간한다.
// libwebp 변형(-variants)으로 기능을 하나씩 끄면 격차가 어느 기능에서 나오는지 분해할 수 있다.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
)

// 품질 설정 스윕 — 변형마다 기준 SSIM을 감싸는 곡선이 필요해 넓게 잡는다
var qualities = []int{40, 50, 60, 70, 75, 80, 85, 90, 95}

// 기준 SSIM을 정하는 JPEG 품질 — 비교 기준으로 삼은 JPEG 설정
const refJPEGQuality = 85

type options struct {
	dir, codecs, variants string
	scale, target         float64
	jobs                  int
}

func main() {
	var o options
	flag.StringVar(&o.dir, "dir", "", "JPEG 원본 디렉터리")
	// 원본이 이미 JPEG라 그대로 재압축하면 8×8 격자가 맞아 JPEG가 부당하게 유리하다 — 축소해서 격자를 깬다
	flag.Float64Var(&o.scale, "scale", 0.75, "인코딩 전 축소 비율 (1이면 축소 안 함)")
	flag.StringVar(&o.codecs, "codecs", "jpeg,libwebp,deepteams", "기본 코덱 (jpeg,libwebp,deepteams,deepteams-zero,linzeyan,webpi)")
	flag.StringVar(&o.variants, "variants", "", `libwebp 변형 "이름=cwebp 인자;..." 예: "sns0=-sns 0;seg1=-segments 1"`)
	flag.Float64Var(&o.target, "target", 0, "휘도 SSIM 기준값 (0이면 JPEG q85의 값) — 다른 지표는 항상 JPEG q85 기준")
	// 병렬이면 인코딩 시간이 서로 간섭한다 — 속도를 볼 때는 -j 1
	flag.IntVar(&o.jobs, "j", runtime.NumCPU(), "동시 처리 이미지 수")
	flag.Parse()
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(o options) error {
	cs, err := selectCodecs(o.codecs, o.variants)
	if err != nil {
		return err
	}
	imgs, err := loadImages(o.dir, o.scale)
	if err != nil {
		return err
	}
	fmt.Printf("images=%d scale=%.2f jobs=%d\n", len(imgs), o.scale, o.jobs)
	ref, err := measure(builtinCodecs()["jpeg"], imgs, refJPEGQuality, o.jobs)
	if err != nil {
		return err
	}
	targets := ref.m
	if o.target != 0 {
		targets[metricSSIMY] = o.target
	}
	fmt.Printf("targets %s\n", formatMetrics(targets))
	curves := map[string][]point{}
	for _, c := range cs {
		for _, q := range qualities {
			p, err := measure(c, imgs, q, o.jobs)
			if err != nil {
				return fmt.Errorf("%s q%d: %w", c.name, q, err)
			}
			curves[c.name] = append(curves[c.name], p)
			fmt.Printf("%-14s q=%d  avgKB=%7.2f  %s  encMs=%6.1f\n", c.name, q, p.bytes/1024, formatMetrics(p.m), p.encMs)
		}
	}
	printSummary(cs, curves, targets)
	return nil
}

func selectCodecs(names, variants string) ([]codec, error) {
	all := builtinCodecs()
	var cs []codec
	for _, n := range strings.Split(names, ",") {
		c, ok := all[strings.TrimSpace(n)]
		if !ok {
			return nil, fmt.Errorf("모르는 코덱 %q", n)
		}
		cs = append(cs, c)
	}
	vs, err := parseVariants(variants)
	return append(cs, vs...), err
}

// printSummary는 지표별 기준값에서의 크기를 libwebp 대비 비율로 낸다. 합격선은 +5% 이내.
func printSummary(cs []codec, curves map[string][]point, targets [nMetrics]float64) {
	fmt.Printf("\n== 지표별 기준값에서의 크기 (vs libwebp) ==\n%-14s", "codec")
	for _, n := range metricNames {
		fmt.Printf(" %9s", n)
	}
	fmt.Println()
	for _, c := range cs {
		fmt.Printf("%-14s", c.name)
		for k := range nMetrics {
			base, kb := bytesAt(curves["libwebp"], k, targets[k]), bytesAt(curves[c.name], k, targets[k])
			if math.IsNaN(base) || math.IsNaN(kb) {
				fmt.Printf(" %9s", "n/a")
				continue
			}
			fmt.Printf(" %+8.1f%%", (kb/base-1)*100)
		}
		fmt.Println()
	}
	printBDRates(cs, curves)
}

// printBDRates는 겹치는 품질 구간 전체의 평균 크기 차이(BD-rate)를 libwebp 대비로 낸다.
func printBDRates(cs []codec, curves map[string][]point) {
	fmt.Printf("\n== BD-rate (겹치는 구간 평균, vs libwebp) ==\n")
	for _, c := range cs {
		fmt.Printf("%-14s", c.name)
		for k := range nMetrics {
			fmt.Printf(" %+8.1f%%", bdRate(curves["libwebp"], curves[c.name], k)*100)
		}
		fmt.Println()
	}
}

func formatMetrics(m [nMetrics]float64) string {
	return fmt.Sprintf("ssimY=%.4f ssimUV=%.4f psnrYUV=%.2f psnrRGB=%.2f",
		m[metricSSIMY], m[metricSSIMUV], m[metricPSNRYUV], m[metricPSNRRGB])
}

// measure는 한 코덱·품질에서 이미지 평균 크기·SSIM·인코딩 시간을 낸다.
func measure(c codec, imgs []*image.RGBA, q, jobs int) (point, error) {
	var (
		mu    sync.Mutex
		sum   point
		first error
		wg    sync.WaitGroup
	)
	work := make(chan *image.RGBA)
	for range max(jobs, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for img := range work {
				p, err := measureOne(c, img, q)
				mu.Lock()
				if err != nil && first == nil {
					first = err
				}
				sum.bytes, sum.encMs = sum.bytes+p.bytes, sum.encMs+p.encMs
				for k := range nMetrics {
					sum.m[k] += p.m[k]
				}
				mu.Unlock()
			}
		}()
	}
	for _, img := range imgs {
		work <- img
	}
	close(work)
	wg.Wait()
	n := float64(len(imgs))
	sum.bytes, sum.encMs = sum.bytes/n, sum.encMs/n
	for k := range nMetrics {
		sum.m[k] /= n
	}
	return sum, first
}

func measureOne(c codec, img *image.RGBA, q int) (point, error) {
	start := time.Now()
	b, err := c.encode(img, q)
	if err != nil {
		return point{}, err
	}
	ms := float64(time.Since(start).Microseconds()) / 1000
	dec, err := c.decode(b)
	if err != nil {
		return point{}, fmt.Errorf("decode: %w", err)
	}
	return point{bytes: float64(len(b)), encMs: ms, m: allMetrics(img, toRGBA(dec))}, nil
}

func loadImages(dir string, scale float64) ([]*image.RGBA, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s 에 .jpg 없음", dir)
	}
	sort.Strings(paths)
	var imgs []*image.RGBA
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		imgs = append(imgs, resize(toRGBA(img), scale))
	}
	return imgs, nil
}

// resize는 CatmullRom으로 줄인다 — 서버 축소본 생성과 같은 종류의 필터.
func resize(img *image.RGBA, scale float64) *image.RGBA {
	if scale == 1 {
		return img
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}
