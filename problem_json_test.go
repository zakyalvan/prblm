package prblm_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/kartaladev/prblm"
)

// problem-document / Scenario: Members serialized to JSON.
func TestProblem_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		problem  *prblm.Problem
		expected string
	}{
		{
			name: "all members",
			problem: &prblm.Problem{
				Type:     "https://example.com/probs/out-of-credit",
				Status:   403,
				Title:    "You do not have enough credit.",
				Detail:   "Your current balance is 30, but that costs 50.",
				Instance: "/account/12345/msgs/abc",
			},
			expected: `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.",` +
				`"status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc"}`,
		},
		{
			name:     "empty members omitted",
			problem:  &prblm.Problem{Status: 500, Title: "Internal Server Error"},
			expected: `{"title":"Internal Server Error","status":500}`,
		},
		{
			name:     "empty members omitted with instance",
			problem:  &prblm.Problem{Status: 404, Title: "Not Found", Instance: "/v1/vessels/7"},
			expected: `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.problem)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			if string(got) != tt.expected {
				t.Errorf("json.Marshal() =\n%s\nwant\n%s", got, tt.expected)
			}
		})
	}
}

func TestProblem_MarshalJSON_Value(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(prblm.Problem{Status: 404, Title: "Not Found"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if expected := `{"title":"Not Found","status":404}`; string(got) != expected {
		t.Errorf("json.Marshal(value) = %s, want %s", got, expected)
	}
}

// problem-document / Scenario: Extensions serialized flat and sorted,
// Scenario: Extension value encoding.
func TestProblem_MarshalJSON_Extensions(t *testing.T) {
	t.Parallel()

	type fieldError struct {
		Detail  string `json:"detail"`
		Pointer string `json:"pointer"`
	}

	type node struct{ Next *node }

	cycle := &node{}
	cycle.Next = cycle

	tests := []struct {
		name     string
		problem  *prblm.Problem
		expected string
		fails    bool
	}{
		{
			name:     "one extension",
			problem:  forbidden(prblm.WithExt("balance", 30)),
			expected: `{"title":"Forbidden","status":403,"balance":30}`,
		},
		{
			name:     "sorted by name",
			problem:  forbidden(prblm.WithExt("trace_id", "abc"), prblm.WithExt("balance", 30)),
			expected: `{"title":"Forbidden","status":403,"balance":30,"trace_id":"abc"}`,
		},
		{
			name:     "array value",
			problem:  forbidden(prblm.WithExt("accounts", []string{"/a/1", "/a/2"})),
			expected: `{"title":"Forbidden","status":403,"accounts":["/a/1","/a/2"]}`,
		},
		{
			name:     "only extensions",
			problem:  prblm.New(0, prblm.WithExt("balance", 30)),
			expected: `{"balance":30}`,
		},
		{
			name:     "json struct tags",
			problem:  prblm.New(422, prblm.WithExt("errors", []fieldError{{Detail: "must be positive", Pointer: "#/age"}})),
			expected: `{"status":422,"errors":[{"detail":"must be positive","pointer":"#/age"}]}`,
		},
		{
			name:     "nil value",
			problem:  prblm.New(422, prblm.WithExt("errors", nil)),
			expected: `{"status":422,"errors":null}`,
		},
		{name: "channel", problem: prblm.New(422, prblm.WithExt("errors", make(chan int))), fails: true},
		{name: "nan", problem: prblm.New(422, prblm.WithExt("errors", math.NaN())), fails: true},
		{name: "pointer cycle", problem: prblm.New(422, prblm.WithExt("errors", cycle)), fails: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.problem)
			if tt.fails {
				if err == nil {
					t.Fatalf("json.Marshal() = %s, want an error", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			if string(got) != tt.expected {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.expected)
			}
		})
	}
}

// forbidden returns a finalized 403 problem with the given fields.
func forbidden(fields ...prblm.Field) *prblm.Problem {
	p := prblm.New(403, fields...)
	p.Title = "Forbidden"

	return p
}

// problem-document / Scenario: Decode JSON.
func TestProblem_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected *prblm.Problem
		ext      map[string]any
		fails    bool
	}{
		{
			name:     "standard members",
			input:    `{"status":404,"title":"Not Found"}`,
			expected: &prblm.Problem{Status: 404, Title: "Not Found"},
		},
		{
			name:     "wrong typed status and an extension",
			input:    `{"status":"404","title":"Not Found","balance":30}`,
			expected: &prblm.Problem{Title: "Not Found"},
			ext:      map[string]any{"balance": json.Number("30")},
		},
		{
			name:     "number beyond float64 in an extension",
			input:    `{"title":"Not Found","big":1e400}`,
			expected: &prblm.Problem{Title: "Not Found"},
			ext:      map[string]any{"big": json.Number("1e400")},
		},
		{
			name:     "wrong typed title",
			input:    `{"title":42,"detail":"d"}`,
			expected: &prblm.Problem{Detail: "d"},
		},
		{
			name:     "fractional status",
			input:    `{"status":404.5,"type":"https://example.com/probs/x","instance":"/a"}`,
			expected: &prblm.Problem{Type: "https://example.com/probs/x", Instance: "/a"},
		},
		{name: "empty object", input: `{}`, expected: &prblm.Problem{}},
		{name: "array", input: `[1,2]`, fails: true},
		{name: "string", input: `"text"`, fails: true},
		{name: "truncated", input: `{"status":`, fails: true},
		{name: "empty input", input: ``, fails: true},
		{name: "null", input: `null`, fails: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var p prblm.Problem

			err := json.Unmarshal([]byte(tt.input), &p)
			if tt.fails {
				if err == nil {
					t.Fatalf("json.Unmarshal(%s) = %+v, want an error", tt.input, p)
				}

				return
			}

			if err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", tt.input, err)
			}

			got := [5]any{p.Type, p.Title, p.Status, p.Detail, p.Instance}

			want := [5]any{tt.expected.Type, tt.expected.Title, tt.expected.Status, tt.expected.Detail, tt.expected.Instance}
			if got != want {
				t.Errorf("decoded members = %v, want %v", got, want)
			}

			if p.EffectiveType() != tt.expected.EffectiveType() {
				t.Errorf("EffectiveType() = %q, want %q", p.EffectiveType(), tt.expected.EffectiveType())
			}

			for _, name := range []string{"status", "title", "type", "detail", "instance"} {
				if _, ok := p.Ext(name); ok {
					t.Errorf("standard member %q was kept as an extension", name)
				}
			}

			for name, expected := range tt.ext {
				if v, ok := p.Ext(name); !ok || v != expected {
					t.Errorf("Ext(%q) = %v, %v; want %v, true", name, v, ok, expected)
				}
			}
		})
	}
}

func FuzzUnmarshalJSON(f *testing.F) {
	for _, seed := range []string{
		`{"status":404,"title":"Not Found"}`,
		`{"status":"404","balance":30}`,
		`{"title":42,"detail":"d","nested":{"a":[1,null,"x"]}}`,
		`{}`, `[1,2]`, `null`, `{"status":`, ``,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		var p prblm.Problem
		if err := json.Unmarshal(input, &p); err != nil {
			return
		}

		if _, err := json.Marshal(p); err != nil {
			t.Fatalf("decoded %q but cannot encode it again: %v", input, err)
		}
	})
}

func TestProblem_UnmarshalJSON_TrailingData(t *testing.T) {
	t.Parallel()

	var p prblm.Problem
	if err := p.UnmarshalJSON([]byte(`{"status":404} {"status":500}`)); err == nil {
		t.Fatalf("UnmarshalJSON with trailing data = %+v, want an error", p)
	}
}
