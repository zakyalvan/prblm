package prblm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

// decodeErr decodes body into a struct with an int field age and returns the
// decoding error.
func decodeErr(body string) error {
	var v struct {
		Age int `json:"age"`
	}

	return json.NewDecoder(strings.NewReader(body)).Decode(&v)
}

// error-mapping / Scenario: Built-in mapping.
func TestBuiltins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     []prblm.Option
		err      error
		expected int
		detail   string
	}{
		{name: "syntax error", err: decodeErr(`{"age":}`), expected: 400, detail: "The request body is not valid JSON."},
		{name: "unexpected eof", err: decodeErr(`{"age":`), expected: 400, detail: "The request body is incomplete."},
		// A bare io.EOF can come from any reader, so it is not blamed on the
		// client.
		{name: "empty body", err: decodeErr(``), expected: 500, detail: ""},
		{name: "eof from an upstream call", err: fmt.Errorf("fetch inventory: %w", io.EOF), expected: 500, detail: ""},
		{name: "unexpected eof from an upstream call", err: fmt.Errorf("fetch inventory: %w", io.ErrUnexpectedEOF), expected: 400, detail: "The request body is incomplete."},
		{name: "wrong type", err: decodeErr(`{"age":"x"}`), expected: 422, detail: "The request body has a value of the wrong type."},
		{name: "too large", err: &http.MaxBytesError{Limit: 1024}, expected: 413, detail: "The request body is too large."},
		{name: "deadline", err: context.DeadlineExceeded, expected: 504, detail: ""},
		{name: "disabled syntax error", opts: []prblm.Option{prblm.WithoutBuiltins()}, err: decodeErr(`{"age":}`), expected: 500},
		{name: "disabled deadline", opts: []prblm.Option{prblm.WithoutBuiltins()}, err: context.DeadlineExceeded, expected: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := prblm.NewHandler(tt.opts...).Resolve(t.Context(), tt.err)
			if p.Status != tt.expected || p.Detail != tt.detail {
				t.Errorf("Resolve() = %d %q, want %d %q", p.Status, p.Detail, tt.expected, tt.detail)
			}

			body, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}

			if strings.Contains(string(body), "age") || strings.Contains(string(body), "1024") {
				t.Errorf("body %s leaks error details", body)
			}
		})
	}
}
