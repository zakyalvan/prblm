package prblm_test

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

// refusedError is a domain error that carries data for the problem.
type refusedError struct{ Reasons []string }

func (e *refusedError) Error() string { return "refused: " + strings.Join(e.Reasons, ", ") }

// error-mapping / Scenario: Sentinel helper.
func TestIs(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.Is(errVesselNotFound, func(error) *prblm.Problem {
		return prblm.New(404)
	})))

	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{name: "sentinel", err: errVesselNotFound, expected: 404},
		{name: "wrapped", err: fmt.Errorf("load: %w", errVesselNotFound), expected: 404},
		{name: "joined", err: errors.Join(io.EOF, errVesselNotFound), expected: 404},
		{name: "same text, other error", err: errors.New("vessel not found"), expected: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := h.Resolve(t.Context(), tt.err).Status; got != tt.expected {
				t.Errorf("Status = %d, want %d", got, tt.expected)
			}
		})
	}
}

// error-mapping / Scenario: Typed helper exposes the value.
func TestAs(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithMappers(prblm.As(func(e *refusedError) *prblm.Problem {
		return prblm.New(409, prblm.WithExt("reasons", e.Reasons))
	})))

	tests := []struct {
		name     string
		err      error
		expected int
		reasons  []string
	}{
		{name: "value", err: &refusedError{Reasons: []string{"closed"}}, expected: 409, reasons: []string{"closed"}},
		{name: "wrapped", err: fmt.Errorf("x: %w", &refusedError{Reasons: []string{"full"}}), expected: 409, reasons: []string{"full"}},
		{name: "other error", err: errors.New("refused"), expected: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := h.Resolve(t.Context(), tt.err)
			if p.Status != tt.expected {
				t.Errorf("Status = %d, want %d", p.Status, tt.expected)
			}

			v, ok := p.Ext("reasons")
			if tt.reasons == nil {
				if ok {
					t.Errorf("Ext(reasons) = %v, want none", v)
				}

				return
			}

			if got, isSlice := v.([]string); !isSlice || !slices.Equal(got, tt.reasons) {
				t.Errorf("Ext(reasons) = %v, want %v", v, tt.reasons)
			}
		})
	}
}

func TestMapperHelpers_Misuse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		build    func()
		argument string
	}{
		{name: "Is with nil target", build: func() { prblm.Is(nil, func(error) *prblm.Problem { return nil }) }, argument: "target"},
		{name: "Is with nil function", build: func() { prblm.Is(errVesselNotFound, nil) }, argument: "function"},
		{name: "Status with nil target", build: func() { prblm.Status(nil, 404) }, argument: "target"},
		{name: "As with nil function", build: func() { prblm.As[*refusedError](nil) }, argument: "function"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("construction did not panic")
				}

				if !strings.Contains(fmt.Sprint(r), tt.argument) {
					t.Errorf("panic %q does not name the %s", r, tt.argument)
				}
			}()

			tt.build()
		})
	}
}
