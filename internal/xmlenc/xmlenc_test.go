package xmlenc_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/kartaladev/prblm/internal/xmlenc"
)

// encode encodes value as the element name and returns the XML.
func encode(name string, value any) (string, error) {
	var buf bytes.Buffer

	e := xml.NewEncoder(&buf)
	if err := xmlenc.Encode(e, name, value); err != nil {
		return "", err
	}

	if err := e.Flush(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

type fieldError struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer"`
}

// problem-document / Scenario: XML encoding.
func TestEncode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ext      string
		value    any
		expected string
	}{
		{name: "number", ext: "balance", value: 30, expected: `<balance>30</balance>`},
		{name: "array", ext: "accounts", value: []string{"/a/1", "/a/2"}, expected: `<accounts><i>/a/1</i><i>/a/2</i></accounts>`},
		{
			name:     "array of objects",
			ext:      "errors",
			value:    []fieldError{{Detail: "must be positive", Pointer: "#/age"}},
			expected: `<errors><i><detail>must be positive</detail><pointer>#/age</pointer></i></errors>`,
		},
		{name: "object sorted by key", ext: "meta", value: map[string]int{"b": 1, "a": 2}, expected: `<meta><a>2</a><b>1</b></meta>`},
		{name: "null", ext: "note", value: nil, expected: `<note></note>`},
		{name: "escaped text", ext: "text", value: "a<b&c", expected: `<text>a&lt;b&amp;c</text>`},
		// Murphy rows.
		{name: "booleans", ext: "flags", value: []bool{true, false}, expected: `<flags><i>true</i><i>false</i></flags>`},
		{name: "fraction", ext: "ratio", value: 1.5, expected: `<ratio>1.5</ratio>`},
		{name: "number beyond float64 kept as written", ext: "big", value: json.RawMessage(`1e400`), expected: `<big>1e400</big>`},
		{name: "nested arrays", ext: "grid", value: [][]int{{1, 2}, {}}, expected: `<grid><i><i>1</i><i>2</i></i><i></i></grid>`},
		{name: "empty object", ext: "meta", value: map[string]int{}, expected: `<meta></meta>`},
		{name: "null inside an array", ext: "items", value: []any{nil, "x"}, expected: `<items><i></i><i>x</i></items>`},
		{name: "character not allowed in XML", ext: "text", value: "a\x00b", expected: "<text>a�b</text>"},
		{name: "html-sensitive text not mangled", ext: "text", value: "<script>", expected: `<text>&lt;script&gt;</text>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := encode(tt.ext, tt.value)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			if got != tt.expected {
				t.Errorf("Encode() = %s, want %s", got, tt.expected)
			}
		})
	}
}

// problem-document / Scenario: Tag options respected in XML.
func TestEncode_TagOptions(t *testing.T) {
	t.Parallel()

	type item struct {
		Code   string `json:"code"`
		Hint   string `json:"hint,omitempty"`
		Secret string `json:"-"`
	}

	got, err := encode("item", item{Code: "x1", Secret: "s3cr3t"})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	tests := []struct {
		name     string
		fragment string
		present  bool
	}{
		{name: "renamed field", fragment: `<item><code>x1</code></item>`, present: true},
		{name: "omitempty field", fragment: "hint", present: false},
		{name: "ignored field", fragment: "s3cr3t", present: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if strings.Contains(got, tt.fragment) != tt.present {
				t.Errorf("Encode() = %s; contains %q = %v, want %v", got, tt.fragment, !tt.present, tt.present)
			}
		})
	}
}

// problem-document / Scenario: Invalid nested key.
func TestEncode_InvalidName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ext   string
		key   string
		valid bool
	}{
		{name: "valid key", ext: "meta", key: "valid_key", valid: true},
		{name: "dot", ext: "meta", key: "file.name"},
		{name: "digit first", ext: "meta", key: "1st"},
		{name: "space", ext: "meta", key: "a b"},
		{name: "empty", ext: "meta", key: ""},
		// Murphy rows.
		{name: "non-ascii", ext: "meta", key: "nãme"},
		{name: "markup", ext: "meta", key: "a><b"},
		{name: "underscore first", ext: "meta", key: "_a"},
		{name: "one letter", ext: "meta", key: "a", valid: true},
		{name: "invalid element name itself", ext: "a b", key: "ok", valid: false},
		{name: "inside an array item", ext: "list", key: "a b", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var value any = map[string]int{tt.key: 1}
			if tt.ext == "list" {
				value = []any{value}
			}

			got, err := encode(tt.ext, value)
			if tt.valid {
				if err != nil {
					t.Errorf("Encode() error = %v, want encoded", err)
				}

				return
			}

			if !errors.Is(err, xmlenc.ErrInvalidName) {
				t.Errorf("Encode() = %s, %v; want an error wrapping ErrInvalidName", got, err)
			}
		})
	}
}

// Values JSON cannot encode fail the same way in XML.
func TestEncode_NotEncodable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
	}{
		{name: "chan", value: make(chan int)},
		{name: "nan", value: math.NaN()},
		{name: "infinity", value: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, err := encode("bad", tt.value); err == nil {
				t.Errorf("Encode() = %s, want an error", got)
			}
		})
	}
}

var errWrite = errors.New("write failed")

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

// A failing writer surfaces from Encode. The encoder buffers 4 KiB, so each
// row overflows the buffer at a different token.
func TestEncode_WriteFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
	}{
		{name: "at a start element", value: map[string]int{strings.Repeat("a", 5000): 1}},
		{name: "at text", value: strings.Repeat("x", 5000)},
		{name: "at an end element", value: map[string]int{strings.Repeat("a", 3000): 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := xmlenc.Encode(xml.NewEncoder(failingWriter{}), "meta", tt.value); !errors.Is(err, errWrite) {
				t.Errorf("Encode() error = %v, want %v", err, errWrite)
			}
		})
	}
}
