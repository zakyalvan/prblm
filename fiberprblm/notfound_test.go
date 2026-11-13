package fiberprblm_test

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm/fiberprblm"
)

// Design D6 rests on fiber v3 letting the error handler tell a router 404
// from a handler that returns fiber.ErrNotFound, proven here before slice 6
// builds on it (CLAUDE.md rule 8). The router returns the sentinel
// fiber.ErrNotFound itself, so the error value cannot tell them apart; the
// signal is c.Matched(), which is true only when an endpoint route (not
// app.Use middleware) matched the request.
func TestFiberNotFound_Distinguishable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(app *fiber.App)
		isMatched bool
	}{
		{name: "router 404", setup: func(*fiber.App) {}},
		{name: "router 404 after middleware", setup: func(app *fiber.App) { app.Use(fiberprblm.Recovery()) }},
		{
			name:      "handler returns ErrNotFound",
			setup:     func(app *fiber.App) { app.Get("/x", func(fiber.Ctx) error { return fiber.ErrNotFound }) },
			isMatched: true,
		},
		{
			name: "handler after middleware returns ErrNotFound",
			setup: func(app *fiber.App) {
				app.Use(fiberprblm.Recovery())
				app.Get("/x", func(fiber.Ctx) error { return fiber.ErrNotFound })
			},
			isMatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				isMatched  bool
				isSentinel bool
			)

			app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
				//nolint:errorlint // the point is that the router returns the sentinel itself.
				isMatched, isSentinel = c.Matched(), err == fiber.ErrNotFound

				return c.SendStatus(http.StatusNotFound)
			}})
			tt.setup(app)

			if res := test(t, app, get(t, http.MethodGet, "/x")); res.status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", res.status)
			}

			if isMatched != tt.isMatched || !isSentinel {
				t.Errorf("Matched() = %v and sentinel %v, want %v and the sentinel itself", isMatched, isSentinel, tt.isMatched)
			}
		})
	}
}
