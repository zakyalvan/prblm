package ginprblm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

// gin-adapter / Scenario: Options configure the middleware.
func TestMiddleware_Options(t *testing.T) {
	t.Parallel()

	mapper := prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound))

	tests := []struct {
		name   string
		opts   []prblm.Option
		status int
	}{
		{name: "none", status: http.StatusInternalServerError},
		{name: "mapper", opts: []prblm.Option{mapper}, status: http.StatusNotFound},
		{name: "nil options around a mapper", opts: []prblm.Option{nil, mapper, nil}, status: http.StatusNotFound},
		{name: "single nil", opts: []prblm.Option{nil}, status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := gin.New()
			engine.Use(ginprblm.Middleware(tt.opts...))
			engine.GET("/v1/vessels/:id", func(c *gin.Context) { _ = c.Error(ErrVesselNotFound) })

			rec := serve(t, engine, get(t, "/v1/vessels/7"))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
		})
	}
}

// gin-adapter / Scenario: Invalid options fail at construction.
func TestMiddleware_InvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		constructor func(...prblm.Option) gin.HandlerFunc
		opts        []prblm.Option
		named       string
	}{
		{
			name:        "error middleware with a relative namespace",
			constructor: ginprblm.Middleware,
			opts:        []prblm.Option{prblm.WithNamespace("v1/api/probs")},
			named:       "WithNamespace",
		},
		{
			name:        "recovery middleware with a relative namespace",
			constructor: ginprblm.Recovery,
			opts:        []prblm.Option{prblm.WithNamespace("v1/api/probs")},
			named:       "WithNamespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if msg := fmt.Sprint(recover()); !strings.Contains(msg, tt.named) {
					t.Errorf("panic = %q, want a message naming %s", msg, tt.named)
				}
			}()

			tt.constructor(tt.opts...)
		})
	}
}

// gin-adapter / Scenario: Recorded errors.
func TestMiddleware_RecordedErrors(t *testing.T) {
	t.Parallel()

	leaky := errors.New("pq: password=hunter2")

	tests := []struct {
		name       string
		middleware gin.HandlerFunc
		handler    gin.HandlerFunc
		status     int
		hookErr    error
	}{
		{
			name:    "sentinel",
			handler: func(c *gin.Context) { _ = c.Error(ErrVesselNotFound) },
			status:  http.StatusNotFound,
			hookErr: ErrVesselNotFound,
		},
		{
			name:    "unmapped",
			handler: func(c *gin.Context) { _ = c.Error(leaky) },
			status:  http.StatusInternalServerError,
			hookErr: leaky,
		},
		{
			name: "last of two",
			handler: func(c *gin.Context) {
				_ = c.Error(ErrVesselNotFound)
				_ = c.Error(ErrLocked)
			},
			status:  http.StatusLocked,
			hookErr: ErrLocked,
		},
		{
			name: "handler after middleware",
			middleware: func(c *gin.Context) {
				_ = c.Error(ErrLocked)
				c.Next()
			},
			handler: func(c *gin.Context) { _ = c.Error(ErrVesselNotFound) },
			status:  http.StatusNotFound,
			hookErr: ErrVesselNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			engine := gin.New()
			engine.Use(ginprblm.Middleware(hook.option(), prblm.WithMappers(
				prblm.Status(ErrVesselNotFound, http.StatusNotFound),
				prblm.Status(ErrLocked, http.StatusLocked),
			)))

			if tt.middleware != nil {
				engine.Use(tt.middleware)
			}

			engine.GET("/v1/vessels/:id", tt.handler)

			rec := serve(t, engine, get(t, "/v1/vessels/7"))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			if p := parseRecorded(t, rec); p.Instance != "/v1/vessels/7" {
				t.Errorf("instance = %q, want /v1/vessels/7", p.Instance)
			}

			if got := hook.last(); !errors.Is(got, tt.hookErr) {
				t.Errorf("hook received %v, want %v", got, tt.hookErr)
			}
		})
	}
}

// gin-adapter / Scenario: Unmapped error text never leaks.
func TestMiddleware_NoLeak(t *testing.T) {
	t.Parallel()

	texts := []string{
		"dial tcp 10.0.0.5:5432: connection refused",
		"user alice@example.com not permitted",
	}

	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			engine := newEngine(func(c *gin.Context) { _ = c.Error(errors.New(text)) })

			rec := serve(t, engine, get(t, "/x"))

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}

			if body := rec.Body.String(); strings.Contains(body, text) {
				t.Errorf("body %q contains the error text %q", body, text)
			}
		})
	}
}

// gin-adapter / Scenario: Faulty consumer code under gin.
func TestMiddleware_FaultyConsumer(t *testing.T) {
	t.Parallel()

	errX := errors.New("x")
	mapX := prblm.Status(errX, http.StatusConflict)

	panickingMapper := prblm.MapperFunc(func(context.Context, error) *prblm.Problem { panic("mapper") })
	nilMapper := prblm.MapperFunc(func(context.Context, error) *prblm.Problem { return nil })

	tests := []struct {
		name string
		opts []prblm.Option
	}{
		{name: "mapper panics", opts: []prblm.Option{prblm.WithMappers(panickingMapper, mapX)}},
		{name: "mapper returns nil", opts: []prblm.Option{prblm.WithMappers(nilMapper, mapX)}},
		{
			name: "enricher panics",
			opts: []prblm.Option{
				prblm.WithEnrichers(func(context.Context, *prblm.Problem) { panic("enricher") }),
				prblm.WithMappers(mapX),
			},
		},
		{
			name: "hook panics",
			opts: []prblm.Option{
				prblm.OnError(func(context.Context, error, *prblm.Problem) { panic("hook") }),
				prblm.WithMappers(mapX),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := newEngine(func(c *gin.Context) { _ = c.Error(errX) }, tt.opts...)

			// The second request shows the engine keeps serving.
			for i := range 2 {
				if rec := serve(t, engine, get(t, "/x")); rec.Code != http.StatusConflict {
					t.Errorf("request %d: status = %d, want 409", i+1, rec.Code)
				}
			}
		})
	}
}

// panickyUnwrapError is an error whose Unwrap panics. The core walks error chains
// outside its consumer-code guards, so Respond panics on it (design D2: the
// adapter must survive a panicking Respond).
type panickyUnwrapError struct{}

func (panickyUnwrapError) Error() string { return "panicky" }
func (panickyUnwrapError) Unwrap() error { panic("unwrap") }

// Design D2 failure mode: Respond panics despite its contract.
func TestMiddleware_FaultyConsumer_RespondPanics(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(ginprblm.Middleware())
	engine.GET("/x", func(c *gin.Context) {
		// c.Error itself walks the chain, so record the error directly.
		c.Errors = append(c.Errors, &gin.Error{Err: panickyUnwrapError{}, Type: gin.ErrorTypePrivate})
	})
	engine.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	rec := serve(t, engine, get(t, "/x"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	if body := rec.Body.String(); body != "" {
		t.Errorf("body = %q, want empty", body)
	}

	if rec := serve(t, engine, get(t, "/ok")); rec.Code != http.StatusOK {
		t.Errorf("follow-up status = %d, want 200", rec.Code)
	}
}

// Slice 2 end to end: a recorded error becomes a problem over a real server,
// with negotiation and the query string left out of the instance.
func TestMiddleware_EndToEnd(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(ginprblm.Middleware(prblm.WithMappers(
		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
	)))
	engine.GET("/v1/vessels/:id", func(c *gin.Context) { _ = c.Error(ErrVesselNotFound) })

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	tests := []struct {
		name        string
		accept      string
		contentType string
	}{
		{name: "json by default", accept: "", contentType: "application/problem+json"},
		{name: "xml when preferred", accept: "application/xml", contentType: "application/problem+xml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := get(t, srv.URL+"/v1/vessels/7?token=abc")
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			rec := do(t, srv, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}

			if ct := rec.Header().Get("Content-Type"); ct != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.contentType)
			}

			if vary := rec.Header().Get("Vary"); vary != "Accept" {
				t.Errorf("Vary = %q, want Accept", vary)
			}

			if p := parseRecorded(t, rec); p.Instance != "/v1/vessels/7" {
				t.Errorf("instance = %q, want /v1/vessels/7", p.Instance)
			}
		})
	}
}

// Murphy row: a recorded error holding a nil error has nothing to answer, so
// the library writes nothing and the handler's response stands.
func TestMiddleware_NilRecordedError(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(ginprblm.Middleware())
	engine.GET("/x", func(c *gin.Context) {
		c.Status(http.StatusAccepted)
		c.Errors = append(c.Errors, &gin.Error{Err: nil, Type: gin.ErrorTypePrivate})
	})

	rec := serve(t, engine, get(t, "/x"))

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want the recorded 202", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q, want none", ct)
	}
}

// Design D3 failure mode: a panic unwinding through the middleware must
// leave gin's own writer in place, so recovery middleware outside it can
// still send its status.
func TestMiddleware_PanicPassesThrough(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	engine.Use(ginprblm.Middleware())
	engine.GET("/x", func(*gin.Context) { panic("boom") })

	if rec := serve(t, engine, get(t, "/x")); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 from the outer recovery", rec.Code)
	}
}

// gin-adapter / Scenario: Concurrent gin requests.
func TestMiddleware_Concurrent(t *testing.T) {
	t.Parallel()

	const requests = 200

	engine := gin.New()
	engine.Use(ginprblm.Middleware(prblm.WithMappers(
		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
	)))
	engine.GET("/a", func(c *gin.Context) { _ = c.Error(ErrVesselNotFound) })
	engine.GET("/b", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	want := map[string]int{"/a": http.StatusNotFound, "/b": http.StatusUnauthorized}

	var wg sync.WaitGroup

	for i := range requests {
		path := "/a"
		if i%2 == 1 {
			path = "/b"
		}

		// Each request reports with t.Errorf only: t.Fatal must not be
		// called outside the test goroutine.
		wg.Go(func() {
			req := get(t, srv.URL+path)
			req.RequestURI = ""

			res, err := srv.Client().Do(req)
			if err != nil {
				t.Errorf("%s: request: %v", path, err)

				return
			}

			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != want[path] {
				t.Errorf("%s: status = %d, want %d", path, res.StatusCode, want[path])
			}

			p, err := prblm.Parse(res)
			if err != nil {
				t.Errorf("%s: Parse: %v", path, err)

				return
			}

			if p.Status != want[path] || p.Instance != path {
				t.Errorf("%s: problem status %d instance %q, want %d %q", path, p.Status, p.Instance, want[path], path)
			}
		})
	}

	wg.Wait()
}
