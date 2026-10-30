// bench는 webpi의 합격 기준을 재는 하네스다 (원본: pocketlabel experiments/webp_bench, 커밋 93d3075).
//
// 질문: 같은 휘도 SSIM에서 각 인코더의 크기가 libwebp 대비 몇 %인가.
// 기준 SSIM은 JPEG q85의 SSIM이다. 품질을 스윕해 곡선을 얻고, 그 SSIM에서의 크기를 보간한다.
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

// 기준 SSIM을 정하는 JPEG 품질 — pocketlabel 축소본의 현재 설정
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
	flag.StringVar(&o.codecs, "codecs", "jpeg,libwebp,deepteams", "기본 코덱 (jpeg,libwebp,deepteams,linzeyan)")
	flag.StringVar(&o.variants, "variants", "", `libwebp 변형 "이름=cwebp 인자;..." 예: "sns0=-sns 0;seg1=-segments 1"`)
	flag.Float64Var(&o.target, "target", 0, "기준 SSIM (0이면 JPEG q85의 SSIM)")
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
	if o.target == 0 {
		ref, err := measure(builtinCodecs()["jpeg"], imgs, refJPEGQuality, o.jobs)
		if err != nil {
			return err
		}
		o.target = ref.ssim
	}
	fmt.Printf("target ssim=%.4f\n", o.target)
	curves := map[string][]point{}
	for _, c := range cs {
		for _, q := range qualities {
			p, err := measure(c, imgs, q, o.jobs)
			if err != nil {
				return fmt.Errorf("%s q%d: %w", c.name, q, err)
			}
			curves[c.name] = append(curves[c.name], p)
			fmt.Printf("%-14s q=%d  avgKB=%7.2f  ssim=%.4f  encMs=%6.1f\n", c.name, q, p.bytes/1024, p.ssim, p.encMs)
		}
	}
	printSummary(cs, curves, o.target)
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

// printSummary는 기준 SSIM에서의 크기와 libwebp 대비 비율을 낸다. 합격선은 +5% 이내.
func printSummary(cs []codec, curves map[string][]point, target float64) {
	base := bytesAtSSIM(curves["libwebp"], target)
	fmt.Printf("\n== ssim %.4f 에서의 크기 ==\n", target)
	for _, c := range cs {
		kb := bytesAtSSIM(curves[c.name], target)
		rel := "   n/a"
		if !math.IsNaN(base) && !math.IsNaN(kb) {
			rel = fmt.Sprintf("%+5.1f%%", (kb/base-1)*100)
		}
		fmt.Printf("%-14s %7.2fKB  vs libwebp %s\n", c.name, kb/1024, rel)
	}
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
				sum.bytes, sum.ssim, sum.encMs = sum.bytes+p.bytes, sum.ssim+p.ssim, sum.encMs+p.encMs
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
	return point{sum.bytes / n, sum.ssim / n, sum.encMs / n}, first
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
	return point{float64(len(b)), lumaSSIM(img, toRGBA(dec)), ms}, nil
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
