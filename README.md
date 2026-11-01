# webpi

cgo 없는 Go lossy WebP 인코더. 목표는 **libwebp와 같은 압축 효율**이다.

- 같은 화질에서 크기가 libwebp와 거의 같다 (휘도 SSIM 기준 +2.4%, RGB PSNR 기준 −1.4%). 기존 순수 Go 인코더는 +14~22%
- arm64에서는 NEON 어셈블리 커널을 기본으로 쓴다. `-tags purego`면 모든 아키텍처에서 순수 Go로 빌드되고, 두 빌드의 출력은 바이트 단위로 같다
- 알파 채널은 libwebp처럼 VP8L 무손실로 압축해 함께 담는다
- 출력은 libwebp `dwebp`(브라우저와 같은 디코더)로 디코딩해 검증한다

## 설치

```sh
go get github.com/playideas/webpi@latest
```

Go 1.25 이상. 외부 의존이 없다.

## 사용

```go
package main

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"

	"github.com/playideas/webpi"
)

func main() {
	in, err := os.Open("in.png")
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()
	img, _, err := image.Decode(in)
	if err != nil {
		log.Fatal(err)
	}
	out, err := os.Create("out.webp")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := webpi.Encode(out, img, &webpi.Options{Quality: 80}); err != nil {
		log.Fatal(err)
	}
}
```

- `Quality`는 0~100이고 cwebp `-q`와 같은 곡선을 쓴다. `Options`가 `nil`이면 75다 (`&webpi.Options{}`는 품질 0이다)
- 어떤 `image.Image`든 받는다. `*image.YCbCr`(JPEG), `*image.NRGBA`, `*image.RGBA`는 픽셀을 직접 읽어 빠르다
- 투명 픽셀이 하나라도 있으면 알파를 담은 확장 형식(`VP8X` + `ALPH` + `VP8 `)으로, 모두 불투명하면 단순 형식(`VP8 ` 청크 하나)으로 쓴다
- `Encode`는 동시에 여러 고루틴에서 불러도 안전하고, 한 장도 매크로블록 행 단위로 여러 코어에 나눠 인코딩한다.
  출력은 코어 수와 상관없이 같다
- 인코딩 도중 패닉이 나면 프로세스를 죽이지 않고 `*webpi.PanicError`를 오류로 돌려준다. 내부 작업 고루틴에서 난 패닉과,
  넘겨받은 이미지의 `At`이나 `io.Writer`에서 난 패닉이 모두 해당한다.
  오류 문자열에는 패닉 값만 담기고, 스택은 `errors.As`로 꺼낸 `PanicError`의 `Stack` 필드에 있다

### 지원하지 않는 것

- 색의 무손실(VP8L) 인코딩 — 무손실 압축은 알파 평면에만 쓴다
- 애니메이션, ICC·EXIF·XMP 메타데이터
- 디코딩 — `golang.org/x/image/webp`를 쓰면 된다
- 품질 외의 세부 옵션 (method, segments, sns 등은 libwebp `-m 4` 기본값에 맞춰 고정)
- 투명한 픽셀의 색 보존 — libwebp 기본 동작처럼 보이지 않는 픽셀의 색은 평탄하게 바꾼다 (cwebp `-exact`에 해당하는 옵션 없음)

## 빌드

| 대상 | 명령 | 커널 |
|---|---|---|
| arm64 (기본) | `go build` | NEON 어셈블리 |
| arm64, 순수 Go | `go build -tags purego` | Go |
| 그 밖의 아키텍처 | `go build` | Go |

cgo를 쓰지 않으므로 `CGO_ENABLED=0`으로 linux·windows·darwin 어디로든 크로스 컴파일된다.

## 성능

COCO val2017 사진, 0.75배 축소, JPEG q85와 같은 화질에서 libwebp 1.6.0(`cwebp -m 4`) 대비 크기 차:

| 인코더 | 휘도 SSIM | 색차 SSIM | YUV PSNR | RGB PSNR |
|---|---|---|---|---|
| **webpi** | **+2.4%** | **−9.8%** | **+1.0%** | **−1.4%** |
| deepteams/webp (순수 Go, 기본값) | +14.1% | −8.9% | +6.5% | +3.8% |

640×480 사진 한 장, q75 인코딩 시간 (Apple M4 Max):

| | webpi arm64 NEON | webpi 순수 Go | deepteams/webp |
|---|---|---|---|
| 코어 하나 | ~25.8ms | ~41.7ms | ~47.3ms |
| 모든 코어 동시(처리량) | ~2.1ms | ~3.3ms | ~3.4ms |

알파: DIS5K 컷아웃 12장(장당 약 1000만 픽셀)에서 ALPH 청크 크기가 libwebp의 1.006~1.011배다.

측정 방법과 단계별 기록은 아래 [개발 기록](#개발-기록)에 있다.

## 테스트

```sh
go test ./...                  # 기본 빌드
go test -tags purego ./...     # 순수 Go 빌드
cd bench && go test ./...      # 디코더 왕복(x/image/webp, dwebp)
```

PATH에 libwebp의 `dwebp`가 있으면 libwebp 디코더 대조도 같이 돈다:
- VP8: dwebp의 필터 없는 YUV 출력이 인코더의 복원 평면과 비트 단위로 같다
- 알파: 두 디코더 모두에서 원본과 정확히 같다

## 개발 기록

### 왜 만들었나

cgo 없이 크로스 컴파일해야 하는 단일 바이너리 서버에서는 libwebp를 쓸 수 없다.
기존 순수 Go 인코더를 재 보니 (2026-10-31):

JPEG q85와 같은 화질(휘도 SSIM 0.9705)에서의 크기 — COCO val2017 99장, 0.75배 축소

| 인코더 | 크기 | JPEG 대비 |
|---|---|---|
| Go 표준 JPEG q85 | 38.3KB | — |
| libwebp 1.6.0 (cwebp -m 4) | 약 26.5KB | −31% |
| deepteams/webp v1.2.8 (순수 Go) | 약 32.1KB | −16% |
| linzeyan/webp-go v0.1.2 (순수 Go) | 약 53KB | +39% |

순수 Go 최선(deepteams)과 libwebp 사이에 약 21%의 격차가 있었고, 이 격차를 없애려고 만들었다. 처음에 정한 합격 기준과 현재 상태:

| 기준 | 상태 |
|---|---|
| 같은 SSIM에서 크기가 libwebp 대비 +5% 이내 | 충족 (+2.4%) |
| libwebp `dwebp`로 디코딩 가능 | 충족 (비트 단위 대조 테스트) |
| cgo 없음, 외부 의존 최소, `CGO_ENABLED=0`으로 linux·windows·darwin 빌드 | 충족 (외부 의존 없음) |
| 인코딩 속도가 deepteams와 같은 자릿수 | 충족 (더 빠름) |

### 측정 방법과 이미 밟은 함정

- 하네스: `bench/` (품질 스윕 → 같은 SSIM에서 크기 보간)
- **원본이 JPEG면 축소 없이 재압축하지 말 것** — 8×8 격자가 맞아 JPEG가 부당하게 유리해진다
- **WebP는 dwebp로 디코딩할 것** — x/image/webp는 크로마 업샘플링이 달라 SSIM이 0.989만큼 어긋나 WebP 점수가 깎였다
- 처음에는 휘도 SSIM 하나만 쟀다. 지금은 지표 넷으로 잰다 ([다중 지표 검증](#다중-지표-검증-2026-10-31))

### 기준선과 격차 분해 (2026-10-31, `bench/`)

COCO val2017 150장, 0.75배 축소. 목표 SSIM 0.9728(JPEG q85)에서 잰 크기:
libwebp 27.79KB, deepteams 34.00KB(**+22.3%**), JPEG 39.09KB. 원 측정의 +21%가 재현됐다.
원본: `bench/results/baseline.txt`

libwebp 기능을 하나씩 끄거나 바꿔 잰 결과(libwebp 기본 `-m 4` 대비, 원본: `bench/results/ablation.txt`):

| 변형 | 크기 차 | 해석 |
|---|---|---|
| `-m 3` / `-m 6` | +0.1% / +0.1% | trellis(m5·m6)는 휘도 SSIM 기준 이득이 없다 (m5는 +1.5%) |
| `-m 2` | +7.1% | m2→m3 사이(libwebp는 m≥3에서 RD 기반 모드 결정 시작)가 격차의 한 축 |
| `-m 1` / `-m 0` | +18.7% / +25.4% | |
| `-segments 1` | +10.7% | 세그먼트별 양자화가 단일 기능으로 가장 크다 |
| `-sns 0` | +7.1% | 적응 양자화를 끄면 세그먼트 효과가 대부분 사라진다 |
| `-sns 0 -segments 1` | +7.4% | |
| `-m 2 -sns 0 -segments 1` | **+25.8%** | deepteams(+22.3%)와 비슷한 수준 |
| `-f 0`, `-nostrong` | +0.8%, +1.2% | 루프 필터 세부는 작다 |
| `-af` | −1.4% | 자동 필터 강도는 소폭 이득 |
| `-sharp_yuv` | +5.6% | 휘도 SSIM으로는 손해 |

결론: 합격선(+5%)에는 **세그먼트 적응 양자화(SNS)** 와 **RD 기반 모드 결정** 둘 다 필요하다
(각각 하나만 빠져도 +7%, 둘 다 빠지면 +26%). trellis는 우선순위에서 뺀다.
단, 이 결론은 휘도 SSIM 하나에 기대고 있다. trellis처럼 SSE를 최적화하는 기능은 다른 지표에서 다르게 나올 수 있다.

실행: `cd bench && go run . -dir <jpg 디렉터리> [-codecs libwebp,deepteams,deepteams-zero,webpi] [-variants "이름=cwebp 인자;..."]`

### 진행 기록

| 단계 | ssim 0.9728 크기 | vs libwebp | 원본 |
|---|---|---|---|
| 골격: i16 예측(SSE 최소), 고정 양자화, 루프 필터 끔, 토큰 확률 갱신 | 34.89KB | +25.5% | `bench/results/webpi-skeleton.txt` |
| + 세그먼트 적응 양자화: DCT 분포로 MB 민감도 → k-means 4 세그먼트, SNS 50으로 세그먼트별 양자화, 색차 DC/AC 델타 | 33.30KB | +19.8% | `bench/results/webpi-segments.txt` |
| + RD 모드 결정과 i4: i16·i4(10모드)·색차를 (왜곡 + 텍스처 왜곡 + λ·비트) 점수로 선택, λ·평탄도 벌점은 libwebp method 4와 같게 | 27.94KB | +0.5% | `bench/results/webpi-rd-i4.txt` |

모든 단계에서 dwebp `-nofilter -yuv` 결과가 인코더의 복원 평면과 비트 단위로 같다 (`Test_encodeFrame_dwebpUnfilteredYUV_matchesReconstructionExactly`).

#### 다중 지표 검증 (2026-10-31)

휘도 SSIM 하나로는 색차를 굶기는 인코더를 가려낼 수 없어서 지표를 넷으로 늘렸다:
휘도 SSIM, 색차(Cb·Cr) SSIM, (6·Y+Cb+Cr)/8 가중 PSNR, RGB PSNR.
각 지표의 JPEG q85 값에서의 크기와, 곡선이 겹치는 구간 전체의 BD-rate를 libwebp 대비로 낸다.

기존 하네스는 deepteams 옵션을 구조체 리터럴로 만들어 SNS·루프 필터·세그먼트가 모두 꺼진 채였다(`deepteams-zero`).
지금은 `DefaultOptions()`(libwebp 기본값)로 부른다. 이전 표의 +22.3%는 `deepteams-zero`의 값이다.

| 코덱 | ssim-y | ssim-uv | psnr-yuv | psnr-rgb |
|---|---|---|---|---|
| deepteams (기본값) | +14.1% / +16.6% | −8.9% / −9.1% | +6.5% / +6.5% | +3.8% / +3.4% |
| deepteams-zero | +22.3% / +23.5% | −8.6% / −7.6% | +2.7% / +2.3% | +0.8% / +1.3% |
| **webpi** | **+2.4% / +1.9%** | **−9.8% / −10.6%** | **+1.0% / +2.1%** | **−1.4% / −0.9%** |

(기준점 값 / BD-rate, libwebp 대비 크기 차. 원본: `bench/results/webpi-multimetric.txt`)
webpi가 두 deepteams 설정 모두를 네 지표·두 측정 방식 전부에서 앞선다.
가장 얇은 곳은 psnr-yuv BD-rate의 deepteams-zero(+2.3%)와의 차이다. (i4 후보 축소 전 값은 +2.2/+1.8, −9.7/−10.4, +0.8/+1.9, −1.5/−1.1)

이 단계에서 바뀐 것:
- 루프 필터: libwebp와 같은 세그먼트별 강도(세기 60) — 모든 지표 소폭 이득
- 색차 2×2 평균을 감마 보정 공간에서 (libwebp와 같음)
- 색차 AC 양자화 −3단계, λ_uv를 libwebp의 1/4로: 색차가 휘도 기준 세그먼트 양자화를 따라가며 질감 많은 영역에서 과하게 깎이던 것을 보정.
  휘도 지표를 ~2% 내주고 색차 SSIM ~6%, RGB PSNR ~1%를 얻는다 (스윕 비교 후 선택)
- 시도했지만 뺀 것: libwebp 주파수 샤프닝(모든 지표 악화), 2패스 확률(휘도 0.4% 이득에 시간 2배)

#### 속도

| | 시작 | 순수 Go (`-tags purego`) | arm64 NEON (기본) | deepteams 기본값 |
|---|---|---|---|---|
| 코어 하나(`GOMAXPROCS=1`), 640×480 장당 q75 | ~50ms | ~41.7ms | ~25.8ms | ~47.3ms |
| 모든 코어에 동시(처리량), 장당 q75 | 3.91ms | ~3.3ms | ~2.1ms | ~3.4ms |

(`cd bench && WEBPI_BENCH_DIR=<jpg 디렉터리> go test -bench 'encode'`의 `encode_*`·`encodeParallel_*`.
다른 작업이 도는 머신이라 두 인코더를 번갈아 돌려 같은 시간대의 비율로 비교했다. deepteams 대비 비율: 순수 Go는 코어 하나 0.88·동시 0.96~1.00, NEON은 코어 하나 0.55·동시 0.60~0.65)
- 비용 표: 토큰 트리를 따라가는 대신 libwebp식 레벨 비용 표로 RD 비트 수를 낸다
- 매크로블록 행을 대각선 물결로 동시에 인코딩한다. 순차 인코딩과 바이트 단위로 같다 (`Test_Encode_parallelRows_matchesSingleThreadBytes`).
  deepteams는 스레드 수에 따라 출력이 달라진다
- 블록마다 힙 할당 제거, 부분 점수로 확실히 지는 후보 조기 종료(결과 불변)
- i4는 블록마다 추정 점수 λ·(모드 비용 + SATD/q) 상위 5개만 끝까지 계산한다 (deepteams는 예측 SSE 상위 3개).
  COCO 16장에서 블록별 모든 모드의 실제 RD 점수를 기록해 추정식을 비교했다: 승자를 놓쳐 잃는 RD 점수가
  SSE 기준 상위 5개 0.50%, SATD/q 기준 0.20%. 전체 지표 손실은 0.1~0.2%다
- 행 결과는 모드 헤더만 프레임 배열에 두고 계수는 행별 int16 버퍼에 마지막 0 아닌 계수까지만 쌓는다.
  RGB 행·토큰 통계는 작업자별로 한 벌 — 인코딩당 할당 3.6MB·1125회 → 2.1MB·226회
- 예측은 모드 분기를 블록마다 한 번만 하고 행 단위로 채운다. 평탄 블록 판정은 분기 없이 센다
- arm64 NEON 커널: 4×4 블록 처리(잔차→DCT→양자화→역양자화→역 DCT→복원→SSE를 한 번에, i4·색차 공용),
  i16 휘도의 블록 DCT와 DC 대체 양자화·복원, i4 후보 순위용 SATD, 분석 히스토그램용 DCT 칸 계산,
  텍스처 왜곡용 가중 하다마드. 남은 시간의 대부분은 비용 표 조회·비트 기록처럼 순차 의존이 있는 엔트로피 단계다.
  Go 어셈블러에 정수 NEON 명령이 없어 `internal/asmgen/gen.py`가 clang으로 어셈블한 기계어를 `WORD`로 넣은
  `kernels_arm64.s`를 만든다(생성 파일을 커밋하므로 빌드에는 clang이 필요 없다). Go 구현과의 비트 일치는
  `go test ./...`의 무작위 대조 테스트로 확인한다. deepteams는 arm64에서 예측·SSE·역 DCT가 NEON 어셈블리다

#### 알파 채널 (2026-11-01)

알파가 있으면 `VP8X` → `ALPH`(VP8L 무손실) → `VP8 ` 순서로 쓴다. 모두 불투명이면 지금처럼 `VP8 ` 청크 하나다.

- VP8L 인코더(알파 전용): 길이 제한 허프만, LZ77(2차원 거리 부호 포함), 색 인덱싱 변환과 픽셀 묶기.
  색 캐시·메타 접두 부호는 없다
- 값이 16개 이하(이진 마스크 등)면 팔레트로 한 바이트에 2·4·8픽셀을 묶고, 그보다 많으면 ALPH 필터
  (없음·가로·세로·기울기) 중 잔차 엔트로피가 가장 낮은 것을 쓴다
- LZ77은 탐욕 분할로 기호 비용을 추정한 뒤 비용 기반 최적 분할을 3번 반복한다 (가까운 2차원 거리 16개 + 해시 사슬)
- 최적 분할은 픽셀마다 12바이트 표를 쓰므로, 묶은 뒤 2²⁴(약 1680만) 픽셀이 넘는 알파 평면은 탐욕 분할만 한다.
  8192² 반투명 합성 마스크 기준 최대 메모리 3.1GB → 1.1GB, 시간 3.5초 → 1.2초, ALPH는 2.5% 커진다.
  이진 마스크는 8픽셀씩 묶이므로 약 1억 3천만 픽셀까지 최적 분할을 한다
- 투명 영역 정리: libwebp처럼 8×8 블록마다 투명 픽셀의 색을 평탄하게 바꿔 VP8이 보이지 않는 내용에 비트를 쓰지 않게 한다
- `*image.RGBA`(알파가 곱해진 색)는 반투명 픽셀의 색을 되돌려 인코딩한다

DIS5K 12장(사진 + 전경 마스크로 만든 컷아웃, 장당 약 1000만 픽셀), q75, libwebp 1.6.0 `cwebp` 기본 설정 대비:

| | ALPH 청크 크기 | 파일 전체 크기 |
|---|---|---|
| 이진 마스크 | 1.011배 | 0.939배 |
| 흐린 마스크(상자 흐림 반지름 2, 반투명 경계) | 1.006배 | — |

12장 인코딩(VP8 포함)에 약 1초가 걸린다. 알파는 무손실이라 x/image/webp와 dwebp 둘 다에서 원본과 정확히 같은지
`bench/alpha_test.go`로 확인한다. 묶지 않은 원본을 2차원 거리로 복사하면 일부 이진 마스크에서 ALPH가 10% 더
작았지만 픽셀이 8배라 시간이 몇 배 들어 쓰지 않았다.
파일 전체 크기는 두 인코더의 q75가 같은 화질이 아니라 참고용이다

## 참고

- VP8 비트스트림: RFC 6386. 컨테이너와 VP8L: Google WebP Container Specification, WebP Lossless Bitstream Format
- 일부 절차와 상수(분석·세그먼트, 양자화 바이어스, 레벨 비용 표, RD 가중치, 투명 영역 정리 등)는 libwebp를 따랐다.
  libwebp의 BSD-3 고지는 [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES)에 있다

## 라이선스

MIT — [LICENSE](LICENSE). libwebp에서 온 부분의 고지는 [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES)
