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
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
)

// 품질 설정 스윕 — 변형마다 기준 SSIM을 감싸는 곡선이 필요해 넓게 잡는다
var qualities = []int{40, 50, 60, 70, 75, 80, 85, 90, 95}

type options struct {
	dir, codecs, variants string
	qualities, ref        string
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
	flag.Float64Var(&o.target, "target", 0, "휘도 SSIM 기준값 (0이면 -ref의 값) — 다른 지표는 항상 -ref 기준")
	flag.StringVar(&o.qualities, "q", "", "스윕할 품질 목록 (쉼표 구분, 비우면 기본 목록)")
	// 높은 품질 구간을 볼 때는 예: -ref libwebp:95 (libwebp q95와 같은 화질에서의 크기)
	flag.StringVar(&o.ref, "ref", "jpeg:85", "지표 기준값을 정하는 코덱:품질 (쉼표로 여러 개)")
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
	fmt.Printf("images=%d scale=%.2f jobs=%d ref=%s\n", len(imgs), o.scale, o.jobs, o.ref)
	qs, err := parseQualities(o.qualities)
	if err != nil {
		return err
	}
	refs, err := parseRefs(o.ref)
	if err != nil {
		return err
	}
	targets, err := refTargets(refs, imgs, o)
	if err != nil {
		return err
	}
	curves, err := sweep(cs, imgs, qs, o.jobs)
	if err != nil {
		return err
	}
	for i, t := range targets {
		fmt.Printf("\n## 기준 %s: %s\n", refs[i].label, formatMetrics(t))
		printSummary(cs, curves, t)
	}
	printBDRates(cs, curves)
	return nil
}

// refPoint는 지표 기준값을 정하는 코덱과 품질이다.
type refPoint struct {
	label string
	c     codec
	q     int
}

// refTargets는 기준마다 지표 값을 잰다. -target이 있으면 휘도 SSIM만 그 값으로 바꾼다.
func refTargets(refs []refPoint, imgs []*image.RGBA, o options) ([][nMetrics]float64, error) {
	var out [][nMetrics]float64
	for _, r := range refs {
		p, err := measure(r.c, imgs, r.q, o.jobs)
		if err != nil {
			return nil, err
		}
		if o.target != 0 {
			p.m[metricSSIMY] = o.target
		}
		out = append(out, p.m)
	}
	return out, nil
}

// sweep은 코덱마다 품질 목록을 돌며 곡선을 잰다.
func sweep(cs []codec, imgs []*image.RGBA, qs []int, jobs int) (map[string][]point, error) {
	curves := map[string][]point{}
	for _, c := range cs {
		for _, q := range qs {
			p, err := measure(c, imgs, q, jobs)
			if err != nil {
				return nil, fmt.Errorf("%s q%d: %w", c.name, q, err)
			}
			curves[c.name] = append(curves[c.name], p)
			fmt.Printf("%-14s q=%d  avgKB=%7.2f  %s  encMs=%6.1f\n", c.name, q, p.bytes/1024, formatMetrics(p.m), p.encMs)
		}
	}
	return curves, nil
}

// parseQualities는 쉼표로 구분한 품질 목록을 읽는다. 비어 있으면 기본 목록이다.
func parseQualities(list string) ([]int, error) {
	if list == "" {
		return qualities, nil
	}
	var qs []int
	for _, f := range strings.Split(list, ",") {
		q, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil, fmt.Errorf("품질 %q: %w", f, err)
		}
		qs = append(qs, q)
	}
	return qs, nil
}

// parseRefs는 쉼표로 구분한 "코덱:품질" 기준 목록을 읽는다.
func parseRefs(list string) ([]refPoint, error) {
	var refs []refPoint
	for _, ref := range strings.Split(list, ",") {
		ref = strings.TrimSpace(ref)
		name, qs, ok := strings.Cut(ref, ":")
		c, known := builtinCodecs()[name]
		q, err := strconv.Atoi(qs)
		if !ok || !known || err != nil {
			return nil, fmt.Errorf("기준 %q는 코덱:품질 형식이어야 한다", ref)
		}
		refs = append(refs, refPoint{ref, c, q})
	}
	return refs, nil
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
