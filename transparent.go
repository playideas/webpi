package webpi

// 투명 영역 정리 — 보이지 않는 픽셀의 색을 평탄하게 바꿔 VP8이 비트를 쓰지 않게 한다.
// 절차는 libwebp WebPCleanupTransparentArea(손실 인코딩)와 같다. 보이는 픽셀(알파 > 0)은 바꾸지 않는다.
const cleanupBlock = 8

// cleanupTransparent는 이미지 안에 온전히 들어가는 8×8 블록마다 정리한다. 일부만 투명한 블록은 투명 픽셀의
// 휘도를 보이는 픽셀의 평균으로, 완전히 투명한 블록은 휘도·색차를 평탄하게 채운다. 같은 행에서 이어지는
// 투명 블록들은 첫 블록의 왼쪽 위 값으로 똑같이 채워 블록 사이도 평탄하게 만든다.
func cleanupTransparent(p *yuvPlanes, alpha []uint8) {
	const half = cleanupBlock / 2
	for by := 0; by+cleanupBlock <= p.h; by += cleanupBlock {
		reset := true
		var yv, uv, vv uint8
		for bx := 0; bx+cleanupBlock <= p.w; bx += cleanupBlock {
			if !smoothenBlock(p, alpha, bx, by) {
				reset = true
				continue
			}
			ci := (by/2)*p.cs + bx/2
			if reset {
				yv, uv, vv = p.y[by*p.ys+bx], p.u[ci], p.v[ci]
				reset = false
			}
			fillPlane(p.y, p.ys, by*p.ys+bx, cleanupBlock, yv)
			fillPlane(p.u, p.cs, ci, half, uv)
			fillPlane(p.v, p.cs, ci, half, vv)
		}
	}
}

// smoothenBlock은 블록의 투명 픽셀 휘도를 보이는 픽셀의 평균으로 바꾸고, 블록 전체가 투명한지 돌려준다.
func smoothenBlock(p *yuvPlanes, alpha []uint8, bx, by int) bool {
	sum, count := 0, 0
	for j := range cleanupBlock {
		for i := range cleanupBlock {
			if alpha[(by+j)*p.w+bx+i] != 0 {
				sum += int(p.y[(by+j)*p.ys+bx+i])
				count++
			}
		}
	}
	if count == 0 || count == cleanupBlock*cleanupBlock {
		return count == 0
	}
	avg := uint8(sum / count)
	for j := range cleanupBlock {
		for i := range cleanupBlock {
			if alpha[(by+j)*p.w+bx+i] == 0 {
				p.y[(by+j)*p.ys+bx+i] = avg
			}
		}
	}
	return false
}

func fillPlane(pl []uint8, stride, off, n int, v uint8) {
	for j := range n {
		row := pl[off+j*stride : off+j*stride+n]
		for i := range row {
			row[i] = v
		}
	}
}
