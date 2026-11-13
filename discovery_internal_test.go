package prblm

import (
	"fmt"
	"html/template"
	"strings"
	"testing"
)

// Murphy rows for type-discovery / Scenario: Conditional requests: the
// edges of If-None-Match parsing that the scenario rows do not reach.
func TestMatchesETag(t *testing.T) {
	t.Parallel()

	const etag = `"0123456789abcdef"`

	tests := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{name: "exact", ifNoneMatch: etag, want: true},
		{name: "weak", ifNoneMatch: "W/" + etag, want: true},
		{name: "any", ifNoneMatch: "*", want: true},
		{name: "tabs and commas", ifNoneMatch: "\t,\t" + etag, want: true},
		{name: "comma inside a tag", ifNoneMatch: `"a,b", ` + etag, want: true},
		{name: "last of three", ifNoneMatch: `"a", W/"b", ` + etag, want: true},
		{name: "empty"},
		{name: "unquoted", ifNoneMatch: "0123456789abcdef"},
		{name: "unterminated", ifNoneMatch: `"0123456789abcdef`},
		{name: "bare weak prefix", ifNoneMatch: "W/"},
		{name: "garbage before the tag", ifNoneMatch: `;;;, ` + etag},
		{name: "prefix of the tag", ifNoneMatch: `"0123456789abcde"`},
		{name: "lowercase weak prefix", ifNoneMatch: "w/" + etag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesETag(tt.ifNoneMatch, etag); got != tt.want {
				t.Errorf("matchesETag(%q) = %v, want %v", tt.ifNoneMatch, got, tt.want)
			}
		})
	}
}

// Design D4 failure mode: a page that cannot be rendered panics when the
// handler is created, naming the resource.
func TestRenderPage_Failure(t *testing.T) {
	t.Parallel()

	broken := template.Must(template.New("broken").Parse(`{{.Missing}}`))

	tests := []struct {
		render func()
		name   string
	}{
		{name: "json", render: func() { jsonPage("contracts/locked", make(chan int)) }},
		{name: "html", render: func() { htmlPage("contracts/locked", broken, struct{}{}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if msg := fmt.Sprint(recover()); !strings.Contains(msg, `"contracts/locked"`) {
					t.Errorf("panic = %q, want a message naming the sub-type", msg)
				}
			}()

			tt.render()
		})
	}
}

// namespacePath is the path discovery is served under (type-discovery /
// Scenario: Discovery state and Absolute namespace).
func TestNamespacePath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/v1/api/probs":                 "/v1/api/probs",
		"https://kartala.id/probs":      "/probs",
		"http://localhost:8080/a/probs": "/a/probs",
	}

	for ns, want := range tests {
		t.Run(ns, func(t *testing.T) {
			t.Parallel()

			if got := namespacePath(ns); got != want {
				t.Errorf("namespacePath(%q) = %q, want %q", ns, got, want)
			}
		})
	}
}
