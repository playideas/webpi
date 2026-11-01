package webpi

import (
	"fmt"
	"runtime/debug"
)

// 패닉을 Encode의 오류로 바꾼다. 작업 고루틴의 패닉은 호출자가 recover로 잡을 수 없어
// 프로세스 전체가 죽기 때문에, 잡은 자리의 스택을 담아 부른 고루틴까지 넘긴다.
// 판단 PD-20261101-b8ce5bf0: 내부 패닉은 프로세스를 죽이지 않고 Encode의 오류로 돌려준다

// PanicError는 Encode 도중 난 패닉이다. 넘겨받은 이미지의 At이나 io.Writer에서 난 패닉도 포함한다.
// Error()는 패닉 값만 담는다 — 오류 문자열이 응답이나 한 줄 로그로 나가도 내부 경로가 드러나지 않게
// 스택은 Stack에 따로 둔다. errors.As로 꺼내 쓴다.
// 판단 PD-20261101-3323928a: 스택은 오류 문자열이 아니라 PanicError.Stack으로 노출한다
type PanicError struct {
	// Value는 panic에 넘겨진 값이다.
	Value any
	// Stack은 패닉이 난 고루틴의, 잡은 순간의 스택이다 (runtime/debug.Stack 형식).
	Stack []byte
}

func (p *PanicError) Error() string {
	return fmt.Sprintf("webpi: 내부 오류: %v", p.Value)
}

// asPanicError는 recover 값을 PanicError로 만든다. 이미 PanicError면 처음 잡은 스택을 그대로 쓴다.
func asPanicError(r any) *PanicError {
	if p, ok := r.(*PanicError); ok {
		return p
	}
	return &PanicError{r, debug.Stack()}
}

// recoverToError는 지연 호출되어 패닉을 오류로 바꿔 *err에 넣는다.
func recoverToError(err *error) {
	if r := recover(); r != nil {
		*err = asPanicError(r)
	}
}
