package prblm_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kartaladev/prblm"
)

// VesselMissingError is a domain error that reports itself as
// prblm.ErrNotFound through its Is method.
type VesselMissingError struct {
	ID    int
	Owner string
}

func (e *VesselMissingError) Error() string {
	return fmt.Sprintf("vessel %d of %s is missing", e.ID, e.Owner)
}

func (e *VesselMissingError) Is(target error) bool { return target == prblm.ErrNotFound }

// vesselRequest returns GET /v1/vessels/7 with ctx and the given Accept
// header (none when empty).
func vesselRequest(ctx context.Context, accept string) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/vessels/7", http.NoBody)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	return req
}

// writeAccept sends err through h.Wrap for GET /v1/vessels/7 with the given
// Accept header (none when empty) and returns the recorded response.
func writeAccept(t *testing.T, h *prblm.Handler, accept string, err error) *httptest.ResponseRecorder {
	t.Helper()

	return answerRequest(h, err, vesselRequest(t.Context(), accept))
}

// error-sentinels / Scenario: Sentinel returned directly.
func TestSentinels_Direct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
		title    string
		detail   string
	}{
		{name: "invalid input", err: prblm.ErrInvalidInput, expected: 400, title: "Bad Request", detail: "The request is invalid."},
		{name: "unauthenticated", err: prblm.ErrUnauthenticated, expected: 401, title: "Unauthorized", detail: "Authentication is required."},
		{name: "permission denied", err: prblm.ErrPermissionDenied, expected: 403, title: "Forbidden", detail: "You are not allowed to perform this action."},
		{name: "not found", err: prblm.ErrNotFound, expected: 404, title: "Not Found", detail: "The requested resource was not found."},
		{name: "conflict", err: prblm.ErrConflict, expected: 409, title: "Conflict", detail: "The request conflicts with the current state of the resource."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			formats := []struct {
				accept    string
				mediaType string
				unmarshal func([]byte, any) error
			}{
				{mediaType: "application/problem+json", unmarshal: json.Unmarshal},
				{accept: "application/problem+xml", mediaType: "application/problem+xml", unmarshal: xml.Unmarshal},
			}

			for _, f := range formats {
				rec := writeAccept(t, prblm.NewHandler(), f.accept, tt.err)
				if ct := rec.Header().Get("Content-Type"); rec.Code != tt.expected || ct != f.mediaType {
					t.Errorf("response %d %q, want %d %q", rec.Code, ct, tt.expected, f.mediaType)
				}

				var p prblm.Problem
				if err := f.unmarshal(rec.Body.Bytes(), &p); err != nil {
					t.Fatalf("body %q is not a problem: %v", rec.Body, err)
				}

				if p.EffectiveType() != "about:blank" || p.Title != tt.title || p.Detail != tt.detail {
					t.Errorf("body = %s, want type about:blank, title %q, detail %q", rec.Body, tt.title, tt.detail)
				}
			}
		})
	}
}

// error-sentinels / Scenario: Wrapped, joined and custom errors.
func TestSentinels_Chain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{name: "wrapped", err: fmt.Errorf("vessel 7: %w", prblm.ErrNotFound), expected: 404},
		{name: "wrapped twice", err: fmt.Errorf("load: %w", fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)), expected: 404},
		{name: "joined", err: errors.Join(io.EOF, prblm.ErrNotFound), expected: 404},
		{name: "custom is", err: &VesselMissingError{ID: 7}, expected: 404},
		{name: "wrapped custom is", err: fmt.Errorf("op: %w", &VesselMissingError{ID: 7}), expected: 404},
		{name: "same text", err: errors.New("prblm: not found"), expected: 500},
		{name: "similar text", err: errors.New("not found"), expected: 500},
		//nolint:errorlint // %v breaks the chain on purpose: the look-alike must not match.
		{name: "formatted with %v", err: fmt.Errorf("vessel 7: %v", prblm.ErrNotFound), expected: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if rec := writeAccept(t, prblm.NewHandler(), "", tt.err); rec.Code != tt.expected {
				t.Errorf("status = %d, want %d", rec.Code, tt.expected)
			}
		})
	}
}

// error-sentinels / Scenario: Context added by the application stays private.
func TestSentinels_NoLeak(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		accept    string
		expected  int
		forbidden []string
	}{
		{
			name:      "not found json",
			err:       fmt.Errorf("vessel 7 of alice@example.com: %w", prblm.ErrNotFound),
			expected:  404,
			forbidden: []string{"alice@example.com", "vessel 7", "prblm:"},
		},
		{
			name:      "not found xml",
			err:       fmt.Errorf("vessel 7 of alice@example.com: %w", prblm.ErrNotFound),
			accept:    "application/problem+xml",
			expected:  404,
			forbidden: []string{"alice@example.com", "vessel 7", "prblm:"},
		},
		{
			name:      "permission denied json",
			err:       fmt.Errorf("user alice@example.com: %w", prblm.ErrPermissionDenied),
			expected:  403,
			forbidden: []string{"alice@example.com", "prblm:"},
		},
		{
			name:      "permission denied xml",
			err:       fmt.Errorf("user alice@example.com: %w", prblm.ErrPermissionDenied),
			accept:    "application/problem+xml",
			expected:  403,
			forbidden: []string{"alice@example.com", "prblm:"},
		},
		{
			name:      "custom error fields",
			err:       &VesselMissingError{ID: 7, Owner: "alice"},
			expected:  404,
			forbidden: []string{"alice", "VesselMissingError"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := writeAccept(t, prblm.NewHandler(), tt.accept, tt.err)
			if rec.Code != tt.expected {
				t.Errorf("status = %d, want %d", rec.Code, tt.expected)
			}

			for _, f := range tt.forbidden {
				if strings.Contains(rec.Body.String(), f) {
					t.Errorf("body %s contains %q", rec.Body, f)
				}
			}
		})
	}
}

// error-sentinels / Scenario: Logging the full error.
func TestSentinels_Hook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		text     string
		expected int
	}{
		{name: "wrapped not found", err: fmt.Errorf("vessel 7: %w", prblm.ErrNotFound), text: "vessel 7: prblm: not found", expected: 404},
		{name: "conflict", err: prblm.ErrConflict, text: "prblm: conflict", expected: 409},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var rec recorder

			writeAccept(t, prblm.NewHandler(prblm.OnError(rec.hook)), "", tt.err)

			calls := rec.recorded()
			if len(calls) != 1 || calls[0].err.Error() != tt.text || calls[0].status != tt.expected {
				t.Errorf("hook calls = %v, want one with %q and status %d", calls, tt.text, tt.expected)
			}
		})
	}
}

// error-sentinels / Scenario: Error text.
func TestSentinels_ErrorText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		text string
	}{
		{name: "invalid input", err: prblm.ErrInvalidInput, text: "prblm: invalid input"},
		{name: "unauthenticated", err: prblm.ErrUnauthenticated, text: "prblm: unauthenticated"},
		{name: "permission denied", err: prblm.ErrPermissionDenied, text: "prblm: permission denied"},
		{name: "not found", err: prblm.ErrNotFound, text: "prblm: not found"},
		{name: "conflict", err: prblm.ErrConflict, text: "prblm: conflict"},
	}

	seen := make(map[string]bool, len(tests))

	for _, tt := range tests {
		got := tt.err.Error()
		if got != tt.text {
			t.Errorf("%s: Error() = %q, want %q", tt.name, got, tt.text)
		}

		if !strings.HasPrefix(got, "prblm: ") {
			t.Errorf("%s: Error() = %q, want the prefix %q", tt.name, got, "prblm: ")
		}

		if seen[got] {
			t.Errorf("%s: Error() = %q is not unique", tt.name, got)
		}

		seen[got] = true
	}
}

// lockedVesselError is a describer with status 409 that reports itself as
// prblm.ErrNotFound.
type lockedVesselError struct{}

func (lockedVesselError) Error() string           { return "vessel 7 is locked" }
func (lockedVesselError) Problem() *prblm.Problem { return prblm.New(409) }
func (lockedVesselError) Is(target error) bool    { return target == prblm.ErrNotFound }

// decliningVesselError is a describer that declines and reports itself as
// prblm.ErrNotFound.
type decliningVesselError struct{}

func (decliningVesselError) Error() string           { return "vessel 7" }
func (decliningVesselError) Problem() *prblm.Problem { return nil }
func (decliningVesselError) Is(target error) bool    { return target == prblm.ErrNotFound }

// error-sentinels / Scenario: Overriding the default problem.
func TestSentinels_Override(t *testing.T) {
	t.Parallel()

	const vesselType = "https://example.com/probs/vessel-not-found"

	wrapped := fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)

	tests := []struct {
		name     string
		opts     []prblm.Option
		scoped   []prblm.Mapper
		err      error
		expected int
		typ      string
		panics   bool
	}{
		{
			name: "consumer is mapper",
			opts: []prblm.Option{prblm.WithMappers(prblm.Is(prblm.ErrNotFound, func(error) *prblm.Problem {
				return prblm.New(404, prblm.WithType(vesselType))
			}))},
			err: wrapped, expected: 404, typ: vesselType,
		},
		{
			name: "consumer status mapper",
			opts: []prblm.Option{prblm.WithMappers(prblm.Status(prblm.ErrNotFound, 410))},
			err:  wrapped, expected: 410, typ: "about:blank",
		},
		{
			name:   "scoped mapper",
			scoped: []prblm.Mapper{prblm.Status(prblm.ErrNotFound, 410)},
			err:    wrapped, expected: 410, typ: "about:blank",
		},
		{
			name: "problem wrapping the sentinel",
			err:  prblm.New(422, prblm.WithCause(wrapped)), expected: 422, typ: "about:blank",
		},
		{
			name: "describer reporting the sentinel",
			err:  lockedVesselError{}, expected: 409, typ: "about:blank",
		},
		{
			name: "consumer mapper declines",
			opts: []prblm.Option{prblm.WithMappers(prblm.Is(prblm.ErrNotFound, func(error) *prblm.Problem { return nil }))},
			err:  wrapped, expected: 404, typ: "about:blank",
		},
		{
			name: "consumer mapper panics",
			opts: []prblm.Option{prblm.WithMappers(prblm.MapperFunc(func(context.Context, error) *prblm.Problem {
				panic("mapper broke")
			}))},
			err: wrapped, expected: 404, typ: "about:blank", panics: true,
		},
		{
			name: "describer declines",
			err:  decliningVesselError{}, expected: 404, typ: "about:blank",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook recorder

			ctx := t.Context()
			if tt.scoped != nil {
				ctx = prblm.ScopeMappers(ctx, tt.scoped...)
			}

			h := prblm.NewHandler(append(tt.opts, prblm.OnError(hook.hook))...)

			rec := answerRequest(h, tt.err, vesselRequest(ctx, ""))
			if rec.Code != tt.expected {
				t.Errorf("status = %d, want %d", rec.Code, tt.expected)
			}

			if typ := decodedProblem(t, rec.Body.String()).EffectiveType(); typ != tt.typ {
				t.Errorf("type = %q, want %q", typ, tt.typ)
			}

			panics := 0
			if tt.panics {
				panics = 1
			}

			assertHookCalls(t, hook.recorded(), tt.err, tt.expected, panics)
		})
	}
}

// anyPanics asks assertHookCalls for one or more panic notes.
const anyPanics = -1

// assertHookCalls checks that the hook received panics panic notes (one or
// more for anyPanics) and then err itself, with a problem of status.
func assertHookCalls(t *testing.T, calls []hookCall, err error, status, panics int) {
	t.Helper()

	if len(calls) == 0 {
		t.Fatal("hook not called")
	}

	notes, last := calls[:len(calls)-1], calls[len(calls)-1]

	if panics == anyPanics && len(notes) == 0 || panics != anyPanics && len(notes) != panics {
		t.Errorf("hook notes = %v, want %d panic notes (-1: one or more)", notes, panics)
	}

	for _, n := range notes {
		if _, ok := errors.AsType[*prblm.PanicError](n.err); !ok {
			t.Errorf("hook note = %v, want a panic error", n.err)
		}
	}

	//nolint:errorlint // the last call must carry the original error value itself.
	if last.err != err || last.status != status {
		t.Errorf("last hook call = %v, want the returned error with status %d", last, status)
	}
}

// error-sentinels / Scenario: Sentinel and hint together.
func TestSentinels_Hint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     []prblm.Option
		err      error
		expected int
		detail   string
	}{
		{name: "hint outside", err: prblm.WithStatus(prblm.ErrNotFound, 400), expected: 404, detail: "The requested resource was not found."},
		{
			name:     "hint inside",
			err:      fmt.Errorf("x: %w", prblm.WithStatus(prblm.ErrConflict, 422)),
			expected: 409,
			detail:   "The request conflicts with the current state of the resource.",
		},
		{name: "hint not an error status", err: prblm.WithStatus(prblm.ErrNotFound, 200), expected: 404, detail: "The requested resource was not found."},
		{name: "built-ins disabled", opts: []prblm.Option{prblm.WithoutBuiltins()}, err: prblm.WithStatus(prblm.ErrNotFound, 400), expected: 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertStatusDetail(t, writeAccept(t, prblm.NewHandler(tt.opts...), "", tt.err), tt.expected, tt.detail)
		})
	}
}

// assertStatusDetail checks the status and the detail of a JSON problem
// response.
func assertStatusDetail(t *testing.T, rec *httptest.ResponseRecorder, status int, detail string) {
	t.Helper()

	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}

	if got := decodedProblem(t, rec.Body.String()).Detail; got != detail {
		t.Errorf("detail = %q, want %q", got, detail)
	}
}

// error-sentinels / Scenario: Built-ins disabled.
func TestSentinels_WithoutBuiltins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mappers  []prblm.Mapper
		err      error
		expected int
	}{
		{name: "no mappers", err: prblm.ErrNotFound, expected: 500},
		{name: "consumer mapper", mappers: []prblm.Mapper{prblm.Status(prblm.ErrNotFound, 404)}, err: prblm.ErrNotFound, expected: 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithoutBuiltins(), prblm.WithMappers(tt.mappers...))
			assertStatusDetail(t, writeAccept(t, h, "", tt.err), tt.expected, "")
		})
	}
}

// error-sentinels / Scenario: Several matches in one chain.
func TestSentinels_Order(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
		detail   string
	}{
		{name: "two sentinels", err: errors.Join(prblm.ErrConflict, prblm.ErrNotFound), expected: 404, detail: "The requested resource was not found."},
		{name: "sentinel and json syntax error", err: fmt.Errorf("%w: %w", prblm.ErrInvalidInput, decodeErr(`{"age":}`)), expected: 400, detail: "The request is invalid."},
		{name: "bare deadline", err: context.DeadlineExceeded, expected: 504},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertStatusDetail(t, writeAccept(t, prblm.NewHandler(), "", tt.err), tt.expected, tt.detail)
		})
	}
}

// panickyIsError is an error whose Is method panics.
type panickyIsError struct{}

func (panickyIsError) Error() string { return "panicky is" }
func (panickyIsError) Is(error) bool { panic("is broke") }

// error-sentinels / Scenario: Degenerate inputs.
func TestSentinels_Degenerate(t *testing.T) {
	t.Parallel()

	var typedNil *myError

	tests := []struct {
		name    string
		err     error
		written bool
		panics  bool
	}{
		{name: "nil", err: nil},
		{name: "hint on nil", err: prblm.WithStatus(nil, 404)},
		{name: "typed nil", err: typedNil, written: true},
		{name: "panicking is", err: panickyIsError{}, written: true, panics: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook recorder

			rec := writeAccept(t, prblm.NewHandler(prblm.OnError(hook.hook)), "", tt.err)
			calls := hook.recorded()

			if !tt.written {
				if rec.Body.Len() != 0 || len(calls) != 0 {
					t.Errorf("response %d %q, hook calls %v, want nothing", rec.Code, rec.Body, calls)
				}

				return
			}

			assertStatusDetail(t, rec, 500, "")

			panics := 0
			if tt.panics {
				panics = anyPanics
			}

			assertHookCalls(t, calls, tt.err, 500, panics)
		})
	}
}

// error-sentinels / Scenario: Concurrent requests.
func TestSentinels_Concurrent(t *testing.T) {
	t.Parallel()

	for _, n := range []int{1, 100} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithEnrichers(traceID))

			var wg sync.WaitGroup

			for i := range n {
				wg.Go(func() {
					id := 90000 + i
					trace := "t-" + strconv.Itoa(i)
					ctx := context.WithValue(t.Context(), traceKey{}, trace)

					rec := answerRequest(h, fmt.Errorf("vessel %d: %w", id, prblm.ErrNotFound), vesselRequest(ctx, ""))
					m := decodeMembers(t, rec.Body.Bytes())

					if rec.Code != 404 || m["trace_id"] != trace {
						t.Errorf("request %d: status %d, trace_id %v, want 404 and %q", i, rec.Code, m["trace_id"], trace)
					}

					if strings.Contains(rec.Body.String(), strconv.Itoa(id)) {
						t.Errorf("request %d: body %s contains the vessel id", i, rec.Body)
					}
				})
			}

			wg.Wait()
		})
	}
}
