package fiberprblm_test

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

// newScopedApp returns an app with a /v1/contracts group using scope and a
// /v1/vessels group without it; both return ErrLocked.
func newScopedApp(scope fiber.Handler) *fiber.App {
	returnLocked := func(fiber.Ctx) error { return ErrLocked }

	app := newApp()
	app.Group("/v1/contracts", scope).Get("/:id", returnLocked)
	app.Group("/v1/vessels").Get("/:id", returnLocked)

	return app
}

// fiber-adapter / Scenario: Route group scope.
func TestScope(t *testing.T) {
	t.Parallel()

	app := newScopedApp(fiberprblm.Scope(prblm.Status(ErrLocked, http.StatusLocked)))

	tests := []struct {
		path   string
		status int
	}{
		{path: "/v1/contracts/1", status: http.StatusLocked},
		{path: "/v1/vessels/1", status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if res := test(t, app, get(t, http.MethodGet, tt.path)); res.status != tt.status {
				t.Errorf("status = %d, want %d", res.status, tt.status)
			}
		})
	}
}

// fiber-adapter / Scenario: Degenerate scopes.
func TestScope_Degenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mappers []prblm.Mapper
	}{
		{name: "no mappers", mappers: nil},
		{name: "a single nil mapper", mappers: []prblm.Mapper{nil}},
		{name: "a nil mapper func", mappers: []prblm.Mapper{prblm.MapperFunc(nil)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := newScopedApp(fiberprblm.Scope(tt.mappers...))

			if res := test(t, app, get(t, http.MethodGet, "/v1/contracts/1")); res.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", res.status)
			}
		})
	}
}

// Murphy row: changing the caller's slice after Scope returns does not
// change the scope.
func TestScope_CallerSliceChanged(t *testing.T) {
	t.Parallel()

	mappers := []prblm.Mapper{prblm.Status(ErrLocked, http.StatusLocked)}
	app := newScopedApp(fiberprblm.Scope(mappers...))
	mappers[0] = prblm.Status(ErrLocked, http.StatusTeapot)

	if res := test(t, app, get(t, http.MethodGet, "/v1/contracts/1")); res.status != http.StatusLocked {
		t.Errorf("status = %d, want 423 from the mapper given to Scope", res.status)
	}
}
