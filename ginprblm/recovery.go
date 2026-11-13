package ginprblm

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
)

// Recovery returns gin middleware that turns a panic in the handlers after
// it into the core's safe 500 problem: status 500, title "Internal Server
// Error" and no detail, whatever the panic value, rendered by the problem
// handler like any other problem (enrichers, negotiated format, headers).
// The value never reaches the client; the [prblm.OnError] hook receives an
// error that unwraps to a [*prblm.PanicError] holding the value and the
// stack.
//
// It is opt-in, and it works whether it is registered before or after
// [Middleware], or without it. It replaces gin.Recovery(); when both are
// installed, the one registered later (closer to the handler) handles the
// panic.
//
// A panic with [http.ErrAbortHandler] itself is passed on unchanged, so
// net/http aborts the response as designed; it is not reported. When the
// response has already started (a body was written, flushed or the
// connection hijacked), or the panic shows the client connection is broken
// (EPIPE, ECONNRESET), nothing is written and only the hook hears of the
// panic.
//
// Recovery never records the panic with c.Error, so [Middleware] never
// answers it a second time.
//
// opts are the options of [prblm.NewHandler], as for [Middleware]. Pass both
// the same options, so panics and errors share one hook and one set of
// enrichers; each builds its own handler from them, which costs only a
// second copy of the configuration.
func Recovery(opts ...prblm.Option) gin.HandlerFunc {
	h := prblm.NewHandler(opts...)

	return func(c *gin.Context) {
		defer recoverPanic(h, c)

		c.Next()
	}
}

// recoverPanic is deferred by Recovery: it recovers a panic in the handlers
// and answers it.
func recoverPanic(h *prblm.Handler, c *gin.Context) {
	v := recover()
	if v == nil {
		return
	}

	// net/http aborts the response on this exact value, by design.
	//nolint:errorlint // only the sentinel itself aborts, not an error wrapping it.
	if err, ok := v.(error); ok && err == http.ErrAbortHandler {
		panic(v)
	}

	c.Abort()

	err := newPanicked(v)
	res, _ := respond(h, c.Request, err)

	// A started response cannot take a problem any more, and a broken
	// connection cannot take anything: the panic only went to the hook.
	if c.Writer.Written() || isBrokenConnection(err.pe) {
		return
	}

	defer guardWrite(h, c.Request)

	send(c.Writer, res)
}

// guardWrite is deferred while a panic is answered. When writing the problem
// panics too, the writer is broken and nothing more can be written, so the
// second panic only goes to the hook instead of escaping Recovery.
func guardWrite(h *prblm.Handler, r *http.Request) {
	v := recover()
	if v == nil {
		return
	}

	_, _ = respond(h, r, newPanicked(v))
}

// panickedError carries a recovered panic to the handler. It describes
// itself as the safe 500 problem, so no mapper ever sees the panic value, as
// with the core's net/http Recover, while the hook still reaches the
// [*prblm.PanicError] through Unwrap.
type panickedError struct {
	pe *prblm.PanicError
}

// newPanicked captures a recovered panic value with the current stack. It
// is called while a deferred function runs, when the panicking frames are
// still on the stack, so the stack shows where the panic happened.
func newPanicked(v any) panickedError {
	return panickedError{pe: &prblm.PanicError{Value: v, Stack: debug.Stack()}}
}

// Error describes the panic; it only reaches the hook.
func (e panickedError) Error() string { return e.pe.Error() }

// Unwrap returns the panic error.
func (e panickedError) Unwrap() error { return e.pe }

// Problem returns the safe 500 problem.
func (panickedError) Problem() *prblm.Problem {
	return prblm.New(http.StatusInternalServerError)
}
