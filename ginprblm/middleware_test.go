package ginprblm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

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

			h := prblm.NewHandler(hook.option(), prblm.WithMappers(
				prblm.Status(ErrVesselNotFound, http.StatusNotFound),
				prblm.Status(ErrLocked, http.StatusLocked),
			))

			engine := gin.New()
			engine.Use(ginprblm.Middleware(h))

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

			engine := gin.New()
			engine.Use(ginprblm.Middleware(prblm.NewHandler()))
			engine.GET("/x", func(c *gin.Context) { _ = c.Error(errors.New(text)) })

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

			engine := gin.New()
			engine.Use(ginprblm.Middleware(prblm.NewHandler(tt.opts...)))
			engine.GET("/x", func(c *gin.Context) { _ = c.Error(errX) })

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
	engine.Use(ginprblm.Middleware(prblm.NewHandler()))
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

// gin-adapter / Scenario: Nil handler, error-middleware row.
func TestMiddleware_NilHandler(t *testing.T) {
	t.Parallel()

	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "problem handler") {
			t.Errorf("panic = %q, want a message naming the problem handler", msg)
		}
	}()

	ginprblm.Middleware(nil)
}

// Slice 2 end to end: a recorded error becomes a problem over a real server,
// with negotiation and the query string left out of the instance.
func TestMiddleware_EndToEnd(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(ginprblm.Middleware(prblm.NewHandler(prblm.WithMappers(
		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
	))))
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

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v1/vessels/7?token=abc", http.NoBody)
			req.RequestURI = ""

			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}

			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", res.StatusCode)
			}

			if ct := res.Header.Get("Content-Type"); ct != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.contentType)
			}

			if vary := res.Header.Get("Vary"); vary != "Accept" {
				t.Errorf("Vary = %q, want Accept", vary)
			}

			if p := parse(t, res); p.Instance != "/v1/vessels/7" {
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
	engine.Use(ginprblm.Middleware(prblm.NewHandler()))
	engine.GET("/x", func(c *gin.Context) {
		c.Errors = append(c.Errors, &gin.Error{Err: nil, Type: gin.ErrorTypePrivate})
	})

	rec := serve(t, engine, get(t, "/x"))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q, want none", ct)
	}
}
