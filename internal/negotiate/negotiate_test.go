package negotiate_test

import (
	"strings"
	"testing"

	"github.com/kartaladev/prblm/internal/negotiate"
)

// offers is the order the library offers its formats in: JSON first, so it
// wins ties.
var offers = []string{
	"application/problem+json",
	"application/json",
	"application/problem+xml",
	"application/xml",
	"text/xml",
}

// problem-response / Scenario: Negotiated format.
func TestSelect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		accept   string
		expected string
	}{
		// Examples: JSON.
		{name: "none", accept: "", expected: "application/problem+json"},
		{name: "any", accept: "*/*", expected: "application/problem+json"},
		{name: "json", accept: "application/json", expected: "application/json"},
		{name: "problem json", accept: "application/problem+json", expected: "application/problem+json"},
		{name: "application wildcard", accept: "application/*", expected: "application/problem+json"},
		{name: "xml and json tie", accept: "application/xml, application/json", expected: "application/json"},
		{name: "any beats a lower xml", accept: "application/xml;q=0.5, */*", expected: "application/problem+json"},
		// Examples: XML.
		{name: "problem xml", accept: "application/problem+xml", expected: "application/problem+xml"},
		{name: "xml", accept: "application/xml", expected: "application/xml"},
		{name: "text xml", accept: "text/xml", expected: "text/xml"},
		{name: "json lower than xml", accept: "application/json;q=0.5, application/xml", expected: "application/xml"},
		{name: "json excluded", accept: "application/json;q=0, application/xml", expected: "application/xml"},
		{name: "upper case", accept: "APPLICATION/XML", expected: "application/xml"},
		// Murphy rows.
		{name: "specific exclusion beats wildcard", accept: "application/problem+json;q=0, application/json;q=0, */*;q=0.1", expected: "application/problem+xml"},
		{name: "spaces around parameters", accept: " application/xml ; q=0.9 , application/json ; Q=0.8 ", expected: "application/xml"},
		{name: "other parameters ignored", accept: "application/xml; charset=utf-8", expected: "application/xml"},
		{name: "blank header", accept: "   ", expected: "application/problem+json"},
		{name: "q bounds with three decimals", accept: "application/json;q=0.999, application/xml;q=1.000", expected: "application/xml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := negotiate.Select(tt.accept, offers); got != tt.expected {
				t.Errorf("Select(%q) = %q, want %q", tt.accept, got, tt.expected)
			}
		})
	}
}

// problem-response / Scenario: Fallback to JSON. An empty result makes the
// caller fall back to JSON.
func TestSelect_Malformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		accept   string
		expected string
	}{
		{name: "unsupported type", accept: "image/png", expected: ""},
		{name: "everything excluded", accept: "application/json;q=0, application/xml;q=0", expected: ""},
		{name: "only separators", accept: ";;;,,,", expected: ""},
		{name: "q not a number", accept: "application/xml;q=abc", expected: ""},
		{name: "q above one", accept: "application/xml;q=2", expected: ""},
		{name: "8 KiB header", accept: strings.Repeat("text/html,", 820), expected: ""},
		// Murphy rows: invalid q values are dropped, not clamped or rounded.
		{name: "negative q", accept: "application/xml;q=-1", expected: ""},
		{name: "q with four decimals", accept: "application/xml;q=0.5000", expected: ""},
		{name: "q in exponent form", accept: "application/xml;q=1e-1", expected: ""},
		{name: "q one with fraction", accept: "application/xml;q=1.001", expected: ""},
		{name: "q infinity", accept: "application/xml;q=Inf", expected: ""},
		{name: "q nan", accept: "application/xml;q=NaN", expected: ""},
		{name: "empty q", accept: "application/xml;q=", expected: ""},
		{name: "q with a letter", accept: "application/xml;q=0.5x", expected: ""},
		{name: "dropped range leaves the rest", accept: "application/xml;q=abc, application/json;q=0.1", expected: "application/json"},
		// Murphy rows: malformed media ranges.
		{name: "wildcard type with subtype", accept: "*/xml", expected: ""},
		{name: "missing subtype", accept: "application/", expected: ""},
		{name: "missing type", accept: "/xml", expected: ""},
		{name: "extra slash", accept: "application/xml/x", expected: ""},
		{name: "space inside type", accept: "application /xml", expected: ""},
		{name: "no slash", accept: "xml", expected: ""},
		{name: "control character", accept: "application/xml\x00", expected: ""},
		// Murphy rows: limits.
		{
			name:     "range beyond the 32nd ignored",
			accept:   strings.Repeat("image/png,", 32) + "application/xml",
			expected: "",
		},
		{
			name:     "range within the first 32 kept",
			accept:   strings.Repeat("image/png,", 31) + "application/xml",
			expected: "application/xml",
		},
		{
			// The limit falls right after "application/json": parsing the cut
			// element would drop its q=0 and select JSON.
			name:     "range cut by the 4 KiB limit dropped",
			accept:   "image/png;a=" + strings.Repeat("x", 4066) + ", application/json;q=0",
			expected: "",
		},
		{
			name:     "range beyond 4 KiB ignored",
			accept:   "image/png;a=" + strings.Repeat("x", 4100) + ", application/xml",
			expected: "",
		},
		{
			name:     "range within 4 KiB kept",
			accept:   "image/png;a=" + strings.Repeat("x", 4000) + ", application/xml",
			expected: "application/xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := negotiate.Select(tt.accept, offers); got != tt.expected {
				t.Errorf("Select(%q) = %q, want %q", truncate(tt.accept), got, tt.expected)
			}
		})
	}
}

// truncate shortens long headers in failure messages.
func truncate(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}

	return s
}
