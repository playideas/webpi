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
