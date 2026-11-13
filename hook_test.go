package prblm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/kartaladev/prblm"
)

// hookCall is one call of the OnError hook.
type hookCall struct {
	err    error
	status int
}

// recorder records OnError hook calls.
type recorder struct {
	mu    sync.Mutex
	calls []hookCall
}

func (r *recorder) hook(_ context.Context, err error, p *prblm.Problem) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status := 0
	if p != nil {
		status = p.Status
	}

	r.calls = append(r.calls, hookCall{err: err, status: status})
}

func (r *recorder) recorded() []hookCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]hookCall(nil), r.calls...)
}

// write sends err through h.Wrap for GET /v1/vessels/7 and returns the
// recorded HTTP response.
func write(t *testing.T, h *prblm.Handler, err error) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	h.Wrap(func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(rec, req)

	return rec
}

// error-mapping / Scenario: Hook sees the original error.
func TestOnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "unmapped", err: errors.New("pq: connection refused"), status: 500},
		{name: "mapped", err: errVesselNotFound, status: 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errVesselNotFound, 404)), prblm.OnError(r.hook))
			write(t, h, tt.err)

			calls := r.recorded()
			//nolint:errorlint // the hook must receive the original error value itself.
			if len(calls) != 1 || calls[0].err != tt.err || calls[0].status != tt.status {
				t.Errorf("hook calls = %+v, want one call with %v and status %d", calls, tt.err, tt.status)
			}
		})
	}
}

// error-mapping / Scenario: Nil error.
func TestNilError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "nil", err: nil},
		{name: "hint on nil", err: prblm.WithStatus(nil, 404)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(prblm.OnError(r.hook))

			// problem-response / Scenario: Respond without writing (nil row).
			req := prblm.Request{Method: http.MethodGet, Path: "/v1/vessels/7"}
			if res, ok := h.Respond(t.Context(), req, tt.err); ok {
				t.Errorf("Respond(nil) = %+v, true; want nothing to write", res)
			}

			rec := write(t, h, tt.err)

			if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
				t.Errorf("wrote %q with headers %v, want nothing", rec.Body.String(), rec.Header())
			}

			if calls := r.recorded(); len(calls) != 0 {
				t.Errorf("hook called %d times, want 0", len(calls))
			}

			if p := h.Resolve(t.Context(), tt.err); p != nil {
				t.Errorf("Resolve(nil) = %+v, want nil", p)
			}
		})
	}
}

// panickyError is an error whose self-description panics; it wraps
// errVesselNotFound.
type panickyError struct{}

func (panickyError) Error() string           { return "panicky" }
func (panickyError) Unwrap() error           { return errVesselNotFound }
func (panickyError) Problem() *prblm.Problem { panic("describer broke") }

// error-mapping / Scenario: Consumer code panics.
func TestResolve_FaultyConsumer(t *testing.T) {
	t.Parallel()

	panicking := prblm.MapperFunc(func(context.Context, error) *prblm.Problem { panic("mapper broke") })
	conflict := prblm.Status(errVesselNotFound, 409)

	tests := []struct {
		name        string
		opts        []prblm.Option
		err         error
		hookPanics  bool
		panicReport bool // the hook first receives a *PanicError
	}{
		{name: "panicking mapper", opts: []prblm.Option{prblm.WithMappers(panicking, conflict)}, err: errVesselNotFound, panicReport: true},
		{name: "panicking describer", opts: []prblm.Option{prblm.WithMappers(conflict)}, err: panickyError{}, panicReport: true},
		{
			name: "panicking enricher",
			opts: []prblm.Option{prblm.WithMappers(conflict), prblm.WithEnrichers(func(context.Context, *prblm.Problem) {
				panic("enricher broke")
			})},
			err:         errVesselNotFound,
			panicReport: true,
		},
		{name: "panicking hook", opts: []prblm.Option{prblm.WithMappers(conflict)}, err: errVesselNotFound, hookPanics: true},
		{name: "nil mapper", opts: []prblm.Option{prblm.WithMappers(nil, conflict)}, err: errVesselNotFound},
		{name: "typed-nil mapper func", opts: []prblm.Option{prblm.WithMappers(prblm.MapperFunc(nil), conflict)}, err: errVesselNotFound},
		{name: "nil enricher", opts: []prblm.Option{prblm.WithMappers(conflict), prblm.WithEnrichers(nil)}, err: errVesselNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			hook := r.hook
			if tt.hookPanics {
				hook = func(ctx context.Context, err error, p *prblm.Problem) {
					r.hook(ctx, err, p)
					panic("hook broke")
				}
			}

			h := prblm.NewHandler(append(tt.opts, prblm.OnError(hook))...)
			rec := write(t, h, tt.err)

			if rec.Code != 409 {
				t.Errorf("status = %d, want 409", rec.Code)
			}

			calls := r.recorded()

			want := 1
			if tt.panicReport {
				want = 2

				if len(calls) == 2 {
					if _, ok := errors.AsType[*prblm.PanicError](calls[0].err); !ok {
						t.Errorf("first hook call got %v, want a *prblm.PanicError", calls[0].err)
					}
				}
			}

			if len(calls) != want {
				t.Fatalf("hook called %d times, want %d: %+v", len(calls), want, calls)
			}

			//nolint:errorlint // the last call must carry the original error value itself.
			if last := calls[len(calls)-1].err; last != tt.err {
				t.Errorf("last hook call got %v, want %v", last, tt.err)
			}
		})
	}
}

// Without a hook, a panicking mapper is still contained.
func TestResolve_FaultyConsumer_NoHook(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.MapperFunc(func(context.Context, error) *prblm.Problem { panic("mapper broke") }),
		prblm.Status(errVesselNotFound, 409),
	))

	if rec := write(t, h, errVesselNotFound); rec.Code != 409 {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}
