package prblm

import (
	"context"
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

// type-discovery / Scenario: Pages rendered on first use (design D1, D4):
// a handler that only answers errors and panics, as the one gin's Recovery
// builds, never renders the documentation, and the first use of discovery
// renders it once, whichever method uses it first.
func TestDiscovery_RenderedOnFirstUse(t *testing.T) {
	t.Parallel()

	uses := map[string]func(h *Handler){
		"DiscoveryPath": func(h *Handler) { h.DiscoveryPath() },
		"Discovery":     func(h *Handler) { h.Discovery() },
		"RespondDiscovery": func(h *Handler) {
			h.RespondDiscovery(t.Context(), DiscoveryRequest{Method: "GET", Path: "/v1/api/probs"})
		},
	}

	for first, use := range uses {
		t.Run(first, func(t *testing.T) {
			t.Parallel()

			h := NewHandler(WithNamespace("/v1/api/probs"), WithTypes(DefineType("out-of-credit", 403, "Out of credit", "")))

			// What gin's Recovery does with its handler: answer errors and
			// panics, nothing else.
			req := Request{Method: "GET", Path: "/v1/vessels/7"}
			h.Respond(t.Context(), req, &PanicError{Value: "boom"})
			h.respondPanic(t.Context(), req, &PanicError{Value: "boom"})

			if n := h.discovery.renders.Load(); n != 0 {
				t.Fatalf("rendered %d times before any use of discovery, want 0", n)
			}

			use(h)

			for _, again := range uses {
				again(h)
			}

			if n := h.discovery.renders.Load(); n != 1 {
				t.Errorf("rendered %d times after using discovery, want 1", n)
			}
		})
	}
}

// Design D4 failure mode: when rendering fails on first use, every later
// use fails the same way, naming the resource, rather than finding no
// pages inside a request.
func TestDiscovery_RenderFailureRepeats(t *testing.T) {
	t.Parallel()

	d := newLazyDiscovery("/v1/api/probs", func() site { panic(`prblm: rendering the documentation of "x": boom`) })

	for i := range 2 {
		msg := func() (msg string) {
			defer func() { msg = fmt.Sprint(recover()) }()

			d.site()

			return ""
		}()

		if !strings.Contains(msg, `"x": boom`) {
			t.Errorf("use %d: panic = %q, want the rendering failure", i+1, msg)
		}
	}

	if n := d.renders.Load(); n != 1 {
		t.Errorf("rendered %d times, want 1", n)
	}
}

// Murphy row for type-discovery / Scenario: Shadowed documentation: without
// a hook, a conflicting route builds no note and is not remembered, so the
// request a route answers pays nothing for it.
//
//nolint:paralleltest // AllocsPerRun counts allocations process-wide; parallel tests would skew it.
func TestNoteShadowedRoute_NoHook(t *testing.T) {
	h := NewHandler(WithNamespace("/v1/api/probs"), WithTypes(DefineType("out-of-credit", 403, "Out of credit", "")))
	req := DiscoveryRequest{Method: "GET", Path: "/v1/api/probs/out-of-credit"}

	h.DiscoveryPath() // render, as every adapter does at construction

	allocs := testing.AllocsPerRun(100, func() {
		h.NoteShadowedRoute(t.Context(), "/v1/api/probs/out-of-credit", req)
	})

	if allocs != 0 {
		t.Errorf("allocations = %v, want 0", allocs)
	}

	if n := len(h.discovery.noted.routes); n != 0 {
		t.Errorf("remembered %d routes, want none", n)
	}
}

// Design D4: noting a route that hides documentation reads the sub-types
// registered in NewHandler, never the rendered pages, so an adapter that
// only notes, such as fiber's Discovery middleware, renders nothing.
func TestNoteShadowedRoute_DoesNotRender(t *testing.T) {
	t.Parallel()

	notes := 0
	h := NewHandler(
		WithNamespace("/v1/api/probs"),
		WithTypes(DefineType("out-of-credit", 403, "Out of credit", "")),
		OnError(func(context.Context, error, *Problem) { notes++ }),
	)

	for _, path := range []string{"/v1/api/probs/out-of-credit", "/v1/api/probs", "/v1/api/probs/", "/v1/api/probs/unknown"} {
		h.NoteShadowedRoute(t.Context(), path, DiscoveryRequest{Method: "GET", Path: path})
	}

	if n := h.discovery.renders.Load(); n != 0 || notes != 3 {
		t.Errorf("rendered %d times with %d notes, want 0 renders and 3 notes (page, index, index with slash)", n, notes)
	}
}
