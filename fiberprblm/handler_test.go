package fiberprblm_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

// fiber-adapter / Scenario: Options configure the error handler.
func TestErrorHandler_Options(t *testing.T) {
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

			app := newApp(tt.opts...)
			app.Get("/v1/vessels/:id", func(fiber.Ctx) error { return ErrVesselNotFound })

			res := test(t, app, get(t, http.MethodGet, "/v1/vessels/7"))

			if res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}

			if ct := res.header.Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
		})
	}
}

// fiber-adapter / Scenario: Invalid options fail at construction.
func TestErrorHandler_InvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		opts  []prblm.Option
		named string
	}{
		{name: "relative namespace", opts: []prblm.Option{prblm.WithNamespace("v1/api/probs")}, named: "WithNamespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if msg := fmt.Sprint(recover()); !strings.Contains(msg, tt.named) {
					t.Errorf("panic = %q, want a message naming %s", msg, tt.named)
				}
			}()

			fiberprblm.ErrorHandler(tt.opts...)
		})
	}
}

// fiber-adapter / Scenario: Returned errors.
func TestErrorHandler_Returned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "sentinel", err: ErrVesselNotFound, status: http.StatusNotFound},
		{name: "wrapped sentinel", err: fmt.Errorf("load: %w", ErrVesselNotFound), status: http.StatusNotFound},
		{name: "unmapped", err: errors.New("pq: password=hunter2"), status: http.StatusInternalServerError},
		{name: "problem", err: prblm.New(http.StatusConflict), status: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := newApp(prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound)))
			app.Get("/v1/vessels/:id", func(fiber.Ctx) error { return tt.err })

			res := test(t, app, get(t, http.MethodGet, "/v1/vessels/7"))

			if res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}

			if ct := res.header.Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			if p := problemOf(t, res); p.Instance != "/v1/vessels/7" {
				t.Errorf("instance = %q, want /v1/vessels/7", p.Instance)
			}
		})
	}
}

// fiber-adapter / Scenario: Unmapped error text never leaks.
func TestErrorHandler_Returned_NoLeak(t *testing.T) {
	t.Parallel()

	texts := []string{
		"dial tcp 10.0.0.5:5432: connection refused",
		"user alice@example.com not permitted",
	}

	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			res := serveX(t, func(fiber.Ctx) error { return errors.New(text) })

			if res.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", res.status)
			}

			if strings.Contains(res.body, text) {
				t.Errorf("body %q contains the error text %q", res.body, text)
			}
		})
	}
}

// fiber-adapter / Scenario: Fiber-generated and handler fiber errors.
func TestErrorHandler_FiberErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method, path string
		status       int
		title        string
		hidden       string // must not appear in the body
	}{
		// routing
		{name: "unknown route", method: http.MethodGet, path: "/v1/unknown", status: http.StatusNotFound, title: "Not Found", hidden: "Cannot GET"},
		{name: "method not allowed", method: http.MethodDelete, path: "/v1/vessels/7", status: http.StatusMethodNotAllowed, title: "Method Not Allowed"},
		// handler-returned
		{name: "fiber error with message", method: http.MethodGet, path: "/v1/vessels/7?e=forbidden", status: http.StatusForbidden, title: "Forbidden", hidden: "token abc"},
		{name: "fiber sentinel", method: http.MethodGet, path: "/v1/vessels/7?e=bad", status: http.StatusBadRequest, title: "Bad Request"},
		{name: "fiber error with a success code", method: http.MethodGet, path: "/v1/vessels/7?e=ok", status: http.StatusInternalServerError, title: "Internal Server Error"},
	}

	returned := map[string]error{
		"forbidden": fiber.NewError(http.StatusForbidden, "token abc expired"),
		"bad":       fiber.ErrBadRequest,
		"ok":        fiber.NewError(http.StatusOK, "ok"),
	}

	app := newApp()
	app.Get("/v1/vessels/:id", func(c fiber.Ctx) error { return returned[c.Query("e")] })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := test(t, app, get(t, tt.method, tt.path))

			if res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}

			p := problemOf(t, res)

			if p.Title != tt.title {
				t.Errorf("title = %q, want %q", p.Title, tt.title)
			}

			// A fiber error's message never becomes the detail.
			if p.Detail != "" {
				t.Errorf("detail = %q, want none", p.Detail)
			}

			if tt.hidden != "" && strings.Contains(res.body, tt.hidden) {
				t.Errorf("body %q contains %q", res.body, tt.hidden)
			}
		})
	}
}

// fiber-adapter / Scenario: Mapper takes precedence over a fiber error code.
func TestErrorHandler_FiberErrors_MapperWins(t *testing.T) {
	t.Parallel()

	handler := func(fiber.Ctx) error { return fmt.Errorf("%w: %w", fiber.ErrBadRequest, ErrLocked) }
	mapper := prblm.Status(ErrLocked, http.StatusLocked)

	if res := serveX(t, handler, prblm.WithMappers(mapper)); res.status != http.StatusLocked {
		t.Errorf("status = %d, want 423", res.status)
	}
}

// Murphy row: a typed-nil *fiber.Error is a non-nil error with no code; it
// is answered like any unmapped error.
func TestErrorHandler_FiberErrors_TypedNil(t *testing.T) {
	t.Parallel()

	app := newApp()
	app.Get("/x", func(fiber.Ctx) error {
		var fe *fiber.Error

		return fe
	})

	res := test(t, app, get(t, http.MethodGet, "/x"))

	if res.status != http.StatusInternalServerError || res.header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("response %d %q, want a 500 problem", res.status, res.header.Get("Content-Type"))
	}
}

// fiber-adapter / Scenario: Request facts.
func TestErrorHandler_RequestFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		target      string
		accept      string
		contentType string
	}{
		{name: "query left out", target: "/v1/vessels/7?token=abc", contentType: "application/problem+json"},
		{name: "xml preferred", target: "/v1/vessels/7", accept: "application/xml", contentType: "application/problem+xml"},
		{name: "unsupported type", target: "/v1/vessels/7", accept: "image/png", contentType: "application/problem+json"},
	}

	app := newApp()
	app.Get("/v1/vessels/:id", func(fiber.Ctx) error {
		return prblm.WithStatus(errors.New("x"), http.StatusNotFound)
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := get(t, http.MethodGet, tt.target)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			res := test(t, app, req)

			if ct := res.header.Get("Content-Type"); ct != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.contentType)
			}

			if p := problemOf(t, res); p.Instance != "/v1/vessels/7" || p.Status != http.StatusNotFound {
				t.Errorf("problem %d %q, want 404 /v1/vessels/7", p.Status, p.Instance)
			}

			if strings.Contains(res.body, "token") {
				t.Errorf("body %q leaks the query string", res.body)
			}
		})
	}
}

// fiber-adapter / Scenario: Handler wrote before returning an error.
func TestErrorHandler_Buffered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		does      func(c fiber.Ctx)
		requestID string // expected X-Request-Id on the problem
		isStream  bool
	}{
		{
			name: "buffered json replaced",
			does: func(c fiber.Ctx) { _ = c.Status(http.StatusCreated).JSON(fiber.Map{"id": 7}) },
		},
		{
			name:      "header kept",
			does:      func(c fiber.Ctx) { c.Set("X-Request-Id", "r1") },
			requestID: "r1",
		},
		{
			name: "stream kept",
			does: func(c fiber.Ctx) {
				_ = c.SendStreamWriter(func(w *bufio.Writer) {
					_, _ = w.WriteString("data: 1\n\n")
				})
			},
			isStream: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			app := newApp(hook.option(), prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound)))
			app.Get("/x", func(c fiber.Ctx) error {
				tt.does(c)

				return ErrVesselNotFound
			})

			res := test(t, app, get(t, http.MethodGet, "/x"))

			assertHook(t, &hook, ErrVesselNotFound)

			if tt.isStream {
				if res.body != "data: 1\n\n" {
					t.Errorf("body = %q, want the stream unchanged", res.body)
				}

				return
			}

			if res.status != http.StatusNotFound || res.header.Get("Content-Type") != "application/problem+json" {
				t.Errorf("response %d %q, want a 404 problem", res.status, res.header.Get("Content-Type"))
			}

			if p := problemOf(t, res); p.Status != http.StatusNotFound {
				t.Errorf("problem status = %d, want 404", p.Status)
			}

			if got := res.header.Get("X-Request-Id"); got != tt.requestID {
				t.Errorf("X-Request-Id = %q, want %q", got, tt.requestID)
			}
		})
	}
}

// fiber-adapter / Scenario: Client went away.
func TestErrorHandler_Canceled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		canceled  bool
		err       error
		isWritten bool
	}{
		{name: "canceled request", canceled: true, err: fmt.Errorf("query: %w", context.Canceled), isWritten: false},
		{name: "active request", canceled: false, err: context.Canceled, isWritten: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			app := newApp(hook.option())
			app.Use(func(c fiber.Ctx) error {
				ctx, cancel := context.WithCancel(c.Context())
				if tt.canceled {
					cancel()
				} else {
					// The error handler runs after this middleware returns,
					// so the context must outlive it.
					t.Cleanup(cancel)
				}

				c.SetContext(ctx)

				return c.Next()
			})
			app.Get("/x", func(fiber.Ctx) error { return tt.err })

			res := test(t, app, get(t, http.MethodGet, "/x"))

			isProblem := res.header.Get("Content-Type") == "application/problem+json"
			if isProblem != tt.isWritten {
				t.Errorf("problem written = %v, want %v (status %d, body %q)", isProblem, tt.isWritten, res.status, res.body)
			}

			if tt.isWritten && res.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", res.status)
			}

			if !tt.isWritten && res.body != "" {
				t.Errorf("body = %q, want nothing written by the library", res.body)
			}

			assertHook(t, &hook, tt.err)
		})
	}
}

// fiber-adapter / Scenario: Faulty consumer code under fiber.
func TestErrorHandler_Faulty(t *testing.T) {
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

			app := newApp(tt.opts...)
			app.Get("/x", func(fiber.Ctx) error { return errX })

			// The second request shows the app keeps serving.
			for i := range 2 {
				if res := test(t, app, get(t, http.MethodGet, "/x")); res.status != http.StatusConflict {
					t.Errorf("request %d: status = %d, want 409", i+1, res.status)
				}
			}
		})
	}
}

// panickyUnwrapError is an error whose Unwrap panics. Walking its chain
// panics, in the adapter's hint extraction and in the core's Respond.
type panickyUnwrapError struct{}

func (panickyUnwrapError) Error() string { return "panicky" }
func (panickyUnwrapError) Unwrap() error { panic("unwrap") }

// Design D2 failure mode: resolving the error panics despite the core's
// contract; the app answers with a bare 500 and keeps serving.
func TestErrorHandler_Faulty_RespondPanics(t *testing.T) {
	t.Parallel()

	app := newApp()
	app.Get("/x", func(fiber.Ctx) error { return panickyUnwrapError{} })
	app.Get("/ok", func(c fiber.Ctx) error { return c.SendString("ok") })

	res := test(t, app, get(t, http.MethodGet, "/x"))

	if res.status != http.StatusInternalServerError || res.body != "" {
		t.Errorf("response %d %q, want a bare 500", res.status, res.body)
	}

	if res := test(t, app, get(t, http.MethodGet, "/ok")); res.status != http.StatusOK {
		t.Errorf("follow-up status = %d, want 200", res.status)
	}
}

// fiber-adapter / Scenario: Concurrent fiber requests.
func TestErrorHandler_Concurrent(t *testing.T) {
	t.Parallel()

	const requests = 200

	app := newApp(prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound)))
	app.Get("/a", func(fiber.Ctx) error { return ErrVesselNotFound })
	app.Get("/b", func(fiber.Ctx) error { return fiber.ErrUnauthorized })

	client, base := listen(t, app)
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
			status, p, err := fetchProblem(t.Context(), client, base+path)
			if err != nil {
				t.Errorf("%s: %v", path, err)

				return
			}

			if status != want[path] || p.Status != want[path] || p.Instance != path {
				t.Errorf("%s: response %d, problem %d %q, want %d %q", path, status, p.Status, p.Instance, want[path], path)
			}
		})
	}

	wg.Wait()
}

// Murphy row: called directly with a nil error, the error handler has
// nothing to answer and writes nothing.
func TestErrorHandler_NilError(t *testing.T) {
	t.Parallel()

	handle := fiberprblm.ErrorHandler()

	app := fiber.New()
	app.Get("/x", func(c fiber.Ctx) error {
		c.Status(http.StatusAccepted)

		return handle(c, nil)
	})

	res := test(t, app, get(t, http.MethodGet, "/x"))

	if res.status != http.StatusAccepted || res.body != "" || strings.HasPrefix(res.header.Get("Content-Type"), "application/problem") {
		t.Errorf("response %d %q %q, want the handler's 202 untouched", res.status, res.header.Get("Content-Type"), res.body)
	}
}
