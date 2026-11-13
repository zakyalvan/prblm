package prblm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kartaladev/prblm"
)

const problemJSONType = "application/problem+json"

// decodeMembers decodes a problem body into its raw members.
func decodeMembers(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var members map[string]any
	if err := json.Unmarshal(body, &members); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", body, err)
	}

	return members
}

// stdlib-adapter / Scenario: Handler result (rows: returns mapped, writes 200
// and returns nil, writes nothing, returns unmapped).
func TestWrap(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)))

	tests := []struct {
		name        string
		fn          func(http.ResponseWriter, *http.Request) error
		status      int
		contentType string
		body        string         // exact body, for non-problem responses
		members     map[string]any // expected members, for problem responses
		absent      []string       // members that must not be present
	}{
		{
			name:        "returns mapped",
			fn:          func(http.ResponseWriter, *http.Request) error { return errVesselNotFound },
			status:      404,
			contentType: problemJSONType,
			members:     map[string]any{"status": 404.0, "instance": "/v1/vessels/7"},
		},
		{
			name: "writes 200 and returns nil",
			fn: func(w http.ResponseWriter, _ *http.Request) error {
				w.WriteHeader(http.StatusOK)
				_, err := w.Write([]byte(`{"id":7}`))

				return err
			},
			status: 200,
			body:   `{"id":7}`,
		},
		{
			name:   "writes nothing",
			fn:     func(http.ResponseWriter, *http.Request) error { return nil },
			status: 200,
			body:   "",
		},
		{
			name:        "returns unmapped",
			fn:          func(http.ResponseWriter, *http.Request) error { return errors.New("db password=hunter2") },
			status:      500,
			contentType: problemJSONType,
			members:     map[string]any{"status": 500.0},
			absent:      []string{"detail"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

			h.Wrap(tt.fn).ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if tt.members == nil {
				if got := rec.Body.String(); got != tt.body {
					t.Errorf("body = %q, want %q", got, tt.body)
				}

				return
			}

			if got := rec.Header().Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
			}

			members := decodeMembers(t, rec.Body.Bytes())
			for name, expected := range tt.members {
				if members[name] != expected {
					t.Errorf("member %q = %v, want %v", name, members[name], expected)
				}
			}

			for _, name := range tt.absent {
				if v, ok := members[name]; ok {
					t.Errorf("member %q = %v, want absent", name, v)
				}
			}
		})
	}
}

// Slice 1 end to end: a real server and client, a mapped sentinel and an
// unmapped error.
func TestWrap_EndToEnd(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, http.StatusNotFound)))

	mux := http.NewServeMux()
	mux.Handle("GET /v1/vessels/{id}", h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		switch r.PathValue("id") {
		case "7":
			return fmt.Errorf("load vessel 7: %w", errVesselNotFound)
		case "8":
			return errors.New("pq: connection refused to 10.0.0.5")
		default:
			_, err := w.Write([]byte(`{"id":1}`))

			return err
		}
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name        string
		path        string
		status      int
		contentType string
		body        string
	}{
		{
			name:        "mapped",
			path:        "/v1/vessels/7?token=abc",
			status:      404,
			contentType: problemJSONType,
			body:        `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`,
		},
		{
			name:        "unmapped",
			path:        "/v1/vessels/8",
			status:      500,
			contentType: problemJSONType,
			body:        `{"title":"Internal Server Error","status":500,"instance":"/v1/vessels/8"}`,
		},
		{
			name:   "success",
			path:   "/v1/vessels/1",
			status: 200,
			body:   `{"id":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := get(t, srv, tt.path)

			if res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}

			if tt.contentType != "" && res.header.Get("Content-Type") != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", res.header.Get("Content-Type"), tt.contentType)
			}

			if string(res.body) != tt.body {
				t.Errorf("body = %s, want %s", res.body, tt.body)
			}
		})
	}
}

// Slice 3 end to end: a mapped problem with extensions and headers, and one
// whose extension cannot be encoded.
func TestWrap_EndToEnd_ProblemModel(t *testing.T) {
	t.Parallel()

	errBusy := errors.New("busy")
	errBroken := errors.New("broken")

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.MapperFunc(func(_ context.Context, err error) *prblm.Problem {
			if !errors.Is(err, errBusy) {
				return nil
			}

			return prblm.New(http.StatusServiceUnavailable,
				prblm.WithExt("retry_in", 5),
				prblm.WithExt("queue", []string{"a", "b"}),
				prblm.WithHeader("Retry-After", "5"),
				prblm.WithCause(err),
			)
		}),
		prblm.MapperFunc(func(_ context.Context, err error) *prblm.Problem {
			if !errors.Is(err, errBroken) {
				return nil
			}

			return prblm.New(http.StatusConflict, prblm.WithExt("bad", make(chan int)))
		}),
	))

	mux := http.NewServeMux()
	mux.Handle("GET /busy", h.Wrap(func(http.ResponseWriter, *http.Request) error { return errBusy }))
	mux.Handle("GET /broken", h.Wrap(func(http.ResponseWriter, *http.Request) error { return errBroken }))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		path       string
		status     int
		body       string
		retryAfter string
	}{
		{
			name:       "extensions and headers",
			path:       "/busy",
			status:     503,
			body:       `{"title":"Service Unavailable","status":503,"instance":"/busy","queue":["a","b"],"retry_in":5}`,
			retryAfter: "5",
		},
		{
			name:   "unencodable extension",
			path:   "/broken",
			status: 500,
			body:   `{"title":"Internal Server Error","status":500}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := get(t, srv, tt.path)

			if res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}

			if got := res.header.Get("Content-Type"); got != problemJSONType {
				t.Errorf("Content-Type = %q, want %q", got, problemJSONType)
			}

			if got := res.header.Get("Retry-After"); got != tt.retryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tt.retryAfter)
			}

			if string(res.body) != tt.body {
				t.Errorf("body = %s, want %s", res.body, tt.body)
			}
		})
	}
}

// response is what a client received.
type response struct {
	header http.Header
	body   []byte
	status int
}

// get sends a GET request for path to srv and returns what came back.
func get(t *testing.T, srv *httptest.Server, path string) response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			t.Errorf("closing body: %v", err)
		}
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return response{header: res.Header, body: body, status: res.StatusCode}
}

// Slice 4 end to end: every resolution step through one server.
func TestWrap_EndToEnd_Resolution(t *testing.T) {
	t.Parallel()

	errLockedE2E := errors.New("locked")

	var r recorder

	h := prblm.NewHandler(
		prblm.WithMappers(prblm.Status(errVesselNotFound, http.StatusNotFound)),
		prblm.WithEnrichers(traceID),
		prblm.OnError(r.hook),
	)

	routes := map[string]error{
		"/v1/contracts/locked":  errLockedE2E,
		"/v1/vessels/missing":   fmt.Errorf("load: %w", errVesselNotFound),
		"/v1/vessels/bad-body":  decodeErr(`{"age":}`),
		"/v1/vessels/forbidden": prblm.WithStatus(errors.New("tenant 42"), http.StatusForbidden),
		"/v1/vessels/conflict":  prblm.New(http.StatusConflict),
		"/v1/vessels/broken":    errors.New("pq: connection refused to 10.0.0.5"),
	}

	fail := h.Wrap(func(_ http.ResponseWriter, req *http.Request) error { return routes[req.URL.Path] })

	mux := http.NewServeMux()
	mux.Handle("/v1/contracts/", scoped(fail, prblm.Status(errLockedE2E, http.StatusLocked)))
	mux.Handle("/v1/vessels/", fail)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mux.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), traceKey{}, "t-1")))
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		path   string
		status int
		detail string
	}{
		{path: "/v1/contracts/locked", status: 423},
		{path: "/v1/vessels/missing", status: 404},
		{path: "/v1/vessels/bad-body", status: 400, detail: "The request body is not valid JSON."},
		{path: "/v1/vessels/forbidden", status: 403},
		{path: "/v1/vessels/conflict", status: 409},
		{path: "/v1/vessels/broken", status: 500},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			res := get(t, srv, tt.path)
			members := decodeMembers(t, res.body)

			if res.status != tt.status || members["status"] != float64(tt.status) {
				t.Errorf("status = %d (body %v), want %d", res.status, members["status"], tt.status)
			}

			if members["instance"] != tt.path || members["trace_id"] != "t-1" {
				t.Errorf("instance = %v, trace_id = %v; want %s, t-1", members["instance"], members["trace_id"], tt.path)
			}

			if detail, _ := members["detail"].(string); detail != tt.detail {
				t.Errorf("detail = %q, want %q", detail, tt.detail)
			}

			if strings.Contains(string(res.body), "10.0.0.5") || strings.Contains(string(res.body), "tenant") {
				t.Errorf("body %s leaks error text", res.body)
			}
		})
	}

	t.Cleanup(func() {
		if calls := r.recorded(); len(calls) != len(tests) {
			t.Errorf("hook called %d times, want %d", len(calls), len(tests))
		}
	})
}

// syncBuffer is a bytes.Buffer safe for the server's concurrent log writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// problem-response / Scenario: Handler wrote before failing; stdlib-adapter /
// Scenario: Handler result (last row).
func TestWrap_Started(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wrote   func(t *testing.T, w http.ResponseWriter)
		status  int
		body    string // exact body; empty with problem set means a problem
		problem bool
	}{
		{
			name: "status 200 and a partial body",
			wrote: func(_ *testing.T, w http.ResponseWriter) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"partial":true}`))
			},
			status: 200,
			body:   `{"partial":true}`,
		},
		{
			name:   "status 202 and no body",
			wrote:  func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) },
			status: 202,
		},
		{
			name: "flushed headers",
			wrote: func(t *testing.T, w http.ResponseWriter) {
				t.Helper()

				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("Flush() = %v", err)
				}
			},
			status: 200,
		},
		{
			name: "flushed headers through http.Flusher",
			wrote: func(t *testing.T, w http.ResponseWriter) {
				t.Helper()

				f, ok := w.(http.Flusher)
				if !ok {
					t.Fatal("writer is not an http.Flusher")
				}

				f.Flush()
			},
			status: 200,
		},
		{
			name:    "set a header but wrote nothing",
			wrote:   func(_ *testing.T, w http.ResponseWriter) { w.Header().Set("X-Request-Id", "r-1") },
			status:  404,
			problem: true,
		},
		{
			name: "stdlib: wrote 200 and a body",
			wrote: func(_ *testing.T, w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"id":7}`))
			},
			status: 200,
			body:   `{"id":7}`,
		},
		// Murphy rows.
		{
			name:   "an empty write",
			wrote:  func(_ *testing.T, w http.ResponseWriter) { _, _ = w.Write(nil) },
			status: 200,
		},
		{
			name:    "an informational status only",
			wrote:   func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(http.StatusEarlyHints) },
			status:  404,
			problem: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)), prblm.OnError(r.hook))
			srv := httptest.NewServer(h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
				tt.wrote(t, w)

				return errVesselNotFound
			}))
			t.Cleanup(srv.Close)

			res := get(t, srv, "/v1/vessels/7")

			if res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}

			if tt.problem {
				if got := res.header.Get("Content-Type"); got != problemJSONType {
					t.Errorf("Content-Type = %q, want a problem", got)
				}

				if got := res.header.Get("X-Request-Id"); tt.name == "set a header but wrote nothing" && got != "r-1" {
					t.Errorf("X-Request-Id = %q, want the handler's header kept", got)
				}
			} else if string(res.body) != tt.body {
				t.Errorf("body = %q, want %q only", res.body, tt.body)
			}

			calls := r.recorded()
			//nolint:errorlint // the hook must receive the original error value itself.
			if len(calls) != 1 || calls[0].err != errVesselNotFound {
				t.Errorf("hook calls = %+v, want one call with ErrVesselNotFound", calls)
			}
		})
	}
}

// A handler that hijacked the connection owns it: nothing more is written
// through the response writer, and the error still reaches the hook.
func TestWrap_Started_Hijacked(t *testing.T) {
	t.Parallel()

	var (
		r    recorder
		logs syncBuffer
	)

	h := prblm.NewHandler(prblm.OnError(r.hook))
	wrapped := h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return err
		}

		defer func() { _ = conn.Close() }()

		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		_ = buf.Flush()

		return errVesselNotFound
	})

	// Close does not wait for hijacked connections, so the handler signals
	// when the adapter is done.
	done := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer close(done)

		wrapped.ServeHTTP(w, req)
	}))
	srv.Config.ErrorLog = log.New(&logs, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)

	res := get(t, srv, "/v1/vessels/7")

	if res.status != http.StatusOK || string(res.body) != "ok" {
		t.Errorf("got %d %q, want 200 \"ok\" written by the handler", res.status, res.body)
	}

	<-done

	if got := logs.String(); got != "" {
		t.Errorf("server log = %q, want nothing written after the hijack", got)
	}

	//nolint:errorlint // the hook must receive the original error value itself.
	if calls := r.recorded(); len(calls) != 1 || calls[0].err != errVesselNotFound {
		t.Errorf("hook calls = %+v, want one call with ErrVesselNotFound", calls)
	}
}

// stdlib-adapter / Scenario: Flush through the adapter.
func TestWrap_ResponseController(t *testing.T) {
	t.Parallel()

	received := make(chan struct{})

	var returned atomic.Bool

	h := prblm.NewHandler()
	srv := httptest.NewServer(h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		defer returned.Store(true)

		rc := http.NewResponseController(w)

		if err := rc.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			t.Errorf("SetWriteDeadline() = %v", err)
		}

		if err := rc.EnableFullDuplex(); err != nil {
			t.Errorf("EnableFullDuplex() = %v", err)
		}

		if _, err := w.Write([]byte("data: 1\n")); err != nil {
			return err
		}

		if err := rc.Flush(); err != nil {
			t.Errorf("Flush() = %v", err)
		}

		// Hold the response open until the client has the flushed data.
		select {
		case <-received:
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}

		return nil
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil || line != "data: 1\n" {
		t.Fatalf("first line = %q, %v; want \"data: 1\\n\"", line, err)
	}

	if returned.Load() {
		t.Error("data arrived only after the handler returned, want it flushed before")
	}

	close(received)
}

// bareWriter is a response writer with no optional capabilities.
type bareWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (b *bareWriter) Header() http.Header { return b.header }

func (b *bareWriter) WriteHeader(code int) { b.status = code }

func (b *bareWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}

	return b.body.Write(p)
}

// A capability the underlying writer lacks is reported as unsupported, as
// without the adapter, and does not count as starting the response.
func TestWrap_ResponseController_NotSupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		use  func(rc *http.ResponseController) error
	}{
		{name: "flush", use: func(rc *http.ResponseController) error { return rc.Flush() }},
		{name: "hijack", use: func(rc *http.ResponseController) error { _, _, err := rc.Hijack(); return err }},
		{name: "write deadline", use: func(rc *http.ResponseController) error { return rc.SetWriteDeadline(time.Time{}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := &bareWriter{header: make(http.Header)}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)))
			h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
				if err := tt.use(http.NewResponseController(w)); !errors.Is(err, http.ErrNotSupported) {
					t.Errorf("err = %v, want http.ErrNotSupported", err)
				}

				return errVesselNotFound
			}).ServeHTTP(w, req)

			if w.status != http.StatusNotFound || w.header.Get("Content-Type") != problemJSONType {
				t.Errorf("got %d %q, want a 404 problem", w.status, w.header.Get("Content-Type"))
			}
		})
	}
}

// readerFromWriter is a bareWriter that also copies with ReadFrom, as
// net/http's writer does to use sendfile, and counts those calls.
type readerFromWriter struct {
	bareWriter

	// err, when set, is returned by ReadFrom without copying.
	err       error
	readFroms int
}

func (w *readerFromWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFroms++

	if w.err != nil {
		return 0, w.err
	}

	return io.Copy(&w.bareWriter, r)
}

// stdlib-adapter / Scenario: Copy through the adapter.
func TestWrap_ReadFrom(t *testing.T) {
	t.Parallel()

	errDiskRead := errors.New("disk read failed")

	tests := []struct {
		name       string
		readerFrom bool  // the underlying writer implements io.ReaderFrom
		failCopy   error // the underlying ReadFrom fails with this error
		returned   error // the handler returns this after copying
		readFroms  int
		status     int
		body       string
	}{
		{name: "copy uses the underlying ReadFrom", readerFrom: true, readFroms: 1, status: 200, body: `{"id":7}`},
		{name: "error after copy", readerFrom: true, returned: errVesselNotFound, readFroms: 1, status: 200, body: `{"id":7}`},
		// Murphy rows.
		{name: "underlying writer without ReadFrom", returned: errVesselNotFound, status: 200, body: `{"id":7}`},
		// A copy marks the response started even when it fails, as Write does:
		// the underlying writer may already have sent the header.
		{name: "underlying ReadFrom fails", readerFrom: true, failCopy: errDiskRead, returned: errVesselNotFound, readFroms: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rw := &readerFromWriter{header: make(http.Header), err: tt.failCopy}

			var w http.ResponseWriter = &rw.bareWriter
			if tt.readerFrom {
				w = rw
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)
			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, http.StatusNotFound)))

			h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
				// Hide WriteTo, so that io.Copy has to use the writer's ReadFrom.
				src := struct{ io.Reader }{strings.NewReader(`{"id":7}`)}
				if _, err := io.Copy(w, src); !errors.Is(err, tt.failCopy) {
					t.Errorf("io.Copy() error = %v, want %v", err, tt.failCopy)
				}

				return tt.returned
			}).ServeHTTP(w, req)

			if rw.readFroms != tt.readFroms {
				t.Errorf("underlying ReadFrom called %d times, want %d", rw.readFroms, tt.readFroms)
			}

			if rw.status != tt.status || rw.body.String() != tt.body {
				t.Errorf("client got %d %q, want %d %q", rw.status, rw.body.String(), tt.status, tt.body)
			}
		})
	}
}

// stdlib-adapter / Scenario: Direct write from a standard handler (rows
// without XML).
func TestWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler func(h *prblm.Handler) http.Handler
		status  int
		body    string // exact body
		hooked  int    // hook calls
	}{
		{
			name: "mapped error",
			handler: func(h *prblm.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					h.Write(w, r, errVesselNotFound)
				})
			},
			status: 404,
			body:   `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`,
			hooked: 1,
		},
		{
			name: "nil error",
			handler: func(h *prblm.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					h.Write(w, r, nil)
				})
			},
			status: 200,
			body:   "",
		},
		// Murphy rows: Write inside a wrapped handler shares its tracking.
		{
			name: "inside Wrap after the handler wrote",
			handler: func(h *prblm.Handler) http.Handler {
				return h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
					_, _ = w.Write([]byte(`{"id":7}`))
					h.Write(w, r, errVesselNotFound)

					return nil
				})
			},
			status: 200,
			body:   `{"id":7}`,
			hooked: 1,
		},
		{
			name: "inside Wrap and also returned",
			handler: func(h *prblm.Handler) http.Handler {
				return h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
					h.Write(w, r, errVesselNotFound)

					return errVesselNotFound
				})
			},
			status: 404,
			body:   `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`,
			hooked: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)), prblm.OnError(r.hook))
			srv := httptest.NewServer(tt.handler(h))
			t.Cleanup(srv.Close)

			res := get(t, srv, "/v1/vessels/7")

			if res.status != tt.status || string(res.body) != tt.body {
				t.Errorf("got %d %s, want %d %s", res.status, res.body, tt.status, tt.body)
			}

			if tt.status == 404 && res.header.Get("Content-Type") != problemJSONType {
				t.Errorf("Content-Type = %q, want %q", res.header.Get("Content-Type"), problemJSONType)
			}

			if calls := r.recorded(); len(calls) != tt.hooked {
				t.Errorf("hook called %d times, want %d", len(calls), tt.hooked)
			}
		})
	}
}

// mustPanic runs fn and returns the message it panicked with.
func mustPanic(t *testing.T, fn func()) (msg string) {
	t.Helper()

	defer func() {
		v := recover()
		if v == nil {
			t.Fatal("did not panic")
		}

		msg = fmt.Sprint(v)
	}()

	fn()

	return ""
}

// stdlib-adapter / Scenario: Nil arguments.
func TestWrap_Nil(t *testing.T) {
	t.Parallel()

	fn := func(http.ResponseWriter, *http.Request) error { return nil }

	tests := []struct {
		name     string
		setup    func()
		argument string
	}{
		{name: "nil handler function", setup: func() { prblm.NewHandler().Wrap(nil) }, argument: "handler function"},
		{name: "nil problem handler", setup: func() { (*prblm.Handler)(nil).Wrap(fn) }, argument: "problem handler"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if msg := mustPanic(t, tt.setup); !strings.Contains(msg, tt.argument) {
				t.Errorf("panic %q does not name the %s", msg, tt.argument)
			}
		})
	}
}

// Write on a nil *Handler panics with a clear message rather than a nil
// dereference inside the resolution.
func TestWrite_NilHandler(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

	msg := mustPanic(t, func() { (*prblm.Handler)(nil).Write(rec, req, errVesselNotFound) })
	if !strings.Contains(msg, "problem handler") {
		t.Errorf("panic %q does not name the problem handler", msg)
	}
}

// Slice 5 end to end: streaming, header-setting, failing-encoder and direct
// write handlers through one server.
func TestWrap_EndToEnd_Hardened(t *testing.T) {
	t.Parallel()

	errInfinite := errors.New("infinite")

	var r recorder

	h := prblm.NewHandler(
		prblm.WithMappers(
			prblm.Status(errVesselNotFound, http.StatusNotFound),
			prblm.Is(errInfinite, func(error) *prblm.Problem {
				return prblm.New(http.StatusConflict, prblm.WithExt("ratio", math.Inf(1)))
			}),
		),
		prblm.OnError(r.hook),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /stream", h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		_, _ = w.Write([]byte("data: 1\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			return err
		}

		return errVesselNotFound // too late: the stream has started
	}))
	mux.Handle("GET /headers", h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Vary", "Origin")
		w.Header().Set("X-Request-Id", "r-1")
		w.Header().Set("Content-Type", "text/html")

		return errVesselNotFound
	}))
	mux.Handle("GET /broken", h.Wrap(func(http.ResponseWriter, *http.Request) error { return errInfinite }))
	mux.HandleFunc("GET /direct", func(w http.ResponseWriter, req *http.Request) {
		h.Write(w, req, errVesselNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		path   string
		status int
		body   string
		header http.Header // exact values per listed header
	}{
		{path: "/stream", status: 200, body: "data: 1\n"},
		{
			path:   "/headers",
			status: 404,
			body:   `{"title":"Not Found","status":404,"instance":"/headers"}`,
			header: http.Header{
				"Content-Type":  {problemJSONType},
				"Vary":          {"Origin", "Accept"},
				"Cache-Control": {"no-store"},
				"X-Request-Id":  {"r-1"},
			},
		},
		{
			path:   "/broken",
			status: 500,
			body:   `{"title":"Internal Server Error","status":500}`,
			header: http.Header{"Content-Type": {problemJSONType}, "Cache-Control": {"no-store"}},
		},
		{
			path:   "/direct",
			status: 404,
			body:   `{"title":"Not Found","status":404,"instance":"/direct"}`,
			header: http.Header{"Content-Type": {problemJSONType}, "Vary": {"Accept"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			res := get(t, srv, tt.path)

			if res.status != tt.status || string(res.body) != tt.body {
				t.Errorf("got %d %q, want %d %q", res.status, res.body, tt.status, tt.body)
			}

			for name, expected := range tt.header {
				if got := res.header.Values(name); !slices.Equal(got, expected) {
					t.Errorf("%s = %q, want %q", name, got, expected)
				}
			}
		})
	}

	t.Cleanup(func() {
		// One call per request, plus the encoding note for /broken.
		calls := r.recorded()
		if len(calls) != len(tests)+1 {
			t.Errorf("hook called %d times, want %d: %+v", len(calls), len(tests)+1, calls)
		}

		var encoding int

		for _, c := range calls {
			if _, ok := errors.AsType[*json.UnsupportedValueError](c.err); ok {
				encoding++
			}
		}

		if encoding != 1 {
			t.Errorf("hook received %d encoding errors, want 1", encoding)
		}
	})
}

// stdlib-adapter / Scenario: Direct write from a standard handler (XML row).
func TestWrite_XML(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Write(w, r, errVesselNotFound)
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v1/vessels/7", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Accept", "application/xml")

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Type") != "application/problem+xml" {
		t.Errorf("got %d %q, want a 404 application/problem+xml problem", res.StatusCode, res.Header.Get("Content-Type"))
	}

	if !strings.Contains(string(body), `<status>404</status>`) {
		t.Errorf("body = %s, want an XML problem", body)
	}
}

// Slice 6 end to end: one server answering JSON and XML clients.
func TestWrap_EndToEnd_Negotiation(t *testing.T) {
	t.Parallel()

	errMeta := errors.New("meta")

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.Status(errVesselNotFound, http.StatusNotFound),
		prblm.Is(errMeta, func(error) *prblm.Problem {
			return prblm.New(http.StatusConflict, prblm.WithExt("meta", map[string]int{"file.name": 1}))
		}),
	))

	mux := http.NewServeMux()
	mux.Handle("GET /v1/vessels/7", h.Wrap(func(http.ResponseWriter, *http.Request) error { return errVesselNotFound }))
	mux.Handle("GET /v1/meta", h.Wrap(func(http.ResponseWriter, *http.Request) error { return errMeta }))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const notFoundXML = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<problem xmlns="urn:ietf:rfc:7807"><title>Not Found</title><status>404</status><instance>/v1/vessels/7</instance></problem>`

	tests := []struct {
		name        string
		path        string
		accept      string
		status      int
		contentType string
		body        string
	}{
		{name: "json client", path: "/v1/vessels/7", accept: "application/json", status: 404, contentType: problemJSONType, body: `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`},
		{name: "xml client", path: "/v1/vessels/7", accept: "application/xml", status: 404, contentType: "application/problem+xml", body: notFoundXML},
		{name: "browser", path: "/v1/vessels/7", accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", status: 404, contentType: "application/problem+xml", body: notFoundXML},
		{name: "malformed accept", path: "/v1/vessels/7", accept: "application/xml;q=abc", status: 404, contentType: problemJSONType, body: `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`},
		{name: "json client, name xml rejects", path: "/v1/meta", accept: "application/json", status: 409, contentType: problemJSONType, body: `{"title":"Conflict","status":409,"instance":"/v1/meta","meta":{"file.name":1}}`},
		{name: "xml client, name xml rejects", path: "/v1/meta", accept: "application/xml", status: 500, contentType: problemJSONType, body: `{"title":"Internal Server Error","status":500}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+tt.path, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("Accept", tt.accept)

			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = res.Body.Close() }()

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}

			if res.StatusCode != tt.status || res.Header.Get("Content-Type") != tt.contentType || string(body) != tt.body {
				t.Errorf("got %d %q %s\nwant %d %q %s", res.StatusCode, res.Header.Get("Content-Type"), body, tt.status, tt.contentType, tt.body)
			}

			if got := res.Header.Values("Vary"); !slices.Equal(got, []string{"Accept"}) {
				t.Errorf("Vary = %q, want [Accept]", got)
			}
		})
	}
}

// Repeated Accept field lines form one list (RFC 9110, section 5.3).
func TestWrap_RepeatedAccept(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	req.Header.Add("Accept", "image/png")
	req.Header.Add("Accept", "application/xml")

	h.Wrap(func(http.ResponseWriter, *http.Request) error { return errVesselNotFound }).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/problem+xml" {
		t.Errorf("Content-Type = %q, want application/problem+xml from the second Accept line", got)
	}
}

// safe500 is the body of a recovered panic for GET /v1/vessels/7.
const safe500 = `{"title":"Internal Server Error","status":500,"instance":"/v1/vessels/7"}`

// panicking returns a handler that panics with v.
func panicking(v any) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(v) })
}

// serveRecorded serves GET /v1/vessels/7 with handler and returns the
// recorded response.
func serveRecorded(t *testing.T, handler http.Handler, accept string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	handler.ServeHTTP(rec, req)

	return rec
}

// panic-recovery / Scenario: Middleware installed or not.
func TestRecover(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler()

	t.Run("installed", func(t *testing.T) {
		t.Parallel()

		rec := serveRecorded(t, h.Recover(panicking("boom")), "")

		if rec.Code != http.StatusInternalServerError || rec.Body.String() != safe500 {
			t.Errorf("got %d %s, want 500 %s", rec.Code, rec.Body, safe500)
		}

		if got := rec.Header().Get("Content-Type"); got != problemJSONType {
			t.Errorf("Content-Type = %q, want %q", got, problemJSONType)
		}
	})

	t.Run("installed, no panic", func(t *testing.T) {
		t.Parallel()

		ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":7}`)) })
		rec := serveRecorded(t, h.Recover(ok), "")

		if rec.Code != http.StatusOK || rec.Body.String() != `{"id":7}` {
			t.Errorf("got %d %s, want the handler's own 200 response", rec.Code, rec.Body)
		}
	})

	t.Run("only the error-returning adapter", func(t *testing.T) {
		t.Parallel()

		wrapped := h.Wrap(func(http.ResponseWriter, *http.Request) error { panic("boom") })

		if msg := mustPanic(t, func() { serveRecorded(t, wrapped, "") }); msg != "boom" {
			t.Errorf("panic = %q, want the handler's own panic to propagate", msg)
		}
	})

	t.Run("nil next handler", func(t *testing.T) {
		t.Parallel()

		if msg := mustPanic(t, func() { h.Recover(nil) }); !strings.Contains(msg, "next handler") {
			t.Errorf("panic %q does not name the next handler", msg)
		}
	})

	t.Run("nil problem handler", func(t *testing.T) {
		t.Parallel()

		if msg := mustPanic(t, func() { (*prblm.Handler)(nil).Recover(panicking("boom")) }); !strings.Contains(msg, "problem handler") {
			t.Errorf("panic %q does not name the problem handler", msg)
		}
	})
}

// panic-recovery / Scenario: Panic values.
func TestRecover_PanicValues(t *testing.T) {
	t.Parallel()

	catchAll := prblm.MapperFunc(func(context.Context, error) *prblm.Problem {
		return prblm.New(http.StatusTeapot)
	})

	tests := []struct {
		name  string
		value any
		text  string
		opts  []prblm.Option
	}{
		{name: "string", value: "index out of range", text: "index out of range"},
		{name: "error", value: errors.New("db password=hunter2"), text: "hunter2"},
		{name: "number", value: 42, text: "42"},
		{name: "nil", value: nil, text: "nil"},
		// Murphy rows: a panic is never mapped like a returned error.
		{
			name:  "mapped sentinel",
			value: errVesselNotFound,
			text:  "not found",
			opts:  []prblm.Option{prblm.WithMappers(prblm.Status(errVesselNotFound, http.StatusNotFound))},
		},
		{name: "built-in error", value: io.ErrUnexpectedEOF, text: "EOF"},
		{name: "catch-all mapper", value: "boom", text: "boom", opts: []prblm.Option{prblm.WithMappers(catchAll)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(tt.opts...)
			rec := serveRecorded(t, h.Recover(panicking(tt.value)), "")

			if rec.Code != http.StatusInternalServerError || rec.Body.String() != safe500 {
				t.Errorf("got %d %s, want 500 %s", rec.Code, rec.Body, safe500)
			}

			if body := rec.Body.String(); strings.Contains(body, tt.text) || strings.Contains(body, ".go:") {
				t.Errorf("body %s leaks the panic value or a stack frame", body)
			}
		})
	}
}

// loadVessel panics with v; the hook's stack must name it.
func loadVessel(v any) {
	panic(v)
}

// panic-recovery / Scenario: Hook receives the panic.
func TestRecover_Hook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   any
		matches bool // errors.Is(err, io.ErrClosedPipe)
	}{
		{name: "error value", value: io.ErrClosedPipe, matches: true},
		{name: "string value", value: "boom", matches: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu  sync.Mutex
				got []error
			)

			h := prblm.NewHandler(prblm.OnError(func(_ context.Context, err error, _ *prblm.Problem) {
				mu.Lock()
				defer mu.Unlock()

				got = append(got, err)
			}))

			serveRecorded(t, h.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				loadVessel(tt.value)
			})), "")

			mu.Lock()
			defer mu.Unlock()

			if len(got) != 1 {
				t.Fatalf("hook called %d times, want 1", len(got))
			}

			pe, ok := errors.AsType[*prblm.PanicError](got[0])
			if !ok {
				t.Fatalf("hook got %T, want a *prblm.PanicError", got[0])
			}

			if pe.Value != tt.value {
				t.Errorf("Value = %v, want %v", pe.Value, tt.value)
			}

			if !strings.Contains(string(pe.Stack), "loadVessel") {
				t.Errorf("stack does not contain loadVessel:\n%s", pe.Stack)
			}

			if errors.Is(got[0], io.ErrClosedPipe) != tt.matches {
				t.Errorf("errors.Is(err, io.ErrClosedPipe) = %v, want %v", !tt.matches, tt.matches)
			}
		})
	}
}

// panic-recovery / Scenario: Negotiated 500.
func TestRecover_Negotiated(t *testing.T) {
	t.Parallel()

	addTrace := func(_ context.Context, p *prblm.Problem) {
		if err := p.Set("trace_id", "t-1"); err != nil {
			panic(err)
		}
	}

	tests := []struct {
		name        string
		accept      string
		contentType string
		trace       string
	}{
		{name: "no accept", accept: "", contentType: problemJSONType, trace: `"trace_id":"t-1"`},
		{name: "problem xml", accept: "application/problem+xml", contentType: "application/problem+xml", trace: `<trace_id>t-1</trace_id>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithEnrichers(addTrace))
			rec := serveRecorded(t, h.Recover(panicking("boom")), tt.accept)

			if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != tt.contentType {
				t.Errorf("got %d %q, want 500 %q", rec.Code, rec.Header().Get("Content-Type"), tt.contentType)
			}

			if !strings.Contains(rec.Body.String(), tt.trace) {
				t.Errorf("body = %s, want it to contain %s", rec.Body, tt.trace)
			}

			if rec.Header().Get("Vary") != "Accept" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("headers = %v, want Vary: Accept and Cache-Control: no-store", rec.Header())
			}
		})
	}
}

// panic-recovery / Scenario: Abort handler.
func TestRecover_Abort(t *testing.T) {
	t.Parallel()

	t.Run("exact sentinel", func(t *testing.T) {
		t.Parallel()

		var r recorder

		h := prblm.NewHandler(prblm.OnError(r.hook))
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

		var value any

		func() {
			defer func() { value = recover() }()

			h.Recover(panicking(http.ErrAbortHandler)).ServeHTTP(rec, req)
		}()

		//nolint:errorlint // net/http compares the sentinel itself.
		if err, ok := value.(error); !ok || err != http.ErrAbortHandler {
			t.Errorf("re-panicked with %v, want http.ErrAbortHandler", value)
		}

		if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
			t.Errorf("wrote %q with headers %v, want nothing", rec.Body, rec.Header())
		}

		if calls := r.recorded(); len(calls) != 0 {
			t.Errorf("hook called %d times, want 0", len(calls))
		}
	})

	t.Run("wrapped sentinel", func(t *testing.T) {
		t.Parallel()

		h := prblm.NewHandler()
		rec := serveRecorded(t, h.Recover(panicking(fmt.Errorf("x: %w", http.ErrAbortHandler))), "")

		if rec.Code != http.StatusInternalServerError || rec.Body.String() != safe500 {
			t.Errorf("got %d %s, want 500 %s", rec.Code, rec.Body, safe500)
		}
	})
}

// panic-recovery / Scenario: Panic after output or on broken connection.
func TestRecover_Started(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		status  int
		body    string
	}{
		{
			name: "wrote a partial body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"partial":`))

				panic("boom")
			},
			status: 200,
			body:   `{"partial":`,
		},
		{
			name:    "panics before writing anything",
			handler: func(http.ResponseWriter, *http.Request) { panic("boom") },
			status:  500,
			body:    safe500,
		},
		// Murphy rows.
		{
			name: "wrote a status only",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				panic("boom")
			},
			status: 202,
		},
		{
			name: "flushed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_ = http.NewResponseController(w).Flush()

				panic("boom")
			},
			status: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(prblm.OnError(r.hook))
			rec := serveRecorded(t, h.Recover(tt.handler), "")

			if rec.Code != tt.status || rec.Body.String() != tt.body {
				t.Errorf("got %d %q, want %d %q", rec.Code, rec.Body, tt.status, tt.body)
			}

			calls := r.recorded()
			if len(calls) != 1 {
				t.Fatalf("hook called %d times, want 1", len(calls))
			}

			if _, ok := errors.AsType[*prblm.PanicError](calls[0].err); !ok {
				t.Errorf("hook got %v, want the panic", calls[0].err)
			}
		})
	}
}

// headerPanicWriter is a response writer whose Header panics the first
// panics times it is called.
type headerPanicWriter struct {
	http.ResponseWriter

	mu     sync.Mutex
	panics int
}

func (w *headerPanicWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.panics > 0 {
		w.panics--

		panic("writer broke")
	}

	return w.ResponseWriter.Header()
}

// panic-recovery / Scenario: Faulty consumer code during recovery.
func TestRecover_Faulty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []prblm.Option
		panics  int // times the writer's Header panics
		minimal bool
	}{
		{
			name: "an enricher that panics",
			opts: []prblm.Option{prblm.WithEnrichers(func(context.Context, *prblm.Problem) { panic("enricher broke") })},
		},
		{
			name: "a hook that panics",
			opts: []prblm.Option{prblm.OnError(func(context.Context, error, *prblm.Problem) { panic("hook broke") })},
		},
		{
			name: "an encoder that panics",
			opts: []prblm.Option{prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) {
				if err := p.Set("meta", panickyValue{}); err != nil {
					panic(err)
				}
			})},
			minimal: true,
		},
		// Murphy row: the writer itself panics while the problem is written.
		{name: "a writer that panics once", panics: 1, minimal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(tt.opts...)
			recovered := h.Recover(panicking("boom"))

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recovered.ServeHTTP(&headerPanicWriter{ResponseWriter: w, panics: tt.panics}, r)
			}))
			t.Cleanup(srv.Close)

			// The server keeps serving: every request gets the problem.
			for range 2 {
				res := get(t, srv, "/v1/vessels/7")

				if res.status != http.StatusInternalServerError || res.header.Get("Content-Type") != problemJSONType {
					t.Errorf("got %d %q, want a 500 %s", res.status, res.header.Get("Content-Type"), problemJSONType)
				}

				if tt.minimal && string(res.body) != `{"title":"Internal Server Error","status":500}` {
					t.Errorf("body = %s, want the minimal problem", res.body)
				}
			}
		})
	}
}

// A writer that keeps panicking cannot be written to, but no second panic
// escapes the middleware.
func TestRecover_Faulty_WriterAlwaysPanics(t *testing.T) {
	t.Parallel()

	var (
		r          recorder
		mu         sync.Mutex
		noRequests int
	)

	h := prblm.NewHandler(prblm.OnError(func(ctx context.Context, err error, p *prblm.Problem) {
		r.hook(ctx, err, p)

		if _, ok := prblm.RequestFrom(ctx); !ok {
			mu.Lock()
			noRequests++
			mu.Unlock()
		}
	}))
	w := &headerPanicWriter{ResponseWriter: httptest.NewRecorder(), panics: 100}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

	h.Recover(panicking("boom")).ServeHTTP(w, req)

	if calls := r.recorded(); len(calls) == 0 {
		t.Error("hook not called, want the panics reported")
	}

	mu.Lock()
	defer mu.Unlock()

	if noRequests != 0 {
		t.Errorf("%d hook calls had no request in the context, want RequestFrom to work in every call", noRequests)
	}
}

// writeHeaderPanicWriter is a response writer whose WriteHeader panics once.
type writeHeaderPanicWriter struct {
	*httptest.ResponseRecorder

	panicked bool
}

func (w *writeHeaderPanicWriter) WriteHeader(code int) {
	if !w.panicked {
		w.panicked = true

		panic("writer broke")
	}

	w.ResponseRecorder.WriteHeader(code)
}

// When writing the status panics, the status may already be on the wire, so
// the fallback does not write a second response.
func TestRecover_Faulty_WriteHeaderPanics(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler()
	w := &writeHeaderPanicWriter{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)

	h.Recover(panicking("boom")).ServeHTTP(w, req)

	if w.Body.Len() != 0 {
		t.Errorf("wrote %q after the status write failed, want nothing", w.Body)
	}
}

// Slice 7 end to end: panics, an abort and a panic after streaming through
// one server, with Recover around Wrap.
//
//nolint:paralleltest // the subtests run in order: the last checks the server still serves after the panics.
func TestRecover_EndToEnd(t *testing.T) {
	t.Parallel()

	var r recorder

	h := prblm.NewHandler(prblm.OnError(r.hook))

	mux := http.NewServeMux()
	mux.Handle("GET /panic", h.Wrap(func(http.ResponseWriter, *http.Request) error { panic("boom") }))
	mux.Handle("GET /abort", h.Wrap(func(http.ResponseWriter, *http.Request) error { panic(http.ErrAbortHandler) }))
	mux.Handle("GET /stream", h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		_, _ = w.Write([]byte("data: 1\n"))
		_ = http.NewResponseController(w).Flush()

		panic("boom")
	}))
	mux.Handle("GET /ok", h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		_, err := w.Write([]byte("ok"))

		return err
	}))

	srv := httptest.NewUnstartedServer(h.Recover(mux))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)

	t.Run("panic", func(t *testing.T) {
		res := get(t, srv, "/panic")

		if res.status != http.StatusInternalServerError || string(res.body) != `{"title":"Internal Server Error","status":500,"instance":"/panic"}` {
			t.Errorf("got %d %s, want the safe 500 problem", res.status, res.body)
		}
	})

	t.Run("abort", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/abort", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		res, err := srv.Client().Do(req)
		if err == nil {
			_ = res.Body.Close()
			t.Errorf("got status %d, want the connection aborted", res.StatusCode)
		}
	})

	t.Run("panic after streaming", func(t *testing.T) {
		res := get(t, srv, "/stream")

		if res.status != http.StatusOK || string(res.body) != "data: 1\n" {
			t.Errorf("got %d %q, want only the streamed data", res.status, res.body)
		}
	})

	t.Run("keeps serving", func(t *testing.T) {
		if res := get(t, srv, "/ok"); res.status != http.StatusOK || string(res.body) != "ok" {
			t.Errorf("got %d %q, want 200 ok", res.status, res.body)
		}
	})

	t.Cleanup(func() {
		// /panic and /stream are reported; the abort is not.
		var panics int

		for _, c := range r.recorded() {
			if _, ok := errors.AsType[*prblm.PanicError](c.err); ok {
				panics++
			}
		}

		if panics != 2 {
			t.Errorf("hook received %d panics, want 2", panics)
		}
	})
}
