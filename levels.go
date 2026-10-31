package webpi

// packLevels는 블록마다 "지그재그 순서의 마지막 0 아닌 계수까지의 개수"와 그 계수들을 dst에 덧붙인다.
// 레벨은 maxLevel(2047)로 잘려 있어 int16에 들어간다.
func packLevels(dst []int16, levels *[nBlocks][16]int32) []int16 {
	for b := range levels {
		n := 0
		for k := 15; k >= 0; k-- {
			if levels[b][zigzag[k]] != 0 {
				n = k + 1
				break
			}
		}
		dst = append(dst, int16(n))
		for k := range n {
			dst = append(dst, int16(levels[b][zigzag[k]]))
		}
	}
	return dst
}

// unpackLevels는 packLevels로 쓴 매크로블록 하나를 levels에 풀고 남은 버퍼를 돌려준다.
func unpackLevels(src []int16, levels *[nBlocks][16]int32) []int16 {
	*levels = [nBlocks][16]int32{}
	for b := range levels {
		n := int(src[0])
		for k := range n {
			levels[b][zigzag[k]] = int32(src[1+k])
		}
		src = src[1+n:]
	}
	return src
}
