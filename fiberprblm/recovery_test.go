package fiberprblm_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

// loadVessel returns a handler that panics with v, named so the stack can
// be checked.
func loadVessel(v any) fiber.Handler {
	return func(fiber.Ctx) error { panic(v) }
}

// fiber-adapter / Scenario: Recovery with the adapter's error handler, rows
// with the recovery middleware.
func TestRecovery_ErrorHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       any
		accept      string
		contentType string
	}{
		{name: "string value", value: "boom", contentType: "application/problem+json"},
		{name: "error value as xml", value: io.ErrClosedPipe, accept: "application/xml", contentType: "application/problem+xml"},
		{name: "abort handler value", value: http.ErrAbortHandler, contentType: "application/problem+json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			app := newApp(hook.option())
			app.Use(fiberprblm.Recovery())
			app.Get("/x", loadVessel(tt.value))

			req := get(t, http.MethodGet, "/x")
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			res := test(t, app, req)

			if res.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", res.status)
			}

			if ct := res.header.Get("Content-Type"); ct != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.contentType)
			}

			if s, ok := tt.value.(string); ok && strings.Contains(res.body, s) {
				t.Errorf("body %q leaks the panic value", res.body)
			}

			got := hook.received()
			if len(got) != 1 {
				t.Fatalf("hook received %v, want one call", got)
			}

			pe, ok := errors.AsType[*prblm.PanicError](got[0])
			if !ok || pe.Value != tt.value {
				t.Fatalf("hook received %v, want a panic error with value %v", got[0], tt.value)
			}

			if err, ok := tt.value.(error); ok && !errors.Is(got[0], err) {
				t.Errorf("hook error %v does not match %v", got[0], err)
			}

			if !strings.Contains(string(pe.Stack), "loadVessel") {
				t.Errorf("stack does not contain loadVessel:\n%s", pe.Stack)
			}
		})
	}
}

// fiber-adapter / Scenario: Recovery with the adapter's error handler, row
// without the recovery middleware: the library does not recover, so the
// panic reaches the middleware outside it.
func TestRecovery_ErrorHandler_NotInstalled(t *testing.T) {
	t.Parallel()

	var (
		hook    hookRecorder
		reached atomic.Value
	)

	app := newApp(hook.option())
	app.Use(func(c fiber.Ctx) (err error) {
		defer func() {
			if v := recover(); v != nil {
				reached.Store(v)

				err = c.SendStatus(http.StatusTeapot)
			}
		}()

		return c.Next()
	})
	app.Get("/x", loadVessel("boom"))

	test(t, app, get(t, http.MethodGet, "/x"))

	if v := reached.Load(); v != "boom" {
		t.Errorf("outer middleware recovered %v, want the panic \"boom\"", v)
	}

	assertHook(t, &hook, nil)
}

// Murphy row: a panic is never mapped like a returned error, as with the
// core's net/http Recover, even when its value is a mapped error.
func TestRecovery_ErrorHandler_MappedValue(t *testing.T) {
	t.Parallel()

	app := newApp(prblm.WithMappers(prblm.Status(ErrVesselNotFound, http.StatusNotFound)))
	app.Use(fiberprblm.Recovery())
	app.Get("/x", loadVessel(ErrVesselNotFound))

	res := test(t, app, get(t, http.MethodGet, "/x"))

	if res.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", res.status)
	}

	if p := problemOf(t, res); p.Title != "Internal Server Error" || p.Detail != "" {
		t.Errorf("problem %q / %q, want the safe 500", p.Title, p.Detail)
	}
}

// fiber-adapter / Scenario: Recovery with fiber's default error handler.
func TestRecovery_DefaultHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		text  string
	}{
		{name: "string value", value: "db password=hunter2", text: "hunter2"},
		{name: "error value", value: errors.New("secret"), text: "secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New() // fiber's default error handler sends err.Error()
			app.Use(fiberprblm.Recovery())
			app.Get("/x", loadVessel(tt.value))

			res := test(t, app, get(t, http.MethodGet, "/x"))

			if res.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", res.status)
			}

			if strings.Contains(res.body, tt.text) {
				t.Errorf("body %q leaks the panic value", res.body)
			}
		})
	}
}

// Slice 7 end to end: route-group scopes and recovered panics over a real
// listener, with Recovery and the error handler together.
func TestRecovery_EndToEnd(t *testing.T) {
	t.Parallel()

	app := newApp()
	app.Use(fiberprblm.Recovery())
	contracts := app.Group("/v1/contracts", fiberprblm.Scope(prblm.Status(ErrLocked, http.StatusLocked)))
	contracts.Get("/:id", func(fiber.Ctx) error { return ErrLocked })
	contracts.Get("/:id/panic", loadVessel("boom"))

	client, base := listen(t, app)

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

			status, p, err := fetchProblem(t.Context(), client, base+tt.path)
			if err != nil {
				t.Fatal(err)
			}

			if status != tt.status || p.Status != tt.status || p.Instance != tt.path {
				t.Errorf("response %d, problem %d %q, want %d %q", status, p.Status, p.Instance, tt.status, tt.path)
			}
		})
	}
}
