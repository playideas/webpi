# webpi

순수 Go(cgo 없음) lossy WebP 인코더. 목표는 **libwebp와 같은 압축 효율**이다.

## 왜 만드나

pocketlabel(단일 바이너리 라벨링 서버)은 cgo 없이 크로스 컴파일해야 해서 libwebp를 못 쓴다.
기존 순수 Go 인코더를 재 보니 (2026-10-31, pocketlabel `experiments/webp_bench`, 커밋 93d3075):

JPEG q85와 같은 화질(휘도 SSIM 0.9705)에서의 크기 — COCO val2017 99장, 0.75배 축소

| 인코더 | 크기 | JPEG 대비 |
|---|---|---|
| Go 표준 JPEG q85 | 38.3KB | — |
| libwebp 1.6.0 (cwebp -m 4) | 약 26.5KB | −31% |
| deepteams/webp v1.2.8 (순수 Go) | 약 32.1KB | −16% |
| linzeyan/webp-go v0.1.2 (순수 Go) | 약 53KB | +39% |

순수 Go 최선(deepteams)과 libwebp 사이에 약 21%의 격차가 있다. **이 격차를 없애는 것이 webpi의 존재 이유다.**

## 합격 기준 (게시 조건)

1. 같은 SSIM에서 크기가 libwebp 대비 +5% 이내 (deepteams는 +21%)
2. 출력은 libwebp `dwebp`로 디코딩 가능해야 한다 (브라우저와 같은 디코더)
3. cgo 없음, 외부 의존 최소, `CGO_ENABLED=0`으로 linux/windows/darwin 빌드
4. 인코딩 속도는 deepteams와 같은 자릿수

## 측정 방법과 이미 밟은 함정

- 하네스 원본: `~/git/pocketlabel/experiments/webp_bench` (품질 스윕 → 같은 SSIM에서 크기 보간)
- **원본이 JPEG면 축소 없이 재압축하지 말 것** — 8×8 격자가 맞아 JPEG가 부당하게 유리해진다
- **WebP는 dwebp로 디코딩할 것** — x/image/webp는 크로마 업샘플링이 달라 SSIM이 0.989만큼 어긋나 WebP 점수가 깎였다
- 휘도 SSIM 하나만 재 왔다. 큰 이미지(2048px)와 다른 지표(예: 크로마 포함, butteraugli 계열)로도 확인할 것

## 참고

- VP8 비트스트림: RFC 6386. 컨테이너: Google WebP Container Specification
- libwebp는 BSD-3. 코드를 옮겨 오면 고지 의무가 생긴다 — 명세 기반 구현이면 불필요

## 기준선과 격차 분해 (2026-10-31, `bench/`)

COCO val2017 150장(`~/datasets/coco_val/images`), 0.75배 축소. 목표 SSIM 0.9728(JPEG q85)에서 잰 크기:
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

실행: `cd bench && go run . -dir <jpg 디렉터리> [-codecs libwebp,deepteams] [-variants "이름=cwebp 인자;..."]`

## 진행 기록

| 단계 | ssim 0.9728 크기 | vs libwebp | 원본 |
|---|---|---|---|
| 골격: i16 예측(SSE 최소), 고정 양자화, 루프 필터 끔, 토큰 확률 갱신 | 34.89KB | +25.5% | `bench/results/webpi-skeleton.txt` |
| + 세그먼트 적응 양자화: DCT 분포로 MB 민감도 → k-means 4 세그먼트, SNS 50으로 세그먼트별 양자화, 색차 DC/AC 델타 | 33.30KB | +19.8% | `bench/results/webpi-segments.txt` |
| + RD 모드 결정과 i4: i16·i4(10모드)·색차를 (왜곡 + 텍스처 왜곡 + λ·비트) 점수로 선택, λ·평탄도 벌점은 libwebp method 4와 같게 | 27.94KB | +0.5% | `bench/results/webpi-rd-i4.txt` |

모든 단계에서 dwebp `-yuv` 결과가 인코더의 복원 평면과 비트 단위로 같다 (`Test_encodeFrame_noLoopFilter_dwebpYUVMatchesReconstructionExactly`).

속도(`-j 1` 직렬, q75 장당): webpi 44.5ms, deepteams 7.5ms, libwebp 37.6ms(cwebp 프로세스 실행 포함). RD·i4 도입으로 webpi가 deepteams의 약 6배 — 합격 기준 4(같은 자릿수) 안이지만 여유가 적다.
