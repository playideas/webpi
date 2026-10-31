package webpi

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// parallelRows는 서로 독립인 행 0..n-1에 fn을 작업자 여럿으로 나눠 부른다.
// 행마다 결과를 따로 쓰므로 순서와 무관하게 결과가 같다.
func parallelRows(n int, fn func(y int)) {
	var next atomic.Int32
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y := int(next.Add(1)) - 1; y < n; y = int(next.Add(1)) - 1 {
				fn(y)
			}
		}()
	}
	wg.Wait()
}
