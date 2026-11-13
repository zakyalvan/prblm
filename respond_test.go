package prblm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/internal/xmlenc"
)

// mapping returns a handler whose only mapper returns p for every error.
func mapping(p *prblm.Problem) *prblm.Handler {
	return prblm.NewHandler(prblm.WithMappers(prblm.MapperFunc(func(context.Context, error) *prblm.Problem {
		return p
	})))
}

// problem-response / Scenario: Status consistency.
func TestRespond_Status(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		returned int
		status   int
	}{
		{name: "409", returned: 409, status: 409},
		{name: "404", returned: 404, status: 404},
		{name: "none", returned: 0, status: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := write(t, mapping(&prblm.Problem{Status: tt.returned}), errors.New("x"))

			if rec.Code != tt.status {
				t.Errorf("HTTP status = %d, want %d", rec.Code, tt.status)
			}

			if got := decodeMembers(t, rec.Body.Bytes())["status"]; got != float64(tt.status) {
				t.Errorf("body status = %v, want %d", got, tt.status)
			}
		})
	}
}

// problem-response / Scenario: Headers on the response.
func TestRespond_Headers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing http.Header // set by the handler before it returns the error
		problem  http.Header
		expected http.Header // exact values per listed header
	}{
		{
			name:     "none",
			expected: http.Header{"Vary": {"Accept"}, "Cache-Control": {"no-store"}},
		},
		{
			name:     "existing vary kept",
			existing: http.Header{"Vary": {"Origin"}},
			expected: http.Header{"Vary": {"Origin", "Accept"}, "Cache-Control": {"no-store"}},
		},
		{
			name:     "existing vary accept not duplicated",
			existing: http.Header{"Vary": {"Accept"}},
			expected: http.Header{"Vary": {"Accept"}},
		},
		{
			name:     "existing cache control kept",
			existing: http.Header{"Cache-Control": {"private"}},
			expected: http.Header{"Cache-Control": {"private"}},
		},
		{
			name:     "problem cache control",
			problem:  http.Header{"Cache-Control": {"max-age=60"}},
			expected: http.Header{"Cache-Control": {"max-age=60"}, "Vary": {"Accept"}},
		},
		{
			name:     "problem retry after",
			problem:  http.Header{"Retry-After": {"5"}},
			expected: http.Header{"Retry-After": {"5"}, "Cache-Control": {"no-store"}},
		},
		// Murphy rows.
		{
			name:     "existing vary list with accept in other case",
			existing: http.Header{"Vary": {"origin, accept"}},
			expected: http.Header{"Vary": {"origin, accept"}},
		},
		{
			name:     "problem vary kept",
			problem:  http.Header{"Vary": {"Origin"}},
			expected: http.Header{"Vary": {"Origin", "Accept"}},
		},
		{
			name:     "problem and existing vary accept",
			existing: http.Header{"Vary": {"Accept"}},
			problem:  http.Header{"Vary": {"Accept"}},
			expected: http.Header{"Vary": {"Accept"}},
		},
		{
			name:     "non-canonical problem cache control",
			problem:  http.Header{"cache-control": {"max-age=60"}},
			expected: http.Header{"Cache-Control": {"max-age=60"}},
		},
		{
			name:     "existing cache control wins over the problem's",
			existing: http.Header{"Cache-Control": {"private"}},
			problem:  http.Header{"Cache-Control": {"max-age=60"}},
			expected: http.Header{"Cache-Control": {"private"}},
		},
		{
			name:     "problem header replaces the handler's",
			existing: http.Header{"Retry-After": {"1"}},
			problem:  http.Header{"Retry-After": {"5"}},
			expected: http.Header{"Retry-After": {"5"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := mapping(&prblm.Problem{Status: http.StatusNotFound, Header: tt.problem})
			srv := httptest.NewServer(h.Wrap(func(w http.ResponseWriter, _ *http.Request) error {
				maps.Copy(w.Header(), tt.existing)

				return errors.New("x")
			}))
			t.Cleanup(srv.Close)

			res := get(t, srv, "/v1/vessels/7")

			if res.status != http.StatusNotFound {
				t.Errorf("status = %d, want 404", res.status)
			}

			for name, expected := range tt.expected {
				if got := res.header.Values(name); !slices.Equal(got, expected) {
					t.Errorf("%s = %q, want %q", name, got, expected)
				}
			}
		})
	}
}

// problem-response / Scenario: Protected headers.
func TestRespond_HeadersProtected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header http.Header
	}{
		{name: "content type", header: http.Header{"Content-Type": {"text/html"}}},
		{name: "content length", header: http.Header{"Content-Length": {"1"}}},
		// Murphy rows: keys put straight into the map, bypassing canonicalization.
		{name: "non-canonical content type", header: http.Header{"content-type": {"text/html"}}},
		{name: "non-canonical content length", header: http.Header{"content-length": {"1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := mapping(&prblm.Problem{Status: http.StatusNotFound, Header: tt.header})
			srv := httptest.NewServer(h.Wrap(func(http.ResponseWriter, *http.Request) error {
				return errors.New("x")
			}))
			t.Cleanup(srv.Close)

			res := get(t, srv, "/v1/vessels/7")

			if got := res.header.Values("Content-Type"); !slices.Equal(got, []string{problemJSONType}) {
				t.Errorf("Content-Type = %q, want exactly %q", got, problemJSONType)
			}

			if got, want := res.header.Values("Content-Length"), strconv.Itoa(len(res.body)); !slices.Equal(got, []string{want}) {
				t.Errorf("Content-Length = %q, want exactly the real body length %q", got, want)
			}

			if expected := `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`; string(res.body) != expected {
				t.Errorf("body = %s, want %s", res.body, expected)
			}
		})
	}
}

const problemXMLType = "application/problem+xml"

// decodedProblem returns a problem decoded from JSON, whose extension names
// are not validated.
func decodedProblem(t *testing.T, doc string) *prblm.Problem {
	t.Helper()

	var p prblm.Problem
	if err := json.Unmarshal([]byte(doc), &p); err != nil {
		t.Fatal(err)
	}

	return &p
}

// problem-response / Scenario: Respond without writing (XML row).
func TestRespond_XML(t *testing.T) {
	t.Parallel()

	const xmlHead = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

	full := prblm.New(http.StatusForbidden,
		prblm.WithExt("balance", 30),
		prblm.WithExt("accounts", []string{"/a/1", "/a/2"}),
	)
	full.Type = "https://example.com/probs/out-of-credit"
	full.Title = "You do not have enough credit."
	full.Detail = "Your balance is 30 & that costs 50."

	tests := []struct {
		name        string
		problem     func(t *testing.T) *prblm.Problem
		accept      string
		status      int
		contentType string
		body        string
	}{
		{
			name:        "mapped error",
			problem:     func(*testing.T) *prblm.Problem { return prblm.New(http.StatusNotFound) },
			accept:      "application/xml",
			status:      404,
			contentType: problemXMLType,
			body:        xmlHead + `<problem xmlns="urn:ietf:rfc:7807"><title>Not Found</title><status>404</status><instance>/v1/vessels/7</instance></problem>`,
		},
		// Murphy rows.
		{
			name:        "every member",
			problem:     func(*testing.T) *prblm.Problem { return full },
			accept:      "application/problem+xml",
			status:      403,
			contentType: problemXMLType,
			body: xmlHead + `<problem xmlns="urn:ietf:rfc:7807">` +
				`<type>https://example.com/probs/out-of-credit</type>` +
				`<title>You do not have enough credit.</title><status>403</status>` +
				`<detail>Your balance is 30 &amp; that costs 50.</detail><instance>/v1/vessels/7</instance>` +
				`<accounts><i>/a/1</i><i>/a/2</i></accounts><balance>30</balance></problem>`,
		},
		{
			name: "about:blank omitted",
			problem: func(*testing.T) *prblm.Problem {
				return &prblm.Problem{Type: "about:blank", Status: http.StatusConflict}
			},
			accept:      "text/xml",
			status:      409,
			contentType: problemXMLType,
			body:        xmlHead + `<problem xmlns="urn:ietf:rfc:7807"><title>Conflict</title><status>409</status><instance>/v1/vessels/7</instance></problem>`,
		},
		{
			name:        "unacceptable accept falls back to JSON",
			problem:     func(*testing.T) *prblm.Problem { return prblm.New(http.StatusNotFound) },
			accept:      "image/png",
			status:      404,
			contentType: problemJSONType,
			body:        `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`,
		},
		{
			name: "decoded name invalid in XML",
			problem: func(t *testing.T) *prblm.Problem {
				t.Helper()

				return decodedProblem(t, `{"status":409,"a b":1}`)
			},
			accept:      "application/xml",
			status:      500,
			contentType: problemJSONType,
			body:        `{"title":"Internal Server Error","status":500}`,
		},
		{
			name: "decoded name kept in JSON",
			problem: func(t *testing.T) *prblm.Problem {
				t.Helper()

				return decodedProblem(t, `{"status":409,"a b":1}`)
			},
			accept:      "application/json",
			status:      409,
			contentType: problemJSONType,
			body:        `{"title":"Conflict","status":409,"instance":"/v1/vessels/7","a b":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := mapping(tt.problem(t))
			req := prblm.Request{Method: http.MethodGet, Path: "/v1/vessels/7", Accept: tt.accept}

			res, ok := h.Respond(t.Context(), req, errVesselNotFound)
			if !ok {
				t.Fatal("Respond() reported nothing to write")
			}

			if res.Status != tt.status || res.Header.Get("Content-Type") != tt.contentType {
				t.Errorf("Respond() = %d %q, want %d %q", res.Status, res.Header.Get("Content-Type"), tt.status, tt.contentType)
			}

			if string(res.Body) != tt.body {
				t.Errorf("body = %s\nwant   %s", res.Body, tt.body)
			}

			if got, want := res.Header.Get("Content-Length"), strconv.Itoa(len(res.Body)); got != want {
				t.Errorf("Content-Length = %q, want %q", got, want)
			}

			if res.Header.Get("Vary") != "Accept" || res.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("Vary = %q, Cache-Control = %q; want Accept, no-store", res.Header.Get("Vary"), res.Header.Get("Cache-Control"))
			}
		})
	}
}

// panickyValue is an extension value whose JSON encoding panics.
type panickyValue struct{}

func (panickyValue) MarshalJSON() ([]byte, error) { panic("marshaler broke") }

// problem-response / Scenario: Encoding fails.
func TestRespond_EncodeFailure(t *testing.T) {
	t.Parallel()

	isType := func(err error) bool { _, ok := errors.AsType[*json.UnsupportedTypeError](err); return ok }
	isValue := func(err error) bool { _, ok := errors.AsType[*json.UnsupportedValueError](err); return ok }
	isPanic := func(err error) bool { _, ok := errors.AsType[*prblm.PanicError](err); return ok }

	isName := func(err error) bool { return errors.Is(err, xmlenc.ErrInvalidName) }

	tests := []struct {
		name     string
		accept   string
		ext      string
		value    any
		noHook   bool
		encoding func(error) bool // recognizes the encoding error the hook receives
		panics   bool             // the encoder panics: the note is a *PanicError, not ErrEncoding
	}{
		{name: "chan", accept: "application/json", ext: "bad", value: make(chan int), encoding: isType},
		{name: "xml name", accept: "application/xml", ext: "meta", value: map[string]int{"file.name": 1}, encoding: isName},
		{name: "infinity", ext: "ratio", value: math.Inf(1), encoding: isValue},
		// Murphy rows.
		{name: "panicking marshaler", ext: "meta", value: panickyValue{}, encoding: isPanic, panics: true},
		{name: "panicking marshaler without hook", ext: "meta", value: panickyValue{}, noHook: true, panics: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			opts := []prblm.Option{prblm.WithMappers(prblm.Status(errVesselNotFound, http.StatusConflict))}
			if !tt.noHook {
				opts = append(opts, prblm.OnError(r.hook))
			}

			h := prblm.NewHandler(append(opts, prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) {
				if err := p.Set(tt.ext, tt.value); err != nil {
					t.Errorf("Set(%q) = %v", tt.ext, err)
				}
			}))...)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)
			req.Header.Set("Accept", tt.accept)
			h.Write(rec, req, errVesselNotFound)

			assertMinimalProblem(t, rec)

			if tt.noHook {
				return
			}

			calls := r.recorded()
			if len(calls) != 2 {
				t.Fatalf("hook called %d times, want 2 (encoding error, then the error): %+v", len(calls), calls)
			}

			if got := errors.Is(calls[0].err, prblm.ErrEncoding); got == tt.panics {
				t.Errorf("errors.Is(first hook call, ErrEncoding) = %v, want %v", got, !tt.panics)
			}

			if !tt.encoding(calls[0].err) {
				t.Errorf("first hook call got %v (%T), want the encoding error", calls[0].err, calls[0].err)
			}

			//nolint:errorlint // the last call must carry the original error value itself.
			if last := calls[1]; last.err != errVesselNotFound || last.status != http.StatusInternalServerError {
				t.Errorf("last hook call = %+v, want the original error with the 500 that was sent", last)
			}
		})
	}
}

// assertMinimalProblem checks that rec holds the minimal 500 problem sent
// when the resolved problem could not be encoded.
func assertMinimalProblem(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != problemJSONType {
		t.Errorf("Content-Type = %q, want %q", got, problemJSONType)
	}

	if got := rec.Body.String(); got != `{"title":"Internal Server Error","status":500}` {
		t.Errorf("body = %s, want the minimal problem", got)
	}

	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
		t.Errorf("Content-Length = %q, want %q", got, want)
	}
}

// error-mapping / Scenario: Request context canceled; problem-response /
// Scenario: Respond without writing (canceled row).
func TestRespond_Canceled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		canceled bool
		err      error
		status   int
	}{
		{name: "canceled request, canceled error", canceled: true, err: fmt.Errorf("query: %w", context.Canceled), status: 500},
		{name: "active request, canceled error", canceled: false, err: context.Canceled, status: 500},
		{name: "canceled request, other error", canceled: true, err: errVesselNotFound, status: 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.canceled {
				cancel()
			}

			var r recorder

			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)), prblm.OnError(r.hook))
			res, ok := h.Respond(ctx, prblm.Request{Method: http.MethodGet, Path: "/v1/vessels/7"}, tt.err)

			if !ok || res.Status != tt.status || res.Header.Get("Content-Type") != problemJSONType {
				t.Errorf("Respond() = %d %q, %v; want a %d problem", res.Status, res.Header.Get("Content-Type"), ok, tt.status)
			}

			if calls := r.recorded(); len(calls) != 1 {
				t.Errorf("hook called %d times, want 1", len(calls))
			}
		})
	}
}

// benchmarkRespond measures Respond for a mapped error with the given Accept.
func benchmarkRespond(b *testing.B, accept string) {
	b.Helper()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, http.StatusNotFound)))
	req := prblm.Request{Method: http.MethodGet, Path: "/v1/vessels/7", Accept: accept}
	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		if _, ok := h.Respond(ctx, req, errVesselNotFound); !ok {
			b.Fatal("nothing to write")
		}
	}
}

func BenchmarkRespond_JSON(b *testing.B) {
	for _, accept := range []string{"", "*/*", "application/json"} {
		b.Run("accept="+accept, func(b *testing.B) { benchmarkRespond(b, accept) })
	}
}

func BenchmarkRespond_XML(b *testing.B) {
	// A browser's default Accept prefers application/xml (q=0.9) over */*
	// (q=0.8), so it gets XML.
	for _, accept := range []string{"application/xml", "application/problem+xml", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"} {
		b.Run("accept="+accept, func(b *testing.B) { benchmarkRespond(b, accept) })
	}
}

// problem-response / Scenario: Headers on the response, as adapters merge
// them into a framework's header (design D2 of add-framework-adapters).
func TestResponse_MergeInto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dst      http.Header
		src      http.Header
		expected http.Header
	}{
		{
			name:     "response header replaces the handler's",
			dst:      http.Header{"Content-Type": {"text/plain"}, "X-Request-Id": {"r1"}},
			src:      http.Header{"Content-Type": {"application/problem+json"}},
			expected: http.Header{"Content-Type": {"application/problem+json"}, "X-Request-Id": {"r1"}},
		},
		{
			name:     "vary merged without duplicates",
			dst:      http.Header{"Vary": {"Origin, accept"}},
			src:      http.Header{"Vary": {"Accept"}},
			expected: http.Header{"Vary": {"Origin, accept"}},
		},
		{
			name:     "vary adds accept to the handler's names",
			dst:      http.Header{"Vary": {"Origin"}},
			src:      http.Header{"Vary": {"Accept"}},
			expected: http.Header{"Vary": {"Origin", "Accept"}},
		},
		{
			name:     "handler cache-control kept",
			dst:      http.Header{"Cache-Control": {"max-age=5"}},
			src:      http.Header{"Cache-Control": {"no-store"}},
			expected: http.Header{"Cache-Control": {"max-age=5"}},
		},
		{
			name:     "response cache-control used when the handler set none",
			dst:      http.Header{},
			src:      http.Header{"Cache-Control": {"no-store"}},
			expected: http.Header{"Cache-Control": {"no-store"}},
		},
		{
			name:     "empty response header changes nothing",
			dst:      http.Header{"X-Request-Id": {"r1"}},
			src:      nil,
			expected: http.Header{"X-Request-Id": {"r1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := prblm.Response{Header: tt.src}
			res.MergeInto(tt.dst)

			if !maps.EqualFunc(tt.dst, tt.expected, slices.Equal[[]string]) {
				t.Errorf("MergeInto: header = %v, want %v", tt.dst, tt.expected)
			}
		})
	}
}

func TestResponse_MergeInto_NilDestination(t *testing.T) {
	t.Parallel()

	res := prblm.Response{Header: http.Header{"Content-Type": {"application/problem+json"}}}

	// Nothing to merge into: it must not panic.
	res.MergeInto(nil)
}

func TestResponse_MergeInto_NoAliasing(t *testing.T) {
	t.Parallel()

	res := prblm.Response{Header: http.Header{"X-Trace": {"t1"}, "Cache-Control": {"no-store"}}}
	dst := http.Header{}

	res.MergeInto(dst)

	for name, want := range map[string]string{"X-Trace": "t1", "Cache-Control": "no-store"} {
		dst[name][0] = "changed"

		if got := res.Header.Get(name); got != want {
			t.Errorf("response header %s = %q after changing the destination, want %q", name, got, want)
		}
	}
}
