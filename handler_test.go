package prblm_test

import (
	"context"
	"encoding/json"
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

var errVesselNotFound = errors.New("vessel not found")

// myError is a domain error used to check that type names never leak.
type myError struct{}

func (*myError) Error() string { return "MyError" }

// error-mapping / Scenario: Match or no match.
func TestResolve_Match(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)))

	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{name: "mapped sentinel", err: errVesselNotFound, expected: 404},
		{name: "unrelated error", err: errors.New("other"), expected: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := h.Resolve(t.Context(), tt.err)
			if p == nil {
				t.Fatal("Resolve() = nil, want a problem")
			}

			if p.Status != tt.expected {
				t.Errorf("Resolve().Status = %d, want %d", p.Status, tt.expected)
			}
		})
	}
}

// error-mapping / Scenario: Unmapped error text never leaks.
func TestResolve_Fallback(t *testing.T) {
	t.Parallel()

	var typedNil *myError

	tests := []struct {
		name string
		err  error
		text string
	}{
		{
			name: "database error",
			err:  errors.New("pq: connection refused to 10.0.0.5"),
			text: "10.0.0.5",
		},
		{
			name: "wrapped error with personal data",
			err:  fmt.Errorf("user alice@example.com: %w", io.ErrClosedPipe),
			text: "alice@example.com",
		},
		{
			name: "typed nil in an error",
			err:  typedNil,
			text: "MyError",
		},
	}

	h := prblm.NewHandler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := h.Resolve(t.Context(), tt.err)
			if p == nil {
				t.Fatal("Resolve() = nil, want a problem")
			}

			body, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			if p.Status != 500 {
				t.Errorf("Resolve().Status = %d, want 500", p.Status)
			}

			if expected := `{"title":"Internal Server Error","status":500}`; string(body) != expected {
				t.Errorf("body = %s, want %s", body, expected)
			}

			if strings.Contains(string(body), tt.text) {
				t.Errorf("body %s leaks %q", body, tt.text)
			}
		})
	}
}

// error-mapping / Scenario: Shared problem reused concurrently (the
// "unchanged" part; TestResolve_CopyConcurrent covers the enricher part).
func TestResolve_Copy(t *testing.T) {
	t.Parallel()

	shared := &prblm.Problem{Status: 409, Title: "Conflict"}
	h := mapping(shared)
	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error { return errors.New("x") })

	for _, path := range []string{"/a", "/b"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))

		if want := `{"title":"Conflict","status":409,"instance":"` + path + `"}`; rec.Body.String() != want {
			t.Errorf("body = %s, want %s", rec.Body.String(), want)
		}
	}

	if shared.Instance != "" || shared.Title != "Conflict" || shared.Status != 409 {
		t.Errorf("shared problem was modified: %+v", *shared)
	}

	t.Run("extensions are not shared", func(t *testing.T) {
		t.Parallel()

		shared := prblm.New(409, prblm.WithExt("balance", 30))
		h := mapping(shared)

		resolved := h.Resolve(t.Context(), errors.New("x"))
		if err := resolved.Set("trace_id", "abc"); err != nil {
			t.Fatal(err)
		}

		if _, ok := shared.Ext("trace_id"); ok {
			t.Error("setting an extension on the resolved problem changed the shared problem")
		}

		if v, _ := resolved.Ext("balance"); v != 30 {
			t.Errorf("resolved Ext(balance) = %v, want 30", v)
		}
	})

	t.Run("headers are not shared", func(t *testing.T) {
		t.Parallel()

		shared := prblm.New(503, prblm.WithHeader("Retry-After", "5"))
		h := mapping(shared)

		resolved := h.Resolve(t.Context(), errors.New("x"))
		resolved.Header.Set("Retry-After", "60")

		if got := shared.Header.Get("Retry-After"); got != "5" {
			t.Errorf("shared Retry-After = %q after changing the resolved problem, want 5", got)
		}
	})
}

// error-mapping / Scenario: Out-of-range status from a mapper.
func TestResolve_Normalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		returned int
		expected int
		reported bool
	}{
		{name: "client error kept", returned: 404, expected: 404},
		{name: "missing status", returned: 0, expected: 500},
		{name: "success status", returned: 200, expected: 500, reported: true},
		{name: "redirect status", returned: 302, expected: 500, reported: true},
		{name: "beyond 599", returned: 600, expected: 500, reported: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, tt.returned)), prblm.OnError(r.hook))
			rec := write(t, h, errVesselNotFound)

			if rec.Code != tt.expected {
				t.Errorf("status = %d, want %d", rec.Code, tt.expected)
			}

			if got := decodeMembers(t, rec.Body.Bytes())["status"]; got != float64(tt.expected) {
				t.Errorf("body status = %v, want %d", got, tt.expected)
			}

			calls := r.recorded()

			wantCalls := 1
			if tt.reported {
				wantCalls = 2
			}

			if len(calls) != wantCalls {
				t.Fatalf("hook called %d times, want %d: %+v", len(calls), wantCalls, calls)
			}

			if tt.reported && !strings.Contains(calls[0].err.Error(), strconv.Itoa(tt.returned)) {
				t.Errorf("first hook call %q does not report status %d", calls[0].err, tt.returned)
			}

			if tt.reported && !errors.Is(calls[0].err, prblm.ErrInvalidStatus) {
				t.Errorf("first hook call %v does not wrap ErrInvalidStatus", calls[0].err)
			}

			last := calls[len(calls)-1]
			if errors.Is(last.err, prblm.ErrInvalidStatus) {
				t.Errorf("last hook call %v wraps ErrInvalidStatus, want only the note to", last.err)
			}

			if !errors.Is(last.err, errVesselNotFound) {
				t.Errorf("last hook call got %v, want the original error", last.err)
			}
		})
	}
}

// lockedError describes itself as a 422 problem.
type lockedError struct{}

func (lockedError) Error() string           { return "locked" }
func (lockedError) Problem() *prblm.Problem { return prblm.New(422) }

// silentError is a describer that declines to describe itself.
type silentError struct{}

func (silentError) Error() string           { return "silent" }
func (silentError) Problem() *prblm.Problem { return nil }

// aliasError converts itself to a describer through a custom As method, as
// errors.As allows.
type aliasError struct{}

func (aliasError) Error() string { return "alias" }

func (aliasError) As(target any) bool {
	d, ok := target.(*prblm.Describer)
	if ok {
		*d = lockedError{}
	}

	return ok
}

// error-mapping / Scenario: Problem or describer in the chain.
func TestResolve_SelfDescribing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
		calls    int
	}{
		{name: "problem", err: prblm.New(409), expected: 409},
		{name: "wrapped problem", err: fmt.Errorf("op: %w", prblm.New(409)), expected: 409},
		{name: "describer", err: lockedError{}, expected: 422},
		{name: "outermost wins", err: prblm.New(400, prblm.WithCause(lockedError{})), expected: 400},
		{name: "describer returning nil", err: silentError{}, expected: 500, calls: 1},
		{name: "describer in a joined error", err: errors.Join(io.EOF, lockedError{}), expected: 422},
		{name: "describer through an As method", err: aliasError{}, expected: 422},
		{name: "typed nil problem", err: (*prblm.Problem)(nil), expected: 500, calls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			h := prblm.NewHandler(prblm.WithMappers(prblm.MapperFunc(func(context.Context, error) *prblm.Problem {
				calls++

				return nil
			})))

			if got := h.Resolve(t.Context(), tt.err).Status; got != tt.expected {
				t.Errorf("Status = %d, want %d", got, tt.expected)
			}

			if calls != tt.calls {
				t.Errorf("mapper called %d times, want %d", calls, tt.calls)
			}
		})
	}
}

// A nil mapper registered with the handler is skipped, as in a scope.
func TestWithMappers_NilMapper(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(nil, prblm.Status(errVesselNotFound, 404)))

	if got := h.Resolve(t.Context(), errVesselNotFound).Status; got != 404 {
		t.Errorf("Status = %d, want 404", got)
	}
}

// error-mapping / Scenario: Precedence between steps, Scenario: First
// registered consumer mapper wins.
func TestResolve_Order(t *testing.T) {
	t.Parallel()

	errLockedHere := errors.New("locked")
	syntaxErr := decodeErr(`{"age":}`)
	typeErr := decodeErr(`{"age":"x"}`)

	asSyntax := func(status int) prblm.Mapper {
		return prblm.As(func(*json.SyntaxError) *prblm.Problem { return prblm.New(status) })
	}

	tests := []struct {
		name     string
		mappers  []prblm.Mapper
		scoped   []prblm.Mapper
		err      error
		expected int
	}{
		{
			name:     "first consumer mapper beats a later one and the built-in",
			mappers:  []prblm.Mapper{asSyntax(418), asSyntax(400)},
			err:      syntaxErr,
			expected: 418,
		},
		{
			name: "consumer mapper beats the built-in",
			mappers: []prblm.Mapper{prblm.As(func(*json.UnmarshalTypeError) *prblm.Problem {
				return prblm.New(400)
			})},
			err:      typeErr,
			expected: 400,
		},
		{
			name:     "scoped mapper beats consumer mapper",
			mappers:  []prblm.Mapper{prblm.Status(errLockedHere, 409)},
			scoped:   []prblm.Mapper{prblm.Status(errLockedHere, 423)},
			err:      errLockedHere,
			expected: 423,
		},
		{
			name:     "consumer mapper beats the status hint",
			mappers:  []prblm.Mapper{prblm.Status(errLockedHere, 409)},
			err:      prblm.WithStatus(errLockedHere, 400),
			expected: 409,
		},
		{
			name:     "status hint without mappers",
			err:      prblm.WithStatus(errLockedHere, 400),
			expected: 400,
		},
		{
			name:     "problem in the chain beats consumer mapper",
			mappers:  []prblm.Mapper{prblm.Status(errLockedHere, 409)},
			err:      prblm.New(422, prblm.WithCause(errLockedHere)),
			expected: 422,
		},
		{
			name:     "registration order A, B",
			mappers:  []prblm.Mapper{prblm.Status(errLockedHere, 409), prblm.Status(errLockedHere, 410)},
			err:      errLockedHere,
			expected: 409,
		},
		{
			name:     "registration order B, A",
			mappers:  []prblm.Mapper{prblm.Status(errLockedHere, 410), prblm.Status(errLockedHere, 409)},
			err:      errLockedHere,
			expected: 410,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithMappers(tt.mappers...))
			ctx := prblm.ScopeMappers(t.Context(), tt.scoped...)

			if got := h.Resolve(ctx, tt.err).Status; got != tt.expected {
				t.Errorf("Status = %d, want %d", got, tt.expected)
			}
		})
	}
}

// error-mapping / Scenario: Shared problem reused concurrently.
func TestResolve_CopyConcurrent(t *testing.T) {
	t.Parallel()

	for _, requests := range []int{1, 100} {
		t.Run(strconv.Itoa(requests)+" requests", func(t *testing.T) {
			t.Parallel()

			shared := prblm.New(409)
			h := prblm.NewHandler(
				prblm.WithMappers(prblm.MapperFunc(func(context.Context, error) *prblm.Problem { return shared })),
				prblm.WithEnrichers(traceID),
			)
			handler := h.Wrap(func(http.ResponseWriter, *http.Request) error { return errors.New("x") })

			var wg sync.WaitGroup

			for i := range requests {
				wg.Go(func() {
					id := "trace-" + strconv.Itoa(i)
					ctx := context.WithValue(t.Context(), traceKey{}, id)
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/r/"+id, http.NoBody))

					var members map[string]any
					if err := json.Unmarshal(rec.Body.Bytes(), &members); err != nil {
						t.Errorf("request %d: %v", i, err)

						return
					}

					if members["trace_id"] != id || members["instance"] != "/r/"+id {
						t.Errorf("request %d got trace_id %v and instance %v", i, members["trace_id"], members["instance"])
					}
				})
			}

			wg.Wait()

			if _, ok := shared.Ext("trace_id"); ok || shared.Instance != "" || shared.Title != "" {
				t.Errorf("shared problem was modified: %+v", *shared)
			}
		})
	}
}

// error-mapping / Scenario: Zero options.
func TestHandler_ZeroConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     []prblm.Option
		requests int
	}{
		{name: "no options, one request", requests: 1},
		{name: "no options, 100 requests", requests: 100},
		{name: "a nil option", opts: []prblm.Option{nil}, requests: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(tt.opts...)
			handler := h.Wrap(func(http.ResponseWriter, *http.Request) error { return decodeErr(`{"age":}`) })

			var wg sync.WaitGroup

			for i := range tt.requests {
				wg.Go(func() {
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/vessels", http.NoBody))

					if rec.Code != 400 || rec.Header().Get("Content-Type") != "application/problem+json" {
						t.Errorf("request %d: status %d, Content-Type %q; want a 400 problem", i, rec.Code, rec.Header().Get("Content-Type"))
					}
				})
			}

			wg.Wait()
		})
	}
}
