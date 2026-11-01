package webpi

import (
	"fmt"
	"runtime/debug"
)

// 내부 패닉을 Encode의 오류로 바꾼다. 작업 고루틴의 패닉은 호출자가 recover로 잡을 수 없어
// 프로세스 전체가 죽기 때문에, 잡은 자리의 스택을 담아 부른 고루틴까지 넘긴다.
// 판단 PD-20261101-b8ce5bf0: 내부 패닉은 프로세스를 죽이지 않고 Encode의 오류로 돌려준다

// panicError는 잡은 패닉 값과 그 순간의 스택이다.
type panicError struct {
	value any
	stack []byte
}

func (p *panicError) Error() string {
	return fmt.Sprintf("webpi: 내부 오류: %v\n%s", p.value, p.stack)
}

// asPanicError는 recover 값을 panicError로 만든다. 이미 panicError면 처음 잡은 스택을 그대로 쓴다.
func asPanicError(r any) *panicError {
	if p, ok := r.(*panicError); ok {
		return p
	}
	return &panicError{r, debug.Stack()}
}

// recoverToError는 지연 호출되어 패닉을 오류로 바꿔 *err에 넣는다.
func recoverToError(err *error) {
	if r := recover(); r != nil {
		*err = asPanicError(r)
	}
}
