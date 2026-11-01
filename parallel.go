package webpi

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// rowWorkers는 n개 행을 나눌 작업자 수다.
func rowWorkers(n int) int {
	return min(runtime.GOMAXPROCS(0), n)
}

// parallelRows는 서로 독립인 행 0..n-1에 fn을 rowWorkers(n)개 작업자로 나눠 부른다.
// fn은 작업자 번호 w(0..rowWorkers(n)-1)도 받아 작업자별 작업 공간을 쓸 수 있다.
// 행마다 결과를 따로 쓰므로 순서와 무관하게 결과가 같다.
func parallelRows(n int, fn func(w, y int)) {
	var next atomic.Int32
	var wg sync.WaitGroup
	for w := range rowWorkers(n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y := int(next.Add(1)) - 1; y < n; y = int(next.Add(1)) - 1 {
				fn(w, y)
			}
		}()
	}
	wg.Wait()
}
