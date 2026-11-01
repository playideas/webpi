package webpi

import (
	"math/bits"
	"slices"
)

// VP8L 무손실 비트스트림의 비트 기록과 접두 부호(허프만) — WebP Lossless Bitstream Format 3.7.
// 알파 평면을 압축하는 데만 쓴다.
const (
	// 접두 부호의 최대 길이와, 그 길이들을 부호화하는 부호의 최대 길이
	maxCodeLength           = 15
	maxCodeLengthCodeLength = 7
	numCodeLengthCodes      = 19
	minCodeLengthCodes      = 4
	codeLengthCodeBits      = 3
	numCodeLengthCodesBits  = 4
	// 단순 부호는 기호 2개까지, 기호 값은 8비트까지 쓸 수 있다
	simpleCodeMaxSymbols = 2
	simpleSymbolLimit    = 256
	simpleSymbolBits     = 8
	// 길이 부호화에서 반복 기호: 16 = 앞 길이 3~6번, 17 = 0을 3~10번, 18 = 0을 11~138번
	codeRepeatPrev   = 16
	codeRepeatZeros  = 17
	codeRepeatZerosL = 18
	repeatPrevMin    = 3
	repeatPrevMax    = 6
	repeatZerosMin   = 3
	repeatZerosMax   = 10
	repeatZerosLMin  = 11
	repeatZerosLMax  = 138
	// 반복 기호 16이 처음 가리키는 "앞 길이"
	initialPrevCodeLength = 8
)

// codeLengthOrder는 길이 부호의 길이를 쓰는 순서다.
var codeLengthOrder = [numCodeLengthCodes]uint8{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// repeatExtraBits는 반복 기호 16·17·18 뒤에 오는 추가 비트 수다.
var repeatExtraBits = [3]uint{2, 3, 7}

// bitWriter는 VP8L 방식(낮은 비트부터)으로 비트를 쌓는다.
type bitWriter struct {
	buf []byte
	acc uint64
	n   uint
}

func (w *bitWriter) put(v uint32, nbits uint) {
	w.acc |= uint64(v) << w.n
	w.n += nbits
	for w.n >= 8 {
		w.buf = append(w.buf, byte(w.acc))
		w.acc >>= 8
		w.n -= 8
	}
}

// bytes는 남은 비트를 0으로 채워 바이트 경계에 맞춘 결과를 돌려준다.
func (w *bitWriter) bytes() []byte {
	if w.n > 0 {
		w.buf = append(w.buf, byte(w.acc))
		w.acc, w.n = 0, 0
	}
	return w.buf
}

// prefixCode는 기호마다 부호 길이와, 낮은 비트부터 쓰도록 뒤집은 부호다. 길이 0인 기호는 0비트로 쓴다.
type prefixCode struct {
	lengths []uint8
	codes   []uint16
}

func (c *prefixCode) write(w *bitWriter, sym int) {
	w.put(uint32(c.codes[sym]), uint(c.lengths[sym]))
}

// huffmanLengths는 빈도 counts에서 maxLen 이하의 허프만 부호 길이를 만든다.
// 너무 길어지면 작은 빈도를 끌어올려(바닥값을 두 배씩) 트리를 납작하게 만든 뒤 다시 짓는다.
func huffmanLengths(counts []uint32, maxLen int) []uint8 {
	lengths := make([]uint8, len(counts))
	for floor := uint64(1); ; floor *= 2 {
		if buildLengths(counts, floor, lengths) <= maxLen {
			return lengths
		}
	}
}

type huffLeaf struct {
	w   uint64
	sym int
}

// buildLengths는 빈도를 floor 이상으로 올린 허프만 트리의 깊이를 lengths에 쓰고 최대 깊이를 돌려준다.
// 잎을 무게순으로 놓고 내부 노드를 만들어지는 순서(무게 오름차순)로 쌓는 두 줄 병합이다.
func buildLengths(counts []uint32, floor uint64, lengths []uint8) int {
	clear(lengths)
	var leaves []huffLeaf
	for s, c := range counts {
		if c > 0 {
			leaves = append(leaves, huffLeaf{max(uint64(c), floor), s})
		}
	}
	if len(leaves) <= 1 {
		for _, l := range leaves {
			lengths[l.sym] = 1
		}
		return len(leaves)
	}
	slices.SortFunc(leaves, func(a, b huffLeaf) int {
		if a.w != b.w {
			return cmpU64(a.w, b.w)
		}
		return a.sym - b.sym
	})
	n := len(leaves)
	// 노드 id: 잎은 0..n-1, 내부 노드는 n..2n-2. 부모 id는 언제나 자식보다 크다
	parent := make([]int, 2*n-1)
	internal := make([]uint64, 0, n-1)
	li, ii := 0, 0
	pick := func() (uint64, int) {
		if li < n && (ii >= len(internal) || leaves[li].w <= internal[ii]) {
			li++
			return leaves[li-1].w, li - 1
		}
		ii++
		return internal[ii-1], n + ii - 1
	}
	for k := range n - 1 {
		wa, a := pick()
		wb, b := pick()
		parent[a], parent[b] = n+k, n+k
		internal = append(internal, wa+wb)
	}
	depth := make([]int, 2*n-1)
	maxDepth := 0
	for id := 2*n - 3; id >= 0; id-- {
		depth[id] = depth[parent[id]] + 1
		if id < n {
			lengths[leaves[id].sym] = uint8(depth[id])
			maxDepth = max(maxDepth, depth[id])
		}
	}
	return maxDepth
}

func cmpU64(a, b uint64) int {
	if a < b {
		return -1
	}
	return 1
}

// canonicalCodes는 길이에서 정규 허프만 부호를 만들고, 낮은 비트부터 쓰도록 비트 순서를 뒤집는다.
func canonicalCodes(lengths []uint8) []uint16 {
	var count, next [maxCodeLength + 1]int
	for _, l := range lengths {
		count[l]++
	}
	count[0] = 0
	code := 0
	for b := 1; b <= maxCodeLength; b++ {
		code = (code + count[b-1]) << 1
		next[b] = code
	}
	codes := make([]uint16, len(lengths))
	for s, l := range lengths {
		if l > 0 {
			codes[s] = bits.Reverse16(uint16(next[l])) >> (16 - l)
			next[l]++
		}
	}
	return codes
}

// writePrefixCode는 빈도 counts의 접두 부호를 비트스트림에 쓰고 그 부호를 돌려준다.
// 쓰는 기호가 2개 이하이고 모두 8비트 값이면 단순 부호로 쓴다 (기호 하나는 0비트).
func writePrefixCode(w *bitWriter, counts []uint32) prefixCode {
	var syms []int
	for s, c := range counts {
		if c > 0 {
			if syms = append(syms, s); len(syms) > simpleCodeMaxSymbols {
				break
			}
		}
	}
	if len(syms) <= simpleCodeMaxSymbols && (len(syms) == 0 || syms[len(syms)-1] < simpleSymbolLimit) {
		return writeSimpleCode(w, syms, len(counts))
	}
	lengths := huffmanLengths(counts, maxCodeLength)
	writeCodeLengths(w, lengths)
	return prefixCode{lengths, canonicalCodes(lengths)}
}

func writeSimpleCode(w *bitWriter, syms []int, alphabet int) prefixCode {
	c := prefixCode{lengths: make([]uint8, alphabet)}
	if len(syms) == 0 {
		// 쓰이지 않는 부호 — 기호 0 하나짜리로 둔다
		syms = []int{0}
	}
	w.put(1, 1)
	w.put(uint32(len(syms)-1), 1)
	if syms[0] < 2 {
		w.put(0, 1)
		w.put(uint32(syms[0]), 1)
	} else {
		w.put(1, 1)
		w.put(uint32(syms[0]), simpleSymbolBits)
	}
	if len(syms) == simpleCodeMaxSymbols {
		w.put(uint32(syms[1]), simpleSymbolBits)
		c.lengths[syms[0]], c.lengths[syms[1]] = 1, 1
	}
	c.codes = canonicalCodes(c.lengths)
	return c
}

// clToken은 부호 길이 열을 부호화한 기호(0~18)와 반복 횟수 추가 비트다.
type clToken struct {
	sym, extra uint8
}

// writeCodeLengths는 일반 부호의 길이 열을 길이 부호로 압축해 쓴다.
func writeCodeLengths(w *bitWriter, lengths []uint8) {
	tokens := rleCodeLengths(lengths)
	var counts [numCodeLengthCodes]uint32
	for _, t := range tokens {
		counts[t.sym]++
	}
	// 기호가 하나뿐인 부호는 디코더마다 처리가 달라 피한다 — 쓰이지 않는 기호를 하나 더 둔다
	if used := countNonZero(counts[:]); used == 1 {
		counts[b2i(counts[0] > 0)]++
	}
	clLengths := huffmanLengths(counts[:], maxCodeLengthCodeLength)
	clCodes := prefixCode{clLengths, canonicalCodes(clLengths)}
	n := numCodeLengthCodes
	for n > minCodeLengthCodes && clLengths[codeLengthOrder[n-1]] == 0 {
		n--
	}
	w.put(0, 1)
	w.put(uint32(n-minCodeLengthCodes), numCodeLengthCodesBits)
	for _, s := range codeLengthOrder[:n] {
		w.put(uint32(clLengths[s]), codeLengthCodeBits)
	}
	// max_symbol 없음 — 길이 열 전체를 쓴다
	w.put(0, 1)
	for _, t := range tokens {
		clCodes.write(w, int(t.sym))
		if t.sym >= codeRepeatPrev {
			w.put(uint32(t.extra), repeatExtraBits[t.sym-codeRepeatPrev])
		}
	}
}

func countNonZero(c []uint32) int {
	n := 0
	for _, v := range c {
		n += int(b2i(v > 0))
	}
	return n
}

// rleCodeLengths는 길이 열을 리터럴 길이와 반복 기호로 바꾼다.
func rleCodeLengths(lengths []uint8) []clToken {
	var out []clToken
	prev := uint8(initialPrevCodeLength)
	for i := 0; i < len(lengths); {
		v := lengths[i]
		run := 1
		for i+run < len(lengths) && lengths[i+run] == v {
			run++
		}
		i += run
		if v == 0 {
			out = appendZeroRun(out, run)
			continue
		}
		if v != prev {
			out = append(out, clToken{sym: v})
			prev = v
			run--
		}
		for ; run >= repeatPrevMin; run -= min(run, repeatPrevMax) {
			out = append(out, clToken{codeRepeatPrev, uint8(min(run, repeatPrevMax) - repeatPrevMin)})
		}
		for ; run > 0; run-- {
			out = append(out, clToken{sym: v})
		}
	}
	return out
}

func appendZeroRun(out []clToken, run int) []clToken {
	for run >= repeatZerosLMin {
		r := min(run, repeatZerosLMax)
		out = append(out, clToken{codeRepeatZerosL, uint8(r - repeatZerosLMin)})
		run -= r
	}
	if run >= repeatZerosMin {
		out = append(out, clToken{codeRepeatZeros, uint8(run - repeatZerosMin)})
		run = 0
	}
	for ; run > 0; run-- {
		out = append(out, clToken{})
	}
	return out
}
