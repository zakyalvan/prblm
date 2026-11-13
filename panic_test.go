package prblm_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

func TestPanicError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   any
		message string
		unwraps error
	}{
		{name: "error value", value: io.ErrClosedPipe, message: "io: read/write on closed pipe", unwraps: io.ErrClosedPipe},
		{name: "string value", value: "boom", message: "boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &prblm.PanicError{Value: tt.value}

			if !strings.Contains(err.Error(), tt.message) {
				t.Errorf("Error() = %q, want it to contain %q", err.Error(), tt.message)
			}

			if tt.unwraps != nil && !errors.Is(err, tt.unwraps) {
				t.Errorf("errors.Is(err, %v) = false, want true", tt.unwraps)
			}

			if tt.unwraps == nil && errors.Unwrap(err) != nil {
				t.Errorf("Unwrap() = %v, want nil", errors.Unwrap(err))
			}
		})
	}
}
