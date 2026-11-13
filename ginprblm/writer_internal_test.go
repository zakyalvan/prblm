package ginprblm

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Design D3: WriteHeader and WriteHeaderNow record the status instead of
// committing it, so the problem can still set the status and Content-Type.
func TestDeferredWriter_Record(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		abort func(c *gin.Context)
	}{
		{name: "abort with status", abort: func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }},
		{name: "write header", abort: func(c *gin.Context) { c.Writer.WriteHeader(http.StatusUnauthorized) }},
		{
			name: "write header now",
			abort: func(c *gin.Context) {
				c.Status(http.StatusUnauthorized)
				c.Writer.WriteHeaderNow()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			underlying := c.Writer
			c.Writer = newDeferredWriter(underlying)

			tt.abort(c)

			if underlying.Written() {
				t.Error("underlying writer committed the header, want it uncommitted")
			}

			if got := c.Writer.Status(); got != http.StatusUnauthorized {
				t.Errorf("Status() = %d, want 401", got)
			}

			if c.Writer.Written() {
				t.Error("Written() = true before any body or commit, want false")
			}
		})
	}
}

// Design D3: once the body starts, the header is committed and the writer
// behaves as gin's own: later status changes are passed on (and ignored by
// gin), and Size counts the body.
func TestDeferredWriter_Committed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		write func(w gin.ResponseWriter)
	}{
		{name: "write", write: func(w gin.ResponseWriter) { _, _ = w.Write([]byte("abc")) }},
		{name: "write string", write: func(w gin.ResponseWriter) { _, _ = w.WriteString("abc") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			w := newDeferredWriter(c.Writer)

			w.WriteHeader(0) // no status: ignored
			w.WriteHeader(http.StatusCreated)

			if got := w.Size(); got != noWritten {
				t.Errorf("Size() before the body = %d, want %d", got, noWritten)
			}

			tt.write(w)
			tt.write(w) // the second write commits nothing more

			w.WriteHeader(http.StatusInternalServerError)
			w.WriteHeaderNow()

			if rec.Code != http.StatusCreated || rec.Body.String() != "abcabc" {
				t.Errorf("sent %d %q, want 201 %q", rec.Code, rec.Body.String(), "abcabc")
			}

			if got := w.Status(); got != http.StatusCreated {
				t.Errorf("Status() = %d, want 201", got)
			}

			if got := w.Size(); got != len("abcabc") {
				t.Errorf("Size() = %d, want %d", got, len("abcabc"))
			}

			if !w.Written() {
				t.Error("Written() = false after the body, want true")
			}
		})
	}
}

// failingHijacker is a gin writer whose connection cannot be hijacked, as
// over HTTP/2.
type failingHijacker struct {
	gin.ResponseWriter
}

func (failingHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

// Murphy row: a failed hijack hands nothing over, so the response stays
// uncommitted and can still become a problem.
func TestDeferredWriter_Hijack_Fails(t *testing.T) {
	t.Parallel()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := newDeferredWriter(failingHijacker{ResponseWriter: c.Writer})

	if _, _, err := w.Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("Hijack error = %v, want ErrNotSupported", err)
	}

	if w.isCommitted {
		t.Error("response committed after a failed hijack, want uncommitted")
	}
}
