package prblm

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
)

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
