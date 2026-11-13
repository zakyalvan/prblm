package prblm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

var errRead = errors.New("read failed")

// trackedBody is a response body that records whether it was closed.
type trackedBody struct {
	io.Reader

	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true

	return nil
}

// paddedJSON returns a JSON problem with status 404 of exactly size bytes.
func paddedJSON(size int) io.Reader {
	const head, tail = `{"status":404,"pad":"`, `"}`

	return strings.NewReader(head + strings.Repeat("x", size-len(head)-len(tail)) + tail)
}

// failingReader fails every read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errRead }

// problem-document / Scenario: Parse response.
func TestParse(t *testing.T) {
	t.Parallel()

	isNotProblem := func(err error) bool { return errors.Is(err, prblm.ErrNotProblem) }
	isRead := func(err error) bool { return errors.Is(err, errRead) }
	isError := func(err error) bool { return err != nil && !errors.Is(err, prblm.ErrNotProblem) }
	isTooLarge := func(err error) bool { return errors.Is(err, prblm.ErrTooLarge) }

	tests := []struct {
		name        string
		contentType string
		body        io.Reader
		status      int
		refused     func(error) bool // nil when the response parses
	}{
		// Examples: parsed.
		{name: "json", contentType: "application/problem+json", body: strings.NewReader(`{"status":404,"title":"Not Found"}`), status: 404},
		{name: "json with charset", contentType: "application/problem+json; charset=utf-8", body: strings.NewReader(`{"status":409}`), status: 409},
		{name: "xml", contentType: "application/problem+xml", body: strings.NewReader(`<problem xmlns="urn:ietf:rfc:7807"><status>404</status></problem>`), status: 404},
		// Examples: refused.
		{name: "html", contentType: "text/html", body: strings.NewReader(`<html></html>`), refused: isNotProblem},
		{name: "no content type", contentType: "", body: strings.NewReader(`{"status":404}`), refused: isNotProblem},
		{name: "body read fails", contentType: "application/problem+json", body: failingReader{}, refused: isRead},
		{name: "truncated json", contentType: "application/problem+json", body: strings.NewReader(`{"status":`), refused: isError},
		// Murphy rows.
		{name: "media type in other case", contentType: "Application/Problem+JSON", body: strings.NewReader(`{"status":410}`), status: 410},
		{name: "malformed content type", contentType: "application/problem+json;;", body: strings.NewReader(`{"status":404}`), refused: isNotProblem},
		{name: "plain json", contentType: "application/json", body: strings.NewReader(`{"status":404}`), refused: isNotProblem},
		{name: "not a problem element", contentType: "application/problem+xml", body: strings.NewReader(`<other/>`), refused: isError},
		// Murphy rows: the 1 MiB limit, at the boundary.
		{name: "body of exactly 1 MiB", contentType: "application/problem+json", body: paddedJSON(1 << 20), status: 404},
		{name: "body of 1 MiB and a byte", contentType: "application/problem+json", body: paddedJSON(1<<20 + 1), refused: isTooLarge},
		{
			// Cut at the limit, this would still decode.
			name:        "complete xml followed by padding",
			contentType: "application/problem+xml",
			body:        strings.NewReader(`<problem><status>404</status></problem>` + strings.Repeat(" ", 1<<20)),
			refused:     isTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := &trackedBody{Reader: tt.body}
			res := &http.Response{StatusCode: tt.status, Header: http.Header{}, Body: body}

			if tt.contentType != "" {
				res.Header.Set("Content-Type", tt.contentType)
			}

			p, err := prblm.Parse(res)

			if !body.closed {
				t.Error("body not closed")
			}

			if tt.refused != nil {
				if p != nil || !tt.refused(err) {
					t.Errorf("Parse() = %+v, %v; want the refusal", p, err)
				}

				return
			}

			if err != nil || p == nil || p.Status != tt.status {
				t.Errorf("Parse() = %+v, %v; want status %d", p, err, tt.status)
			}
		})
	}
}

// Parse refuses a nil response or body instead of panicking.
func TestParse_Nil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		res  *http.Response
	}{
		{name: "nil response", res: nil},
		{name: "nil body", res: &http.Response{Header: http.Header{"Content-Type": {"application/problem+json"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if p, err := prblm.Parse(tt.res); p != nil || err == nil {
				t.Errorf("Parse() = %+v, %v; want an error", p, err)
			}
		})
	}
}

// The whole library end to end: a server using every feature, and a client
// parsing its answers in both formats.
func TestParse_EndToEnd(t *testing.T) {
	t.Parallel()

	errLocked := errors.New("locked")
	errBusy := errors.New("busy")

	var r recorder

	h := prblm.NewHandler(
		prblm.WithMappers(
			prblm.Status(errVesselNotFound, http.StatusNotFound),
			prblm.Is(errBusy, func(error) *prblm.Problem {
				return prblm.New(http.StatusServiceUnavailable, prblm.WithHeader("Retry-After", "5"), prblm.WithExt("retry_in", 5))
			}),
		),
		prblm.WithEnrichers(traceID),
		prblm.OnError(r.hook),
	)

	routes := map[string]error{
		"/v1/contracts/locked":  errLocked,
		"/v1/vessels/missing":   errVesselNotFound,
		"/v1/vessels/busy":      errBusy,
		"/v1/vessels/bad-body":  decodeErr(`{"age":}`),
		"/v1/vessels/forbidden": prblm.WithStatus(errors.New("tenant 42"), http.StatusForbidden),
		"/v1/vessels/broken":    errors.New("pq: connection refused to 10.0.0.5"),
	}

	fail := h.Wrap(func(_ http.ResponseWriter, req *http.Request) error {
		if req.URL.Path == "/v1/vessels/panic" {
			panic("index out of range")
		}

		return routes[req.URL.Path]
	})

	mux := http.NewServeMux()
	mux.Handle("/v1/contracts/", scoped(fail, prblm.Status(errLocked, http.StatusLocked)))
	mux.Handle("/v1/vessels/", fail)
	mux.HandleFunc("/v1/direct", func(w http.ResponseWriter, req *http.Request) { h.Write(w, req, errVesselNotFound) })

	// The trace middleware is outside Recover, so recovered panics see it.
	recovered := h.Recover(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recovered.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), traceKey{}, "t-1")))
	}))
	t.Cleanup(srv.Close)

	tests := []endToEndCase{
		{path: "/v1/contracts/locked", status: 423},
		{path: "/v1/vessels/missing", status: 404},
		{path: "/v1/vessels/busy", status: 503, retryAfter: "5"},
		{path: "/v1/vessels/bad-body", status: 400, detail: "The request body is not valid JSON."},
		{path: "/v1/vessels/forbidden", status: 403},
		{path: "/v1/vessels/broken", status: 500},
		{path: "/v1/vessels/panic", status: 500},
		{path: "/v1/direct", status: 404},
	}

	for _, tt := range tests {
		for _, accept := range []string{"application/json", "application/xml"} {
			t.Run(tt.path+" "+accept, func(t *testing.T) {
				t.Parallel()

				checkEndToEnd(t, srv, accept, tt)
			})
		}
	}

	t.Cleanup(func() {
		if calls := r.recorded(); len(calls) != 2*len(tests) {
			t.Errorf("hook called %d times, want %d", len(calls), 2*len(tests))
		}
	})
}

// endToEndCase is what a client must parse from one path of the end-to-end
// server.
type endToEndCase struct {
	path       string
	status     int
	detail     string
	retryAfter string
}

// checkEndToEnd requests tt.path with accept and checks what Parse returns.
func checkEndToEnd(t *testing.T, srv *httptest.Server, accept string, tt endToEndCase) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+tt.path+"?token=secret", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Accept", accept)

	res, err := srv.Client().Do(req) //nolint:bodyclose // Parse closes the body.
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != tt.status || res.Header.Get("Retry-After") != tt.retryAfter {
		t.Errorf("got %d, Retry-After %q; want %d, %q", res.StatusCode, res.Header.Get("Retry-After"), tt.status, tt.retryAfter)
	}

	p, err := prblm.Parse(res)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if p.Status != tt.status || p.Detail != tt.detail || p.Instance != tt.path {
		t.Errorf("parsed status %d, detail %q, instance %q; want %d, %q, %q", p.Status, p.Detail, p.Instance, tt.status, tt.detail, tt.path)
	}

	if v, ok := p.Ext("trace_id"); !ok || fmt.Sprint(v) != "t-1" {
		t.Errorf("trace_id = %v, want t-1", v)
	}

	assertNoLeak(t, p, "10.0.0.5", "tenant", "index out of range", "secret")
}

// assertNoLeak fails when the encoded p contains any of secrets.
func assertNoLeak(t *testing.T, p *prblm.Problem, secrets ...string) {
	t.Helper()

	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("problem %s leaks %q", encoded, secret)
		}
	}
}
