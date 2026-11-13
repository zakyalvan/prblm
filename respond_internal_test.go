package prblm

import (
	"errors"
	"testing"
)

// fallbackBody is a constant so it cannot fail; this keeps it in step with
// what Resolve produces for an unmapped error.
func TestFallbackBody_MatchesResolvedFallback(t *testing.T) {
	t.Parallel()

	encoded, err := NewHandler().Resolve(t.Context(), errors.New("unmapped")).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	if string(encoded) != fallbackBody {
		t.Errorf("fallbackBody = %s, want %s", fallbackBody, encoded)
	}
}
