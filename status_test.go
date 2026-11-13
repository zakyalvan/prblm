package prblm_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

// error-mapping / Scenario: Hint used when unmapped, Scenario: Nested and
// wrapped hints.
func TestWithStatus(t *testing.T) {
	t.Parallel()

	secret := errors.New("secret text")

	tests := []struct {
		name   string
		err    error
		status int
		title  string
	}{
		{name: "404 hint", err: prblm.WithStatus(secret, 404), status: 404, title: "Not Found"},
		{name: "403 hint", err: prblm.WithStatus(secret, 403), status: 403, title: "Forbidden"},
		{name: "599 hint", err: prblm.WithStatus(secret, 599), status: 599, title: ""},
		{name: "200 ignored", err: prblm.WithStatus(secret, 200), status: 500, title: "Internal Server Error"},
		{name: "399 ignored", err: prblm.WithStatus(secret, 399), status: 500, title: "Internal Server Error"},
		{name: "600 ignored", err: prblm.WithStatus(secret, 600), status: 500, title: "Internal Server Error"},
		{name: "0 ignored", err: prblm.WithStatus(secret, 0), status: 500, title: "Internal Server Error"},
		{name: "negative ignored", err: prblm.WithStatus(secret, -1), status: 500, title: "Internal Server Error"},
		{name: "outermost hint wins", err: prblm.WithStatus(prblm.WithStatus(secret, 400), 404), status: 404, title: "Not Found"},
		{name: "wrapped hint", err: fmt.Errorf("x: %w", prblm.WithStatus(secret, 401)), status: 401, title: "Unauthorized"},
	}

	h := prblm.NewHandler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := h.Resolve(t.Context(), tt.err)
			if p.Status != tt.status || p.Title != tt.title {
				t.Errorf("Resolve() = %d %q, want %d %q", p.Status, p.Title, tt.status, tt.title)
			}

			body, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}

			if strings.Contains(string(body), "secret text") {
				t.Errorf("body %s leaks the error text", body)
			}
		})
	}

	t.Run("hinted error keeps message and chain", func(t *testing.T) {
		t.Parallel()

		err := prblm.WithStatus(secret, 404)
		if err.Error() != "secret text" {
			t.Errorf("Error() = %q, want the original message", err.Error())
		}

		if !errors.Is(err, secret) {
			t.Error("the hinted error no longer wraps the original")
		}
	})

	t.Run("nil error stays nil", func(t *testing.T) {
		t.Parallel()

		if err := prblm.WithStatus(nil, 404); err != nil {
			t.Errorf("WithStatus(nil, 404) = %v, want nil", err)
		}
	})
}
