package negotiate

import (
	"slices"
	"strings"
	"testing"
)

// Select runs on every error response, so parsing a header must not
// allocate.
//
//nolint:paralleltest // AllocsPerRun counts allocations process-wide; parallel tests would skew it.
func TestSelect_NoAllocations(t *testing.T) {
	offers := []string{"application/problem+json", "application/json", "application/problem+xml", "application/xml", "text/xml"}

	for _, accept := range []string{"", "*/*", "application/json", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"} {
		if n := testing.AllocsPerRun(100, func() { Select(accept, offers) }); n != 0 {
			t.Errorf("Select(%q) allocates %v times, want 0", accept, n)
		}
	}
}

func FuzzSelect(f *testing.F) {
	offers := []string{"application/problem+json", "application/json", "application/problem+xml", "application/xml", "text/xml"}

	for _, seed := range []string{
		"", "*/*", "application/json;q=0.5, application/xml", "APPLICATION/XML", ";;;,,,",
		"application/xml;q=abc", "application/xml;q=1.000", "*/xml", "text/*;q=0.001",
		strings.Repeat("text/html,", 820),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, accept string) {
		got := Select(accept, offers)
		if got != "" && !slices.Contains(offers, got) {
			t.Fatalf("Select(%q) = %q, not an offer", accept, got)
		}

		ranges := parse(nil, accept)
		if len(ranges) > maxElements {
			t.Fatalf("parse(%q) returned %d ranges, limit %d", accept, len(ranges), maxElements)
		}

		for _, r := range ranges {
			if r.q < 0 || r.q > 1000 || !isToken(r.typ) || !isToken(r.subtype) {
				t.Fatalf("parse(%q) returned invalid range %+v", accept, r)
			}
		}
	})
}
