package prblm

import (
	"context"
	"fmt"
	"runtime/debug"
)

// PanicError is a recovered panic, reported to the [OnError] hook. It never
// reaches the client.
type PanicError struct {
	// Value is the value passed to panic.
	Value any
	// Stack is the stack trace captured where the panic was recovered,
	// which still contains the panicking frames.
	Stack []byte
}

// Error describes the panic value.
func (e *PanicError) Error() string {
	return fmt.Sprintf("prblm: recovered panic: %v", e.Value)
}

// Unwrap returns the panic value when it is an error, so [errors.Is] and
// [errors.As] see through the panic.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)

	return err
}

// safeCall runs consumer code. A panic in fn is recovered and reported to the
// OnError hook as a *PanicError, and safeCall then returns the zero value, so
// faulty consumer code cannot break the response. The stack is only captured
// when there is a hook to receive it.
func safeCall[T any](ctx context.Context, h *Handler, fn func() T) T {
	defer func() {
		if v := recover(); v != nil && h.onError != nil {
			h.report(ctx, &PanicError{Value: v, Stack: debug.Stack()}, nil)
		}
	}()

	return fn()
}
