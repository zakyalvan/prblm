package prblm_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/kartaladev/prblm"
)

var errLocked = errors.New("locked")

// scoped is a middleware that attaches mappers to the request context.
func scoped(next http.Handler, mappers ...prblm.Mapper) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(prblm.ScopeMappers(r.Context(), mappers...)))
	})
}

// error-mapping / Scenario: Scoped only on its route.
func TestScopeMappers(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler()
	fail := h.Wrap(func(http.ResponseWriter, *http.Request) error { return errLocked })

	mux := http.NewServeMux()
	mux.Handle("/v1/contracts/", scoped(fail, prblm.Status(errLocked, 423)))
	mux.Handle("/v1/vessels/", fail)

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{name: "inside the scope", path: "/v1/contracts/1", status: 423},
		{name: "outside the scope", path: "/v1/vessels/1", status: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

// error-mapping / Scenario: Nested scopes.
func TestScopeMappers_Nested(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler()
	outer := prblm.ScopeMappers(t.Context(), prblm.Status(errLocked, 423))
	inner := prblm.ScopeMappers(outer, prblm.Status(errLocked, 409))

	if got := h.Resolve(inner, errLocked).Status; got != 409 {
		t.Errorf("inner scope status = %d, want 409", got)
	}

	if got := h.Resolve(outer, errLocked).Status; got != 423 {
		t.Errorf("outer scope status after nesting = %d, want 423 (parent changed)", got)
	}
}

func TestScopeMappers_Options(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler()

	t.Run("registration order within one scope", func(t *testing.T) {
		t.Parallel()

		ctx := prblm.ScopeMappers(t.Context(), prblm.Status(errLocked, 409), prblm.Status(errLocked, 410))
		if got := h.Resolve(ctx, errLocked).Status; got != 409 {
			t.Errorf("status = %d, want 409", got)
		}
	})

	t.Run("nil mapper is skipped", func(t *testing.T) {
		t.Parallel()

		ctx := prblm.ScopeMappers(t.Context(), nil, prblm.Status(errLocked, 423))
		if got := h.Resolve(ctx, errLocked).Status; got != 423 {
			t.Errorf("status = %d, want 423", got)
		}
	})

	t.Run("typed-nil mapper func is skipped without a panic note", func(t *testing.T) {
		t.Parallel()

		var r recorder

		h := prblm.NewHandler(prblm.OnError(r.hook))

		ctx := prblm.ScopeMappers(t.Context(), prblm.MapperFunc(nil), prblm.Status(errLocked, 423))
		if got := h.Resolve(ctx, errLocked).Status; got != 423 {
			t.Errorf("status = %d, want 423", got)
		}

		if calls := r.recorded(); len(calls) != 0 {
			t.Errorf("hook called %d times, want 0 (Resolve reports no notes for a skipped mapper): %+v", len(calls), calls)
		}
	})
}

// Nested scopes built concurrently from one parent must not share storage.
func TestScopeMappers_Concurrent(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler()
	parent := prblm.ScopeMappers(t.Context(), prblm.Status(errLocked, 423))

	var wg sync.WaitGroup

	for i := range 50 {
		status := 450 + i

		wg.Go(func() {
			ctx := prblm.ScopeMappers(parent, prblm.Status(errLocked, status))
			if got := h.Resolve(ctx, errLocked).Status; got != status {
				t.Errorf("goroutine %d: status = %d, want %d", i, got, status)
			}
		})
	}

	wg.Wait()

	if got := h.Resolve(parent, errLocked).Status; got != 423 {
		t.Errorf("parent status = %d, want 423", got)
	}
}
