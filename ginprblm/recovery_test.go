package ginprblm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

// gin-adapter / Scenario: Shared options across middleware and recovery.
func TestRecovery_Options(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		status  int
	}{
		{
			name:    "recorded error",
			handler: func(c *gin.Context) { _ = c.Error(ErrVesselNotFound) },
			status:  http.StatusNotFound,
		},
		{
			name:    "panic",
			handler: func(*gin.Context) { panic("boom") },
			status:  http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			opts := []prblm.Option{
				prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound)),
				hook.option(),
			}

			engine := gin.New()
			engine.Use(ginprblm.Recovery(opts...), ginprblm.Middleware(opts...))
			engine.GET("/v1/vessels/:id", tt.handler)

			rec := serve(t, engine, get(t, "/v1/vessels/7"))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if got := hook.received(); len(got) != 1 {
				t.Errorf("hook called %d times (%v), want 1", len(got), got)
			}
		})
	}
}

// gin-adapter / Scenario: Recovery placement, rows with recovery.
func TestRecovery_Placement(t *testing.T) {
	t.Parallel()

	type constructor = func(...prblm.Option) gin.HandlerFunc

	tests := []struct {
		name        string
		chain       []constructor
		accept      string
		contentType string
	}{
		{
			name:        "recovery then middleware",
			chain:       []constructor{ginprblm.Recovery, ginprblm.Middleware},
			contentType: "application/problem+json",
		},
		{
			name:        "middleware then recovery",
			chain:       []constructor{ginprblm.Middleware, ginprblm.Recovery},
			contentType: "application/problem+json",
		},
		{
			name:        "recovery only",
			chain:       []constructor{ginprblm.Recovery},
			contentType: "application/problem+json",
		},
		{
			name:        "recovery then middleware with xml",
			chain:       []constructor{ginprblm.Recovery, ginprblm.Middleware},
			accept:      "application/xml",
			contentType: "application/problem+xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := gin.New()
			for _, middleware := range tt.chain {
				engine.Use(middleware())
			}

			engine.GET("/x", func(*gin.Context) { panic("boom") })

			req := get(t, "/x")
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			rec := serve(t, engine, req)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}

			if ct := rec.Header().Get("Content-Type"); ct != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.contentType)
			}

			if body := rec.Body.String(); strings.Contains(body, "boom") {
				t.Errorf("body %q leaks the panic value", body)
			}
		})
	}
}

// gin-adapter / Scenario: Recovery placement, middleware-only row: without
// Recovery the panic reaches gin's own handling.
func TestRecovery_Placement_NotInstalled(t *testing.T) {
	t.Parallel()

	engine := newEngine(func(*gin.Context) { panic("boom") })

	defer func() {
		if v := recover(); v != "boom" {
			t.Errorf("recovered %v, want the panic to reach gin as \"boom\"", v)
		}
	}()

	serve(t, engine, get(t, "/x"))
	t.Fatal("no panic reached gin")
}

// loadVessel is a handler that panics, named so the stack can be checked.
func loadVessel(*gin.Context) {
	panic(io.ErrClosedPipe)
}

// gin-adapter / Scenario: Panic details go to the hook.
func TestRecovery_Hook(t *testing.T) {
	t.Parallel()

	var hook hookRecorder

	engine := newRecoveryEngine(loadVessel, hook.option())

	serve(t, engine, get(t, "/x"))

	assertPanicHook(t, &hook, io.ErrClosedPipe)

	got := hook.received()
	pe, _ := errors.AsType[*prblm.PanicError](got[0])

	// The message is what a hook logs; it never reaches the client.
	if msg := got[0].Error(); !strings.Contains(msg, io.ErrClosedPipe.Error()) {
		t.Errorf("hook error message %q does not describe the panic value", msg)
	}

	if !strings.Contains(string(pe.Stack), "loadVessel") {
		t.Errorf("stack does not contain loadVessel:\n%s", pe.Stack)
	}
}

// gin-adapter / Scenario: Recovery edge cases.
func TestRecovery_Edge(t *testing.T) {
	t.Parallel()

	t.Run("abort handler panic is passed on", func(t *testing.T) {
		t.Parallel()

		var hook hookRecorder

		engine := newRecoveryEngine(func(*gin.Context) { panic(http.ErrAbortHandler) }, hook.option())

		rec := httptest.NewRecorder()

		func() {
			defer func() {
				if v := recover(); v != http.ErrAbortHandler { //nolint:errorlint // the exact sentinel must be passed on.
					t.Errorf("recovered %v, want http.ErrAbortHandler passed on", v)
				}
			}()

			engine.ServeHTTP(rec, get(t, "/x"))
		}()

		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want nothing written", rec.Body.String())
		}

		assertHook(t, &hook, nil)
	})

	t.Run("partial body flushed before the panic", func(t *testing.T) {
		t.Parallel()

		var hook hookRecorder

		engine := newRecoveryEngine(func(c *gin.Context) {
			_, _ = c.Writer.WriteString(`{"partial":`)
			c.Writer.Flush()
			panic("boom")
		}, hook.option())

		rec := fetch(t, engine, "/x")

		if body := rec.Body.String(); body != `{"partial":` {
			t.Errorf("body = %q, want only the partial body", body)
		}

		assertPanicHook(t, &hook, "boom")
	})

	t.Run("broken connection", func(t *testing.T) {
		t.Parallel()

		var hook hookRecorder

		broken := fmt.Errorf("write: %w", syscall.EPIPE)

		engine := newRecoveryEngine(func(*gin.Context) { panic(broken) }, hook.option())

		rec := serve(t, engine, get(t, "/x"))

		if ct := rec.Header().Get("Content-Type"); ct != "" || rec.Body.Len() != 0 {
			t.Errorf("response %q %q, want nothing written", ct, rec.Body.String())
		}

		assertPanicHook(t, &hook, broken)
	})

	t.Run("enricher panics too", func(t *testing.T) {
		t.Parallel()

		panickingEnricher := prblm.WithEnrichers(func(context.Context, *prblm.Problem) { panic("enricher") })

		engine := newRecoveryEngine(func(*gin.Context) { panic("boom") }, panickingEnricher)
		engine.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

		rec := serve(t, engine, get(t, "/x"))

		if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("response %d %q, want a 500 problem", rec.Code, rec.Header().Get("Content-Type"))
		}

		if rec := serve(t, engine, get(t, "/ok")); rec.Code != http.StatusOK {
			t.Errorf("follow-up status = %d, want 200", rec.Code)
		}
	})

	// Murphy row: a panic is never mapped like a returned error, as with the
	// core's net/http Recover, even when its value is a mapped error.
	t.Run("mapped panic value", func(t *testing.T) {
		t.Parallel()

		mapper := prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound))

		engine := newRecoveryEngine(func(*gin.Context) { panic(ErrVesselNotFound) }, mapper)

		rec := serve(t, engine, get(t, "/x"))

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}

		if p := parseRecorded(t, rec); p.Title != "Internal Server Error" || p.Detail != "" {
			t.Errorf("problem %q / %q, want the safe 500", p.Title, p.Detail)
		}
	})
}

// Slice 5 end to end: route-group scopes and recovered panics over a real
// server, with Recovery and Middleware together.
func TestRecovery_EndToEnd(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(ginprblm.Recovery(), ginprblm.Middleware())
	contracts := engine.Group("/v1/contracts", ginprblm.Scope(prblm.Status(ErrLocked, http.StatusLocked)))
	contracts.GET("/:id", func(c *gin.Context) { _ = c.Error(ErrLocked) })
	contracts.GET("/:id/panic", func(c *gin.Context) {
		c.Status(http.StatusUnauthorized) // recorded, then replaced by the panic's 500
		panic("boom")
	})

	tests := []struct {
		path   string
		status int
	}{
		{path: "/v1/contracts/1", status: http.StatusLocked},
		{path: "/v1/contracts/1/panic", status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			rec := fetch(t, engine, tt.path)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if p := parseRecorded(t, rec); p.Status != tt.status || p.Instance != tt.path {
				t.Errorf("problem %d %q, want %d %q", p.Status, p.Instance, tt.status, tt.path)
			}
		})
	}
}

// panickingWriter is a response writer whose Write panics, standing in for
// a broken writer further down the stack.
type panickingWriter struct {
	*httptest.ResponseRecorder
}

func (panickingWriter) Write([]byte) (int, error) { panic("writer") }

// Murphy row (CLAUDE.md rule 5): a panic while Recovery writes the problem
// does not escape, and the hook still hears of the original panic and of
// the failed write.
func TestRecovery_Edge_WriterPanics(t *testing.T) {
	t.Parallel()

	var hook hookRecorder

	engine := newRecoveryEngine(func(*gin.Context) { panic("boom") }, hook.option())

	func() {
		defer func() {
			if v := recover(); v != nil {
				t.Errorf("panic escaped Recovery: %v", v)
			}
		}()

		engine.ServeHTTP(panickingWriter{httptest.NewRecorder()}, get(t, "/x"))
	}()

	var values []any

	for _, err := range hook.received() {
		if pe, ok := errors.AsType[*prblm.PanicError](err); ok {
			values = append(values, pe.Value)
		}
	}

	if !slices.Contains(values, any("boom")) || !slices.Contains(values, any("writer")) {
		t.Errorf("hook received panic values %v, want both \"boom\" and \"writer\"", values)
	}
}
