package webpi

import (
	"bytes"
	"os/exec"
	"testing"
)

// 토큰을 여러 파티션에 나눠 써도 dwebp 출력이 인코더의 복원 평면과 비트 단위로 같아야 한다.
// 큰 이미지에서만 나누므로, 작은 이미지에서 파티션 수를 강제로 정해 시험한다.
func Test_bitstreamFrom_multipleTokenPartitions_dwebpMatchesReconstruction(t *testing.T) {
	if _, err := exec.LookPath("dwebp"); err != nil {
		t.Skip("dwebp 없음")
	}
	for partsLog2 := 1; partsLog2 <= maxPartsLog2; partsLog2++ {
		// 행 수(9)가 파티션 수의 배수가 아니게 잡아 마지막 파티션이 덜 찬 경우도 본다
		e := newEncoder(toYUV420(syntheticImage(83, 141), nil), defaultQuality)
		vp8, err := e.bitstreamFrom(e.encodeRows(), partsLog2)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := writeRIFF(&buf, riffChunk{fourccVP8, vp8}); err != nil {
			t.Fatal(err)
		}
		got, want := dwebpYUV(t, buf.Bytes()), flatYUV(e.rec)
		if !bytes.Equal(got, want) {
			t.Fatalf("파티션 %d개: dwebp YUV가 복원 평면과 다름 (첫 차이 %d / %d바이트)", 1<<partsLog2, firstDiff(got, want), len(want))
		}
	}
}
