package xmlenc

import (
	"encoding/json"
	"testing"
)

// The decode guard of Encode: json.Marshal validates what it produces, so
// Encode never meets malformed JSON, but should that change, decodeTree
// reports it instead of encoding a partial tree.
func TestDecodeTree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		isValid bool
	}{
		{name: "object", input: `{"a":1,"b":[true,null]}`, isValid: true},
		{name: "number kept exact", input: `12345678901234567890`, isValid: true},
		{name: "truncated object", input: `{"a":`},
		{name: "not json", input: `<xml/>`},
		{name: "empty", input: ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tree, err := decodeTree([]byte(tt.input))
			if (err == nil) != tt.isValid {
				t.Fatalf("decodeTree(%q) error = %v, want valid %v", tt.input, err, tt.isValid)
			}

			if n, ok := tree.(json.Number); tt.name == "number kept exact" && (!ok || n.String() != tt.input) {
				t.Errorf("decodeTree(%q) = %#v, want the exact json.Number", tt.input, tree)
			}
		})
	}
}
