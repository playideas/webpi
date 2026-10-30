package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	deepteams "github.com/deepteams/webp"
	linzeyan "github.com/linzeyan/webp-go"
)

// 인코딩 노력 수준: 비교 대상 WebP 인코더 모두 기본값 4
const webpMethod = 4

type codec struct {
	name   string
	encode func(img *image.RGBA, q int) ([]byte, error)
	decode func(b []byte) (image.Image, error)
}

// builtinCodecs는 이름으로 고를 수 있는 기본 코덱들이다.
// WebP는 모두 libwebp 디코더(dwebp)로 푼다 — x/image/webp는 크로마 업샘플링이 달라 SSIM이 0.989만큼 어긋났다.
func builtinCodecs() map[string]codec {
	return map[string]codec{
		"jpeg":      {"jpeg", encodeJPEG, decodeJPEG},
		"libwebp":   cwebpCodec("libwebp", nil),
		"deepteams": {"deepteams", encodeDeepteams, decodeDwebp},
		"linzeyan":  {"linzeyan", encodeLinzeyan, decodeDwebp},
	}
}

// parseVariants는 "이름=cwebp 인자;이름=인자" 형식을 libwebp 변형 코덱으로 만든다.
// 기능을 하나씩 끈 변형으로 libwebp와 deepteams의 격차가 어디서 나오는지 분해한다.
func parseVariants(spec string) ([]codec, error) {
	var out []codec
	for _, item := range strings.Split(spec, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, args, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("변형 형식 오류 %q (이름=인자)", item)
		}
		out = append(out, cwebpCodec("lw:"+name, strings.Fields(args)))
	}
	return out, nil
}

func encodeJPEG(img *image.RGBA, q int) ([]byte, error) {
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q})
	return buf.Bytes(), err
}

func decodeJPEG(b []byte) (image.Image, error) { return jpeg.Decode(bytes.NewReader(b)) }

func encodeDeepteams(img *image.RGBA, q int) ([]byte, error) {
	var buf bytes.Buffer
	err := deepteams.Encode(&buf, img, &deepteams.EncoderOptions{Quality: float32(q), Method: webpMethod})
	return buf.Bytes(), err
}

func encodeLinzeyan(img *image.RGBA, q int) ([]byte, error) {
	var buf bytes.Buffer
	err := linzeyan.Encode(&buf, img, &linzeyan.Options{Lossy: true, Quality: float32(q), Method: webpMethod})
	return buf.Bytes(), err
}

// cwebpCodec은 libwebp(cwebp CLI)를 부른다. extra는 기본 인자 뒤에 붙어 같은 옵션을 덮어쓴다.
// 프로세스 기동 비용이 시간에 섞이므로 속도 비교에서는 참고만 한다.
func cwebpCodec(name string, extra []string) codec {
	enc := func(img *image.RGBA, q int) ([]byte, error) {
		tmp, err := os.MkdirTemp("", "webpbench")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		in, out := filepath.Join(tmp, "in.png"), filepath.Join(tmp, "out.webp")
		if err := writePNG(in, img); err != nil {
			return nil, err
		}
		args := []string{"-quiet", "-q", fmt.Sprint(q), "-m", fmt.Sprint(webpMethod)}
		args = append(append(args, extra...), in, "-o", out)
		if msg, err := exec.Command("cwebp", args...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cwebp: %w: %s", err, msg)
		}
		return os.ReadFile(out)
	}
	return codec{name, enc, decodeDwebp}
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// decodeDwebp는 libwebp 디코더(dwebp CLI)로 푼다 — 브라우저와 같은 디코더.
func decodeDwebp(b []byte) (image.Image, error) {
	tmp, err := os.MkdirTemp("", "webpdec")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	in, out := filepath.Join(tmp, "in.webp"), filepath.Join(tmp, "out.png")
	if err := os.WriteFile(in, b, 0o600); err != nil {
		return nil, err
	}
	if msg, err := exec.Command("dwebp", "-quiet", in, "-o", out).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("dwebp: %w: %s", err, msg)
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}
