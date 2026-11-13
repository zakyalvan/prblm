package prblm_test

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

var errBrokenWriter = errors.New("write failed")

// brokenWriter fails every write.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errBrokenWriter }

// A failing writer surfaces from MarshalXML, wherever it fails. The encoder
// buffers 4 KiB, so each row overflows the buffer at a different token.
func TestProblem_MarshalXML_WriteFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		failed  bool // the encoder already failed before the problem
		problem prblm.Problem
	}{
		{name: "at the root element", failed: true, problem: prblm.Problem{Status: 404}},
		{name: "at a member", problem: prblm.Problem{Status: 404, Title: strings.Repeat("t", 5000)}},
		// The root start tag and title element fill the buffer to just below
		// 4 KiB, so the root end tag overflows it.
		{name: "at the root end", problem: prblm.Problem{Title: strings.Repeat("t", 4040)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := xml.NewEncoder(brokenWriter{})

			if tt.failed {
				if err := e.EncodeToken(xml.CharData(strings.Repeat("x", 5000))); !errors.Is(err, errBrokenWriter) {
					t.Fatalf("priming the encoder: %v", err)
				}
			}

			if err := e.Encode(tt.problem); !errors.Is(err, errBrokenWriter) {
				t.Errorf("Encode() error = %v, want %v", err, errBrokenWriter)
			}
		})
	}
}

// problem-document / Scenario: Decode XML.
func TestProblem_UnmarshalXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		status   int
		title    string
		ext      map[string]any // expected extensions, exactly
		rejected bool
	}{
		{
			name:   "standard members",
			input:  `<problem xmlns="urn:ietf:rfc:7807"><status>404</status><title>Not Found</title></problem>`,
			status: 404, title: "Not Found",
		},
		{
			name:   "wrong-typed status ignored, extension kept",
			input:  `<problem xmlns="urn:ietf:rfc:7807"><status>abc</status><balance>30</balance></problem>`,
			status: 0, ext: map[string]any{"balance": "30"},
		},
		{name: "other root", input: `<other/>`, rejected: true},
		// Murphy rows.
		{name: "empty input", input: ``, rejected: true},
		{name: "not well-formed", input: `<problem><status>404</status>`, rejected: true},
		{name: "no namespace", input: `<problem><status>409</status></problem>`, status: 409},
		{name: "status with spaces", input: `<problem><status> 404 </status></problem>`, status: 404},
		{name: "status with child elements ignored", input: `<problem><status><i>404</i></status></problem>`, status: 0},
		{name: "title with child elements ignored", input: `<problem><title><b>x</b></title></problem>`, title: ""},
		{name: "last duplicate wins", input: `<problem><title>a</title><title>b</title></problem>`, title: "b"},
		{
			name:  "nested values",
			input: `<problem><errors><i><detail>d</detail><pointer>#/age</pointer></i></errors><meta><a>2</a><b></b></meta><list><i>x</i><i><i>y</i></i></list></problem>`,
			ext: map[string]any{
				"errors": []any{map[string]any{"detail": "d", "pointer": "#/age"}},
				"meta":   map[string]any{"a": "2", "b": ""},
				"list":   []any{"x", []any{"y"}},
			},
		},
		{name: "attributes and comments ignored", input: `<problem lang="en"><!-- c --><status code="1">404</status></problem>`, status: 404},
		{name: "too deep", input: strings.Repeat("<a>", 10002) + strings.Repeat("</a>", 10002), rejected: true},
		{name: "too deep inside a problem", input: "<problem>" + strings.Repeat("<a>", 10001) + strings.Repeat("</a>", 10001) + "</problem>", rejected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Decoding replaces whatever the problem held.
			p := prblm.New(http.StatusTeapot, prblm.WithExt("stale", 1))

			err := xml.Unmarshal([]byte(tt.input), p)
			if tt.rejected {
				if err == nil {
					t.Errorf("Unmarshal() = %+v, want a decode error", p)
				}

				return
			}

			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if p.Status != tt.status || p.Title != tt.title {
				t.Errorf("got status %d, title %q; want %d, %q", p.Status, p.Title, tt.status, tt.title)
			}

			if got := extensions(p); !reflect.DeepEqual(got, orEmpty(tt.ext)) {
				t.Errorf("extensions = %#v, want %#v", got, orEmpty(tt.ext))
			}
		})
	}
}

// extensions returns the extension members of p, decoded from its JSON.
func extensions(p *prblm.Problem) map[string]any {
	body, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	var members map[string]any
	if err := json.Unmarshal(body, &members); err != nil {
		panic(err)
	}

	for _, name := range []string{"type", "title", "status", "detail", "instance"} {
		delete(members, name)
	}

	return members
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}

	return m
}

// A problem survives JSON → XML → Problem with its structure; XML has no
// types, so scalars come back as text.
func TestProblem_UnmarshalXML_RoundTrip(t *testing.T) {
	t.Parallel()

	var original prblm.Problem
	if err := json.Unmarshal([]byte(`{"type":"https://example.com/probs/out-of-credit","title":"Out of credit","status":403,`+
		`"detail":"Balance 30 & cost 50","instance":"/account/1","balance":30,"accounts":["/a/1","/a/2"],"meta":{"b":true,"a":null}}`), &original); err != nil {
		t.Fatal(err)
	}

	encoded, err := xml.Marshal(&original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded prblm.Problem
	if err := xml.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", encoded, err)
	}

	if decoded.Type != original.Type || decoded.Title != original.Title || decoded.Status != original.Status ||
		decoded.Detail != original.Detail || decoded.Instance != original.Instance {
		t.Errorf("standard members = %+v, want %+v", decoded, original)
	}

	expected := map[string]any{
		"balance":  "30",
		"accounts": []any{"/a/1", "/a/2"},
		"meta":     map[string]any{"a": "", "b": "true"},
	}
	if got := extensions(&decoded); !reflect.DeepEqual(got, expected) {
		t.Errorf("extensions = %#v, want %#v", got, expected)
	}
}

func FuzzUnmarshalXML(f *testing.F) {
	for _, seed := range []string{
		``, `<other/>`, `<problem/>`,
		`<problem xmlns="urn:ietf:rfc:7807"><status>404</status><title>Not Found</title></problem>`,
		`<problem><list><i>x</i><i><i>y</i></i></list><meta><a>2</a></meta></problem>`,
		`<problem><status><i>404</i></status>`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		var p prblm.Problem
		if err := xml.Unmarshal([]byte(input), &p); err != nil {
			return
		}

		// Whatever decoded must encode as JSON again.
		if _, err := json.Marshal(&p); err != nil {
			t.Fatalf("decoded %q into a problem that cannot be encoded: %v", input, err)
		}
	})
}
