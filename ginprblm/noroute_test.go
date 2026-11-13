package ginprblm_test

import (
	"net/http"
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
		name      string
		noRoute   string
		path      string
		ran       bool
		fullPath  string
		isMatched bool
	}{
		{name: "unmatched", path: "/v1/api/probs/x", ran: true},
		{name: "unmatched with NoRoute after Use", noRoute: "after", path: "/v1/api/probs/x", ran: true},
		{name: "unmatched with NoRoute before Use", noRoute: "before", path: "/v1/api/probs/x", ran: true},
		{name: "matched", path: "/v1/vessels/7", ran: true, fullPath: "/v1/vessels/:id", isMatched: true},
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

			if ran != tt.ran || fullPath != tt.fullPath {
				t.Errorf("middleware ran %v with FullPath %q, want %v %q", ran, fullPath, tt.ran, tt.fullPath)
			}

			if tt.noRoute != "" && rec.Body.String() != "custom" {
				t.Errorf("body = %q, want the NoRoute handler's", rec.Body.String())
			}
		})
	}
}
