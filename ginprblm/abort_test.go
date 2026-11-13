package ginprblm_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

// Design D3 (CLAUDE.md rule 8): gin commits the response header on abort,
// so a middleware that runs afterwards could no longer set the status or the
// Content-Type. Task 3.1 proved it against a plain middleware (see the task
// notes); through Middleware the problem keeps full control of both.
func TestGinAbort_CommitsHeaderEarly(t *testing.T) {
	t.Parallel()

	errX := errors.New("x")

	aborts := []struct {
		name    string
		handler gin.HandlerFunc
		status  int
	}{
		{
			name:    "abort with status",
			handler: func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) },
			status:  http.StatusUnauthorized,
		},
		{
			name:    "abort with error",
			handler: func(c *gin.Context) { _ = c.AbortWithError(http.StatusForbidden, errX) },
			status:  http.StatusForbidden,
		},
	}

	for _, tt := range aborts {
		engine := newEngine(tt.handler)

		t.Run(tt.name+"/recorder", func(t *testing.T) {
			t.Parallel()

			rec := serve(t, engine, get(t, "/x"))
			assertProblemHeader(t, rec.Code, rec.Header(), tt.status)
		})

		t.Run(tt.name+"/server", func(t *testing.T) {
			t.Parallel()

			rec := fetch(t, engine, "/x")
			assertProblemHeader(t, rec.Code, rec.Header(), tt.status)
		})
	}
}

func assertProblemHeader(t *testing.T, status int, header http.Header, want int) {
	t.Helper()

	if status != want {
		t.Errorf("status = %d, want %d", status, want)
	}

	if ct := header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

// gin-adapter / Scenario: AbortWithError.
func TestMiddleware_AbortWithError(t *testing.T) {
	t.Parallel()

	lockedMapper := prblm.Is(ErrLocked, func(error) *prblm.Problem {
		return prblm.New(http.StatusLocked, prblm.WithDetail("Vessel is locked."))
	})
	conflict := prblm.New(http.StatusConflict)

	tests := []struct {
		name        string
		mappers     []prblm.Mapper
		abortStatus int
		err         error
		status      int
		detail      string
		hidden      string // error text that must not reach the client
	}{
		// hint used
		{name: "hint 403", abortStatus: http.StatusForbidden, err: errors.New("nope"), status: http.StatusForbidden, hidden: "nope"},
		{name: "hint 400 hides text", abortStatus: http.StatusBadRequest, err: errors.New("secret"), status: http.StatusBadRequest, hidden: "secret"},
		{name: "hint 404 for unmapped sentinel", abortStatus: http.StatusNotFound, err: ErrVesselNotFound, status: http.StatusNotFound},
		// mapper wins
		{
			name:        "mapper over hint",
			mappers:     []prblm.Mapper{lockedMapper},
			abortStatus: http.StatusBadRequest,
			err:         ErrLocked,
			status:      http.StatusLocked,
			detail:      "Vessel is locked.",
		},
		{name: "self-describing over hint", abortStatus: http.StatusBadRequest, err: conflict, status: http.StatusConflict},
		// invalid hints
		{name: "success status is no hint", abortStatus: http.StatusOK, err: errors.New("x-text"), status: http.StatusInternalServerError, hidden: "x-text"},
		{name: "redirect status is no hint", abortStatus: http.StatusFound, err: errors.New("x-text"), status: http.StatusInternalServerError, hidden: "x-text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			abort := func(c *gin.Context) { _ = c.AbortWithError(tt.abortStatus, tt.err) }
			engine := newEngine(abort, prblm.WithMappers(tt.mappers...))

			rec := serve(t, engine, get(t, "/x"))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			body := rec.Body.String()

			if tt.hidden != "" && strings.Contains(body, tt.hidden) {
				t.Errorf("body %q leaks the error text %q", body, tt.hidden)
			}

			if p := parseRecorded(t, rec); p.Detail != tt.detail {
				t.Errorf("detail = %q, want %q", p.Detail, tt.detail)
			}
		})
	}
}

// gin-adapter / Scenario: AbortWithStatus and plain statuses.
func TestMiddleware_StatusOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		status  int
		title   string // set for problem rows
		body    string // expected body for pass-through rows
	}{
		// problems
		{name: "abort 401", handler: func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }, status: http.StatusUnauthorized, title: "Unauthorized"},
		{name: "abort 429", handler: func(c *gin.Context) { c.AbortWithStatus(http.StatusTooManyRequests) }, status: http.StatusTooManyRequests, title: "Too Many Requests"},
		{name: "status 500 without body", handler: func(c *gin.Context) { c.Status(http.StatusInternalServerError) }, status: http.StatusInternalServerError, title: "Internal Server Error"},
		// passed through
		{name: "status 204", handler: func(c *gin.Context) { c.Status(http.StatusNoContent) }, status: http.StatusNoContent},
		{name: "status 304", handler: func(c *gin.Context) { c.Status(http.StatusNotModified) }, status: http.StatusNotModified},
		{name: "abort 200", handler: func(c *gin.Context) { c.AbortWithStatus(http.StatusOK) }, status: http.StatusOK},
		{name: "json 200", handler: func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"id": 7}) }, status: http.StatusOK, body: `{"id":7}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := newEngine(tt.handler)

			rec := fetch(t, engine, "/x")
			ct := rec.Header().Get("Content-Type")

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if tt.title == "" {
				if strings.HasPrefix(ct, "application/problem") {
					t.Errorf("Content-Type = %q, want no problem", ct)
				}

				if body := rec.Body.String(); body != tt.body {
					t.Errorf("body = %q, want %q", body, tt.body)
				}

				return
			}

			if ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			if p := parseRecorded(t, rec); p.Title != tt.title {
				t.Errorf("title = %q, want %q", p.Title, tt.title)
			}
		})
	}
}

// gin-adapter / Scenario: Body written before or after an error.
func TestMiddleware_BodyWritten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		status  int
		body    string
		hookErr error // nil: the hook is not called
	}{
		{
			name:    "abort with status json",
			handler: func(c *gin.Context) { c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "x"}) },
			status:  http.StatusBadRequest,
			body:    `{"code":"x"}`,
		},
		{
			name: "error after body",
			handler: func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
				_ = c.Error(ErrLocked)
			},
			status:  http.StatusOK,
			body:    `{"ok":true}`,
			hookErr: ErrLocked,
		},
		{
			name: "body after error",
			handler: func(c *gin.Context) {
				_ = c.Error(ErrLocked)
				c.String(http.StatusConflict, "locked")
			},
			status:  http.StatusConflict,
			body:    "locked",
			hookErr: ErrLocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			engine := newEngine(tt.handler, hook.option())

			rec := fetch(t, engine, "/x")

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if body := rec.Body.String(); body != tt.body {
				t.Errorf("body = %q, want %q", body, tt.body)
			}

			assertHook(t, &hook, tt.hookErr)
		})
	}
}

// gin-adapter / Scenario: Client went away.
func TestMiddleware_Canceled(t *testing.T) {
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

			engine := newEngine(func(c *gin.Context) { _ = c.Error(tt.err) }, hook.option())

			ctx, cancel := context.WithCancel(t.Context())
			if tt.canceled {
				cancel()
			} else {
				t.Cleanup(cancel)
			}

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", http.NoBody)
			rec := serve(t, engine, req)

			isProblem := rec.Header().Get("Content-Type") == "application/problem+json"
			if isProblem != tt.isWritten {
				t.Errorf("problem written = %v, want %v (status %d, body %q)", isProblem, tt.isWritten, rec.Code, rec.Body.String())
			}

			if tt.isWritten && rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}

			if !tt.isWritten && rec.Body.Len() != 0 {
				t.Errorf("body = %q, want nothing written by the library", rec.Body.String())
			}

			assertHook(t, &hook, tt.err)
		})
	}
}

// Design D3 failure mode: middleware registered before ours commits the
// header early. Nothing malformed is written and the error reaches the hook.
func TestMiddleware_RegisteredLate(t *testing.T) {
	t.Parallel()

	var hook hookRecorder

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Status(http.StatusUnauthorized)
		c.Writer.WriteHeaderNow()
		c.Next()
	})
	engine.Use(ginprblm.Middleware(hook.option()))
	engine.GET("/x", func(c *gin.Context) { _ = c.Error(ErrLocked) })

	rec := fetch(t, engine, "/x")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want the early 401", rec.Code)
	}

	if body := rec.Body.String(); body != "" {
		t.Errorf("body = %q, want nothing written after the committed header", body)
	}

	assertHook(t, &hook, ErrLocked)
}
