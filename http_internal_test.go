package prblm

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrack_Reuses(t *testing.T) {
	t.Parallel()

	tw := track(httptest.NewRecorder())

	if again := track(tw); again != tw {
		t.Error("track(trackingWriter) wrapped it again, want the same writer")
	}
}

func TestTrackingWriter_WriteHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		code    int
		started bool
	}{
		{name: "continue", code: http.StatusContinue, started: false},
		{name: "early hints", code: http.StatusEarlyHints, started: false},
		{name: "last informational", code: 199, started: false},
		{name: "switching protocols", code: http.StatusSwitchingProtocols, started: true},
		{name: "ok", code: http.StatusOK, started: true},
		{name: "not found", code: http.StatusNotFound, started: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tw := track(httptest.NewRecorder())
			tw.WriteHeader(tt.code)

			if tw.started != tt.started {
				t.Errorf("WriteHeader(%d): started = %v, want %v", tt.code, tw.started, tt.started)
			}
		})
	}
}
