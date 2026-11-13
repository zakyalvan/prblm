package ginprblm_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Design D5 rests on two gin behaviors, proven here before slice 5 builds
// on them (CLAUDE.md rule 8): middleware added with engine.Use runs for a
// path no route claims, and c.FullPath() is empty there, also when the
// engine has a NoRoute handler, registered before or after Use. A matched
// route has a non-empty FullPath.
func TestGinNoRoute_RunsGlobalMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		noRoute  string
		path     string
		fullPath string
	}{
		{name: "unmatched", path: "/v1/api/probs/x"},
		{name: "unmatched with NoRoute after Use", noRoute: "after", path: "/v1/api/probs/x"},
		{name: "unmatched with NoRoute before Use", noRoute: "before", path: "/v1/api/probs/x"},
		{name: "matched", path: "/v1/vessels/7", fullPath: "/v1/vessels/:id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				ran      bool
				fullPath string
			)

			record := func(c *gin.Context) {
				ran, fullPath = true, c.FullPath()
				c.Next()
			}
			custom := func(c *gin.Context) { c.String(http.StatusNotFound, "custom") }

			engine := gin.New()
			if tt.noRoute == "before" {
				engine.NoRoute(custom)
			}

			engine.Use(record)

			if tt.noRoute == "after" {
				engine.NoRoute(custom)
			}

			engine.GET("/v1/vessels/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

			rec := serve(t, engine, get(t, tt.path))

			if !ran || fullPath != tt.fullPath {
				t.Errorf("middleware ran %v with FullPath %q, want it to run with %q", ran, fullPath, tt.fullPath)
			}

			if tt.noRoute != "" && rec.Body.String() != "custom" {
				t.Errorf("body = %q, want the NoRoute handler's", rec.Body.String())
			}
		})
	}
}

// Design D5 also rests on this gin behavior, proven here before the
// discovery branch builds on it (CLAUDE.md rule 8, task 8.1): with
// engine.HandleMethodNotAllowed on, a path that has a route only for
// another method runs global middleware with an empty c.FullPath() too, but
// gin records 405 on the writer before the middleware runs, and 404 for a
// path no route claims, so the recorded status tells the two apart.
func TestGinNoMethod_RecordsStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           string
		path             string
		methodNotAllowed bool
		recorded         int
		sent             int
	}{
		{name: "no route", method: http.MethodGet, path: "/v1/api/probs/x", methodNotAllowed: true, recorded: http.StatusNotFound, sent: http.StatusNotFound},
		{name: "route for another method", method: http.MethodGet, path: "/v1/api/probs/x/locked", methodNotAllowed: true, recorded: http.StatusMethodNotAllowed, sent: http.StatusMethodNotAllowed},
		{name: "route for another method at the root", method: http.MethodGet, path: "/v1/api/probs", methodNotAllowed: true, recorded: http.StatusMethodNotAllowed, sent: http.StatusMethodNotAllowed},
		{name: "route for another method, 405 off", method: http.MethodGet, path: "/v1/api/probs/x/locked", recorded: http.StatusNotFound, sent: http.StatusNotFound},
		{name: "head with a route for another method", method: http.MethodHead, path: "/v1/api/probs/x/locked", methodNotAllowed: true, recorded: http.StatusMethodNotAllowed, sent: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				ran      bool
				fullPath string
				recorded int
			)

			engine := gin.New()
			engine.HandleMethodNotAllowed = tt.methodNotAllowed
			engine.Use(func(c *gin.Context) {
				ran, fullPath, recorded = true, c.FullPath(), c.Writer.Status()
				c.Next()
			})
			engine.POST("/v1/api/probs/x/locked", func(c *gin.Context) { c.Status(http.StatusOK) })
			engine.POST("/v1/api/probs", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			rec := serve(t, engine, req)

			if !ran || fullPath != "" || recorded != tt.recorded {
				t.Errorf("middleware ran %v with FullPath %q and recorded status %d, want it to run with an empty FullPath and %d",
					ran, fullPath, recorded, tt.recorded)
			}

			if rec.Code != tt.sent {
				t.Errorf("status = %d, want %d", rec.Code, tt.sent)
			}
		})
	}
}
