package prblm_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

// problem-document / Scenario: Field options.
func TestNew_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fields   []prblm.Field
		expected string
	}{
		{
			name:     "type",
			fields:   []prblm.Field{prblm.WithType("https://example.com/probs/out-of-credit")},
			expected: `{"type":"https://example.com/probs/out-of-credit","status":403}`,
		},
		{
			name:     "title",
			fields:   []prblm.Field{prblm.WithTitle("You do not have enough credit.")},
			expected: `{"title":"You do not have enough credit.","status":403}`,
		},
		{
			name:     "detail",
			fields:   []prblm.Field{prblm.WithDetail("Your balance is 30.")},
			expected: `{"status":403,"detail":"Your balance is 30."}`,
		},
		{
			name:     "title and extension",
			fields:   []prblm.Field{prblm.WithTitle("Out of credit"), prblm.WithExt("balance", 30)},
			expected: `{"title":"Out of credit","status":403,"balance":30}`,
		},
		// Murphy rows.
		{
			name:     "later field wins",
			fields:   []prblm.Field{prblm.WithTitle("First"), prblm.WithTitle("Second")},
			expected: `{"title":"Second","status":403}`,
		},
		{
			name:     "empty type",
			fields:   []prblm.Field{prblm.WithType("")},
			expected: `{"status":403}`,
		},
		{
			name:     "explicit about:blank",
			fields:   []prblm.Field{prblm.WithType("about:blank")},
			expected: `{"status":403}`,
		},
		{
			name:     "nil field ignored",
			fields:   []prblm.Field{nil, prblm.WithDetail("Your balance is 30."), nil},
			expected: `{"status":403,"detail":"Your balance is 30."}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, err := json.Marshal(prblm.New(403, tt.fields...))
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			if got := string(body); got != tt.expected {
				t.Errorf("document = %s, want %s", got, tt.expected)
			}
		})
	}
}

// problem-document / Scenario: Effective type.
func TestProblem_EffectiveType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		given     string
		effective string
		hasType   bool
	}{
		{name: "empty", given: "", effective: "about:blank", hasType: false},
		{name: "explicit about blank", given: "about:blank", effective: "about:blank", hasType: false},
		{name: "custom type", given: "https://example.com/probs/x", effective: "https://example.com/probs/x", hasType: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &prblm.Problem{Type: tt.given, Status: 404}

			if got := p.EffectiveType(); got != tt.effective {
				t.Errorf("EffectiveType() = %q, want %q", got, tt.effective)
			}

			body, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			if got := strings.Contains(string(body), `"type"`); got != tt.hasType {
				t.Errorf("body %s has type member = %v, want %v", body, got, tt.hasType)
			}
		})
	}

	t.Run("nil problem", func(t *testing.T) {
		t.Parallel()

		var p *prblm.Problem
		if got := p.EffectiveType(); got != "about:blank" {
			t.Errorf("EffectiveType() = %q, want %q", got, "about:blank")
		}
	})
}

// problem-document / Scenario: Title finalized.
func TestProblem_Finalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		typ      string
		status   int
		title    string
		expected string
	}{
		{name: "filled from 404", status: 404, expected: "Not Found"},
		{name: "filled from 422", status: 422, expected: "Unprocessable Entity"},
		{name: "filled from 503", status: 503, expected: "Service Unavailable"},
		{name: "explicit title kept", status: 404, title: "Vessel not found", expected: "Vessel not found"},
		{name: "custom type left empty", typ: "https://example.com/probs/x", status: 409, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := mapping(&prblm.Problem{Type: tt.typ, Status: tt.status, Title: tt.title})

			if got := h.Resolve(t.Context(), errors.New("x")).Title; got != tt.expected {
				t.Errorf("Title = %q, want %q", got, tt.expected)
			}
		})
	}
}

// problem-document / Scenario: Error message and cause, Scenario: Nil problem.
func TestProblem_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		title   string
		detail  string
		cause   error
		message string
	}{
		{
			name: "title and detail", status: 404, title: "Not Found", detail: "vessel 7 not found",
			cause: sql.ErrNoRows, message: "Not Found: vessel 7 not found",
		},
		{name: "status only", status: 500, message: "Internal Server Error"},
		{name: "title only", status: 418, title: "Teapot", cause: io.EOF, message: "Teapot"},
		{name: "detail without title", status: 404, detail: "vessel 7 not found", message: "Not Found: vessel 7 not found"},
		{name: "status without reason phrase", status: 599, message: "problem status 599"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var opts []prblm.Field
			if tt.cause != nil {
				opts = append(opts, prblm.WithCause(tt.cause))
			}

			p := prblm.New(tt.status, opts...)
			p.Title = tt.title
			p.Detail = tt.detail

			if got := p.Error(); got != tt.message {
				t.Errorf("Error() = %q, want %q", got, tt.message)
			}

			if tt.cause != nil {
				if !errors.Is(p, tt.cause) {
					t.Errorf("errors.Is(p, %v) = false, want true", tt.cause)
				}

				body, err := json.Marshal(p)
				if err != nil {
					t.Fatalf("json.Marshal() error = %v", err)
				}

				if strings.Contains(string(body), tt.cause.Error()) {
					t.Errorf("body %s contains cause text %q", body, tt.cause.Error())
				}
			}

			if tt.cause == nil && errors.Is(p, io.EOF) {
				t.Error("errors.Is(p, io.EOF) = true for a problem without a cause")
			}
		})
	}

	t.Run("nil problem", func(t *testing.T) {
		t.Parallel()

		var p *prblm.Problem
		if got := p.Error(); got != "<nil problem>" {
			t.Errorf("Error() = %q, want %q", got, "<nil problem>")
		}

		var err error = p
		if errors.Is(err, io.EOF) {
			t.Error("errors.Is(nil problem, io.EOF) = true, want false")
		}
	})
}

// problem-document / Scenario: Extension name check.
func TestProblem_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ext      string
		expected error
	}{
		{name: "snake case", ext: "trace_id"},
		{name: "three letters", ext: "abc"},
		{name: "mixed case with digit", ext: "Balance2"},
		{name: "too short", ext: "ab", expected: prblm.ErrInvalidExtensionName},
		{name: "leading digit", ext: "1abc", expected: prblm.ErrInvalidExtensionName},
		{name: "leading underscore", ext: "_abc", expected: prblm.ErrInvalidExtensionName},
		{name: "dot", ext: "field.name", expected: prblm.ErrInvalidExtensionName},
		{name: "hyphen", ext: "trace-id", expected: prblm.ErrInvalidExtensionName},
		{name: "non ascii", ext: "nãme", expected: prblm.ErrInvalidExtensionName},
		{name: "empty", ext: "", expected: prblm.ErrInvalidExtensionName},
		{name: "reserved status", ext: "status", expected: prblm.ErrReservedExtensionName},
		{name: "reserved type", ext: "type", expected: prblm.ErrReservedExtensionName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := prblm.New(403)
			err := p.Set(tt.ext, 30)

			if !errors.Is(err, tt.expected) {
				t.Fatalf("Set(%q) error = %v, want %v", tt.ext, err, tt.expected)
			}

			v, ok := p.Ext(tt.ext)
			if wantSet := tt.expected == nil; ok != wantSet {
				t.Fatalf("Ext(%q) present = %v, want %v", tt.ext, ok, wantSet)
			}

			if ok && v != 30 {
				t.Errorf("Ext(%q) = %v, want 30", tt.ext, v)
			}
		})
	}

	t.Run("replaces a value", func(t *testing.T) {
		t.Parallel()

		p := prblm.New(403)
		if err := errors.Join(p.Set("balance", 30), p.Set("balance", 40)); err != nil {
			t.Fatal(err)
		}

		if v, _ := p.Ext("balance"); v != 40 {
			t.Errorf("Ext(balance) = %v, want 40", v)
		}
	})

	t.Run("nil problem has no extensions", func(t *testing.T) {
		t.Parallel()

		var p *prblm.Problem
		if _, ok := p.Ext("balance"); ok {
			t.Error("Ext on nil problem reported a value")
		}
	})
}

func TestWithExt(t *testing.T) {
	t.Parallel()

	t.Run("valid name", func(t *testing.T) {
		t.Parallel()

		p := prblm.New(403, prblm.WithExt("trace_id", "abc"))
		if v, ok := p.Ext("trace_id"); !ok || v != "abc" {
			t.Errorf("Ext(trace_id) = %v, %v; want abc, true", v, ok)
		}
	})

	t.Run("invalid name panics at construction", func(t *testing.T) {
		t.Parallel()

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("WithExt with an invalid name did not panic")
			}

			if !strings.Contains(fmt.Sprint(r), "trace-id") {
				t.Errorf("panic %v does not name the extension", r)
			}
		}()

		prblm.WithExt("trace-id", "abc")
	})
}

// validExtensionName is the rule from the spec, written independently of the
// implementation.
var validExtensionName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,}$`)

func FuzzExtensionName(f *testing.F) {
	for _, seed := range []string{"trace_id", "abc", "ab", "1abc", "_abc", "nãme", "status", "", "a\x00b"} {
		f.Add(seed)
	}

	reserved := map[string]bool{"type": true, "title": true, "status": true, "detail": true, "instance": true}

	f.Fuzz(func(t *testing.T, name string) {
		p := prblm.New(400)
		err := p.Set(name, true)

		accepted := err == nil
		if want := validExtensionName.MatchString(name) && !reserved[name]; accepted != want {
			t.Fatalf("Set(%q) accepted = %v, want %v (err = %v)", name, accepted, want, err)
		}

		if _, ok := p.Ext(name); ok != accepted {
			t.Fatalf("Ext(%q) present = %v after Set accepted = %v", name, ok, accepted)
		}
	})
}

// problem-document / Scenario: Header not in body.
func TestProblem_Header(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		header string
		value  string
	}{
		{name: "retry after", status: 503, header: "Retry-After", value: "5"},
		{name: "www authenticate", status: 401, header: "WWW-Authenticate", value: "Bearer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := prblm.New(tt.status, prblm.WithHeader(tt.header, tt.value))
			if got := p.Header.Get(tt.header); got != tt.value {
				t.Fatalf("Header.Get(%q) = %q, want %q", tt.header, got, tt.value)
			}

			body, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			for name := range decodeMembers(t, body) {
				if strings.EqualFold(name, tt.header) || strings.EqualFold(name, "header") {
					t.Errorf("body %s has member %q", body, name)
				}
			}
		})
	}
}
