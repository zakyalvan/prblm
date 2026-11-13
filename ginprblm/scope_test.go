package ginprblm_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

// newScopedEngine returns an engine with a /v1/contracts group using scope
// and a /v1/vessels group without it; both record ErrLocked.
func newScopedEngine(scope gin.HandlerFunc) *gin.Engine {
	recordLocked := func(c *gin.Context) { _ = c.Error(ErrLocked) }

	engine := gin.New()
	engine.Use(ginprblm.Middleware())
	engine.Group("/v1/contracts", scope).GET("/:id", recordLocked)
	engine.Group("/v1/vessels").GET("/:id", recordLocked)

	return engine
}

// gin-adapter / Scenario: Route group scope.
func TestScope(t *testing.T) {
	t.Parallel()

	engine := newScopedEngine(ginprblm.Scope(prblm.Status(ErrLocked, http.StatusLocked)))

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

			if rec := serve(t, engine, get(t, tt.path)); rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

// gin-adapter / Scenario: Degenerate scopes.
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

			engine := newScopedEngine(ginprblm.Scope(tt.mappers...))

			if rec := serve(t, engine, get(t, "/v1/contracts/1")); rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
		})
	}
}

// Murphy row: changing the caller's slice after Scope returns does not
// change the scope.
func TestScope_CallerSliceChanged(t *testing.T) {
	t.Parallel()

	mappers := []prblm.Mapper{prblm.Status(ErrLocked, http.StatusLocked)}
	engine := newScopedEngine(ginprblm.Scope(mappers...))
	mappers[0] = prblm.Status(ErrLocked, http.StatusTeapot)

	if rec := serve(t, engine, get(t, "/v1/contracts/1")); rec.Code != http.StatusLocked {
		t.Errorf("status = %d, want 423 from the mapper given to Scope", rec.Code)
	}
}
