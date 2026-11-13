package prblm

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
)

// Wrap turns a handler function that returns an error into an [http.Handler].
// A non-nil error is written as a problem response; a nil error leaves the
// response as fn wrote it.
//
// Wrap watches what fn writes. When fn has already started the response (it
// wrote a status other than 1xx, wrote body bytes, flushed or hijacked the
// connection), the error is still reported to the [OnError] hook but nothing
// more is written: the client has the start of another response, and
// appending a problem would corrupt it. Headers fn set without writing are
// kept, except those the problem response controls.
//
// Wrap panics when h or fn is nil, so the mistake shows at startup rather
// than on the first request.
func (h *Handler) Wrap(fn func(http.ResponseWriter, *http.Request) error) http.Handler {
	h.mustExist("Wrap")

	if fn == nil {
		panic("prblm: Handler.Wrap called with a nil handler function")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := track(w)
		h.answer(tw, r, fn(tw, r))
	})
}

// Write writes err as a problem response to w, for handlers that keep the
// standard signature. A nil err writes nothing.
//
// Write cannot see what was written to w before it was called, so it assumes
// nothing was, unless w comes from [Handler.Wrap]: then a response that has
// already started is left alone, and err only reaches the [OnError] hook.
//
// Write panics when h is nil.
func (h *Handler) Write(w http.ResponseWriter, r *http.Request, err error) {
	h.mustExist("Write")
	h.answer(track(w), r, err)
}

// answer writes the problem response for err to tw, unless tw has already
// started its response; the error reaches the OnError hook even then. The
// response goes through tw, which marks it started, so an error is never
// answered twice.
func (h *Handler) answer(tw *trackingWriter, r *http.Request, err error) {
	res, ok := h.Respond(r.Context(), requestOf(r), err)
	if !ok || tw.started {
		return
	}

	tw.send(res)
}

// Recover returns middleware that turns a panic in next into a 500 problem
// response. It is opt-in: [Handler.Wrap] alone lets panics reach net/http,
// so existing recovery middleware keeps working.
//
// The problem is the safe fallback, whatever the panic value: status 500,
// title "Internal Server Error" and no detail, rendered like any other
// problem (enrichers, negotiated format, headers). The value never reaches
// the client; the [OnError] hook receives it as a [*PanicError].
//
// A panic with [http.ErrAbortHandler] itself is passed on unchanged, so
// net/http aborts the response as designed; it is not reported. When the
// handler has already started the response, or the panic shows the client
// connection is broken (EPIPE, ECONNRESET), nothing is written and the hook
// receives the panic with a nil problem.
//
// Enrichers and the hook see the context of the request Recover received,
// not values that middleware inside it added before the panic: install
// Recover inside middleware whose context values they need.
//
// Recover installs the same tracking writer as [Handler.Wrap], and Wrap
// reuses it, so Recover(h.Wrap(fn)) keeps one record of what was written.
//
// Recover panics when h or next is nil.
func (h *Handler) Recover(next http.Handler) http.Handler {
	h.mustExist("Recover")

	if next == nil {
		panic("prblm: Handler.Recover called with a nil next handler")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := track(w)

		defer h.recoverPanic(tw, r)

		next.ServeHTTP(tw, r)
	})
}

// recoverPanic is deferred by Recover: it recovers a panic in the handler
// and answers it on tw.
func (h *Handler) recoverPanic(tw *trackingWriter, r *http.Request) {
	v := recover()
	if v == nil {
		return
	}

	// net/http aborts the response on this exact value, by design.
	//nolint:errorlint // only the sentinel itself aborts, not an error wrapping it.
	if err, ok := v.(error); ok && err == http.ErrAbortHandler {
		panic(v)
	}

	// The panicking frames are still on the stack while a deferred function
	// runs, so the stack shows where the panic happened.
	pe := &PanicError{Value: v, Stack: debug.Stack()}

	// A started response cannot take a problem any more, and a broken
	// connection cannot take anything: the panic only goes to the hook.
	req := requestOf(r)
	ctx := withRequest(r.Context(), req)

	if tw.started || isBrokenConnection(pe) {
		h.report(ctx, pe, nil)

		return
	}

	// A panic while answering is a bug in the library or in the writer; the
	// constant problem cannot fail the same way.
	defer h.writeFallback(ctx, tw)

	tw.send(h.respondPanic(ctx, req, pe))
}

// writeFallback is deferred while a panic is answered: when answering panics
// too, it writes the constant minimal problem if nothing was written yet. It
// never panics itself.
func (h *Handler) writeFallback(ctx context.Context, tw *trackingWriter) {
	v := recover()
	if v == nil {
		return
	}

	// report already contains a panicking hook; this guards the write,
	// should the writer itself panic again.
	defer func() { _ = recover() }()

	h.report(ctx, &PanicError{Value: v, Stack: debug.Stack()}, nil)

	if tw.started {
		return
	}

	tw.send(newResponse(http.StatusInternalServerError, nil, mediaTypeJSON, []byte(fallbackBody)))
}

// mustExist panics when h is nil, naming the method, so that a missing
// NewHandler call fails where the mistake is rather than inside a request.
func (h *Handler) mustExist(method string) {
	if h == nil {
		panic("prblm: Handler." + method + " called on a nil problem handler; create it with NewHandler")
	}
}

// requestOf describes r for [Handler.Respond]. It uses the URL path rather
// than the request URI, so the query string never reaches the problem.
func requestOf(r *http.Request) Request {
	return Request{
		Method: r.Method,
		Path:   r.URL.Path,
		// Repeated Accept lines form one list (RFC 9110, section 5.3).
		Accept: strings.Join(r.Header.Values("Accept"), ","),
	}
}

// trackingWriter records whether the response has started, so that a
// problem is never written after other output. Optional capabilities of the
// underlying writer stay reachable through [http.ResponseController], which
// calls Unwrap; io.Copy asserts [io.ReaderFrom] directly, so trackingWriter
// implements it itself.
type trackingWriter struct {
	http.ResponseWriter

	// started is set once the status line or body may have reached the
	// client.
	started bool
}

// track returns w as a trackingWriter, reusing w when it already is one so
// that nested adapters share one record.
func track(w http.ResponseWriter) *trackingWriter {
	if tw, ok := w.(*trackingWriter); ok {
		return tw
	}

	return &trackingWriter{ResponseWriter: w}
}

// WriteHeader records a final status; an informational 1xx status does not
// start the response, except 101 Switching Protocols.
func (tw *trackingWriter) WriteHeader(code int) {
	if code >= 200 || code == http.StatusSwitchingProtocols {
		tw.started = true
	}

	tw.ResponseWriter.WriteHeader(code)
}

// Write records that the response started; net/http sends the header with
// the first write, even an empty one.
func (tw *trackingWriter) Write(b []byte) (int, error) {
	tw.started = true

	return tw.ResponseWriter.Write(b)
}

// ReadFrom copies src to the underlying writer, and records that the
// response started, as Write does. It keeps the underlying writer's
// [io.ReaderFrom] reachable by io.Copy, so net/http can still send files with
// sendfile; without one, it copies with the underlying writer's Write.
func (tw *trackingWriter) ReadFrom(src io.Reader) (int64, error) {
	tw.started = true

	if rf, ok := tw.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}

	return io.Copy(tw.ResponseWriter, src)
}

// FlushError flushes the underlying writer, and records that the response
// started when it did. It returns [http.ErrNotSupported] when the
// underlying writer cannot flush, as it would without the adapter.
func (tw *trackingWriter) FlushError() error {
	err := http.NewResponseController(tw.ResponseWriter).Flush()
	if err == nil {
		tw.started = true
	}

	return err
}

// Flush implements [http.Flusher] for handlers that assert it directly.
func (tw *trackingWriter) Flush() {
	_ = tw.FlushError()
}

// Hijack takes over the connection of the underlying writer, and records
// that the response started when it did: the handler then owns the
// connection.
func (tw *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(tw.ResponseWriter).Hijack()
	if err == nil {
		tw.started = true
	}

	return conn, rw, err
}

// send writes res: its headers merged into those already set, its status
// and its body.
func (tw *trackingWriter) send(res Response) {
	res.MergeInto(tw.Header())
	tw.WriteHeader(res.Status)
	// A failed write means the client is gone; there is nobody left to
	// tell.
	_, _ = tw.Write(res.Body)
}

// Unwrap returns the underlying writer, for [http.ResponseController].
func (tw *trackingWriter) Unwrap() http.ResponseWriter {
	return tw.ResponseWriter
}
