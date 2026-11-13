package prblm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kartaladev/prblm"
)

// traceKey is the context key of a request's trace ID.
type traceKey struct{}

// traceID sets extension trace_id from the request context.
func traceID(ctx context.Context, p *prblm.Problem) {
	if id, ok := ctx.Value(traceKey{}).(string); ok {
		_ = p.Set("trace_id", id)
	}
}

// serve writes err through h for a request to target and returns the
// decoded problem members.
func serve(t *testing.T, h *prblm.Handler, target string, err error) map[string]any {
	t.Helper()

	req := httptest.NewRequestWithContext(context.WithValue(t.Context(), traceKey{}, "abc"), http.MethodGet, target, http.NoBody)
	rec := httptest.NewRecorder()
	h.Wrap(func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(rec, req)

	return decodeMembers(t, rec.Body.Bytes())
}

// error-mapping / Scenario: Default instance.
func TestEnrichers_DefaultInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		target   string
		instance string
	}{
		{name: "query string dropped", err: errors.New("x"), target: "/v1/vessels/7?token=abc", instance: "/v1/vessels/7"},
		{name: "existing instance kept", err: prblm.New(404, func(p *prblm.Problem) { p.Instance = "urn:uuid:1" }), target: "/v1/vessels/7", instance: "urn:uuid:1"},
		{name: "root path", err: errors.New("x"), target: "/", instance: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := serve(t, prblm.NewHandler(), tt.target, tt.err)["instance"]; got != tt.instance {
				t.Errorf("instance = %v, want %q", got, tt.instance)
			}
		})
	}

	t.Run("resolve outside a request leaves instance empty", func(t *testing.T) {
		t.Parallel()

		if got := prblm.NewHandler().Resolve(t.Context(), errors.New("x")).Instance; got != "" {
			t.Errorf("Instance = %q, want empty", got)
		}
	})
}

// error-mapping / Scenario: Custom enricher.
func TestEnrichers_Custom(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(
		prblm.WithMappers(prblm.Status(errVesselNotFound, 404)),
		prblm.WithEnrichers(traceID),
	)

	tests := []struct {
		name string
		err  error
	}{
		{name: "fallback", err: errors.New("x")},
		{name: "mapped", err: errVesselNotFound},
		{name: "self describing", err: prblm.New(409)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := serve(t, h, "/v1/vessels/7", tt.err)["trace_id"]; got != "abc" {
				t.Errorf("trace_id = %v, want abc", got)
			}
		})
	}

	t.Run("enrichers run in order after the default", func(t *testing.T) {
		t.Parallel()

		var seen []string

		h := prblm.NewHandler(prblm.WithEnrichers(
			func(_ context.Context, p *prblm.Problem) { seen = append(seen, "first:"+p.Instance) },
			func(_ context.Context, p *prblm.Problem) { seen = append(seen, "second:"+p.Title) },
		))
		serve(t, h, "/a", errors.New("x"))

		if len(seen) != 2 || seen[0] != "first:/a" || seen[1] != "second:Internal Server Error" {
			t.Errorf("enrichers saw %v, want [first:/a second:Internal Server Error]", seen)
		}
	})
}

func TestRequestFrom(t *testing.T) {
	t.Parallel()

	if _, ok := prblm.RequestFrom(t.Context()); ok {
		t.Error("RequestFrom on a bare context reported a request")
	}

	var got prblm.Request

	h := prblm.NewHandler(prblm.WithEnrichers(func(ctx context.Context, _ *prblm.Problem) {
		got, _ = prblm.RequestFrom(ctx)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/x?q=1", http.NoBody)
	req.Header.Set("Accept", "application/json")
	h.Wrap(func(http.ResponseWriter, *http.Request) error { return errors.New("x") }).ServeHTTP(httptest.NewRecorder(), req)

	if want := (prblm.Request{Method: "POST", Path: "/v1/x", Accept: "application/json"}); got != want {
		t.Errorf("RequestFrom() = %+v, want %+v", got, want)
	}
}
