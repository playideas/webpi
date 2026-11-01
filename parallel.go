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
// 작업자에서 난 패닉은 모두 끝난 뒤 부른 고루틴에서 다시 일으킨다 (처음 것 하나, 원래 스택 포함).
func parallelRows(n int, fn func(w, y int)) {
	var next atomic.Int32
	var first atomic.Pointer[PanicError]
	var wg sync.WaitGroup
	for w := range rowWorkers(n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					first.CompareAndSwap(nil, asPanicError(r))
				}
			}()
			for y := int(next.Add(1)) - 1; y < n; y = int(next.Add(1)) - 1 {
				fn(w, y)
			}
		}()
	}
	wg.Wait()
	if p := first.Load(); p != nil {
		panic(p)
	}
}
