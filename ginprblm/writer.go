package ginprblm

import (
	"bufio"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

// noWritten is what gin's Size reports before the header is written.
const noWritten = -1

// deferredWriter keeps the response header uncommitted until the body
// starts, so that the middleware can still answer with a problem after a
// handler aborted (design D3). gin's AbortWithStatus and AbortWithError call
// WriteHeaderNow, which would otherwise send the status at once and leave
// the problem without its status and Content-Type.
//
// WriteHeader and WriteHeaderNow only record the status; writing the body
// or flushing commits the recorded status to the underlying writer, and a
// hijack commits the response without sending anything.
type deferredWriter struct {
	gin.ResponseWriter

	// status is the recorded status, or 0 when none was recorded.
	status int
	// isCommitted is set once the recorded status went to the underlying
	// writer.
	isCommitted bool
}

// newDeferredWriter returns a deferred writer over w. A writer whose header
// was already written, for example by middleware that ran earlier, starts
// committed: its status can no longer change.
func newDeferredWriter(w gin.ResponseWriter) *deferredWriter {
	return &deferredWriter{ResponseWriter: w, isCommitted: w.Written()}
}

// WriteHeader records code, or passes it on once the header is committed.
func (w *deferredWriter) WriteHeader(code int) {
	if w.isCommitted {
		w.ResponseWriter.WriteHeader(code)

		return
	}

	if code > 0 {
		w.status = code
	}
}

// WriteHeaderNow keeps the header uncommitted; the status is already
// recorded. Once committed, it is passed on.
func (w *deferredWriter) WriteHeaderNow() {
	if w.isCommitted {
		w.ResponseWriter.WriteHeaderNow()
	}
}

// Write commits the recorded status and writes b.
func (w *deferredWriter) Write(b []byte) (int, error) {
	w.commit()

	return w.ResponseWriter.Write(b)
}

// WriteString commits the recorded status and writes s.
func (w *deferredWriter) WriteString(s string) (int, error) {
	w.commit()

	return w.ResponseWriter.WriteString(s)
}

// Flush commits the recorded status and flushes, so a streamed response
// reaches the client as it is written.
func (w *deferredWriter) Flush() {
	w.commit()
	w.ResponseWriter.Flush()
}

// Hijack hands the connection to the handler. Once it succeeds the handler
// owns the connection, so the response counts as committed and the
// middleware never writes to it. The recorded status is not sent: the
// handler speaks its own protocol from here on.
func (w *deferredWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.ResponseWriter.Hijack()
	if err == nil {
		w.isCommitted = true
	}

	return conn, rw, err
}

// Status returns the recorded status, or the underlying writer's status
// when none was recorded or the header is committed.
func (w *deferredWriter) Status() int {
	if w.isCommitted || w.status == 0 {
		return w.ResponseWriter.Status()
	}

	return w.status
}

// Size reports that nothing was written until the header is committed, as
// gin does.
func (w *deferredWriter) Size() int {
	if !w.isCommitted {
		return noWritten
	}

	return w.ResponseWriter.Size()
}

// Written reports whether the header is committed.
func (w *deferredWriter) Written() bool {
	return w.isCommitted && w.ResponseWriter.Written()
}

// commit hands the recorded status to the underlying writer, which sends it
// with the next write. It does nothing once committed.
func (w *deferredWriter) commit() {
	if w.isCommitted {
		return
	}

	w.isCommitted = true

	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
}

// commitNow commits the recorded status and sends the header at once, for a
// response that ends without a body.
func (w *deferredWriter) commitNow() {
	w.commit()
	w.ResponseWriter.WriteHeaderNow()
}

// Unwrap returns the underlying writer, so [http.ResponseController] still
// reaches deadlines and full duplex. The controller uses Flush and Hijack
// of this writer first, so they still commit.
func (w *deferredWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
