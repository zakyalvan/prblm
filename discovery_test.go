package prblm_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kartaladev/prblm"
)

// outOfCredit is the second definition of the type-discovery scenarios.
var outOfCredit = prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit", "Balance too low.")

// discover sends method path with the headers to the discovery handler of h
// and returns the response.
func discover(t *testing.T, h *prblm.Handler, method, path string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody)
	maps.Copy(req.Header, header)

	rec := httptest.NewRecorder()
	h.Discovery().ServeHTTP(rec, req)

	return rec
}

// accept returns the headers of a request with Accept set to value.
func accept(value string) http.Header {
	return http.Header{"Accept": {value}}
}

// type-discovery / Scenario: Discovery state.
func TestDiscovery_State(t *testing.T) {
	t.Parallel()

	ns := prblm.WithNamespace("/v1/api/probs")

	tests := []struct {
		name   string
		opts   []prblm.Option
		path   string
		status int
	}{
		{
			name:   "on",
			opts:   []prblm.Option{ns, prblm.WithTypes(outOfCredit)},
			path:   "/v1/api/probs",
			status: http.StatusOK,
		},
		{
			name:   "on with a trailing slash",
			opts:   []prblm.Option{prblm.WithNamespace("/v1/api/probs/"), prblm.WithTypes(outOfCredit)},
			path:   "/v1/api/probs",
			status: http.StatusOK,
		},
		{name: "off without options", status: http.StatusNotFound},
		{name: "off without types", opts: []prblm.Option{ns}, status: http.StatusNotFound},
		{name: "off with no types", opts: []prblm.Option{ns, prblm.WithTypes()}, status: http.StatusNotFound},
		{
			name:   "off after the types",
			opts:   []prblm.Option{ns, prblm.WithTypes(outOfCredit), prblm.WithoutDiscovery()},
			status: http.StatusNotFound,
		},
		{
			name:   "off before the types",
			opts:   []prblm.Option{prblm.WithoutDiscovery(), ns, prblm.WithTypes(outOfCredit)},
			status: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(tt.opts...)

			if got := h.DiscoveryPath(); got != tt.path {
				t.Errorf("DiscoveryPath() = %q, want %q", got, tt.path)
			}

			rec := discover(t, h, http.MethodGet, "/v1/api/probs/out-of-credit", nil)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if tt.status == http.StatusNotFound && rec.Header().Get("Content-Type") != problemJSONType {
				t.Errorf("Content-Type = %q, want a problem", rec.Header().Get("Content-Type"))
			}
		})
	}
}

// documented returns a handler with namespace /v1/api/probs and the
// definitions blacklistedSupplier and outOfCredit registered, plus opts.
func documented(opts ...prblm.Option) *prblm.Handler {
	return prblm.NewHandler(append([]prblm.Option{
		prblm.WithNamespace("/v1/api/probs"),
		prblm.WithTypes(blacklistedSupplier, outOfCredit),
	}, opts...)...)
}

const (
	htmlType = "text/html; charset=utf-8"
	jsonType = "application/json"
	// blacklistedDoc is the JSON documentation of blacklistedSupplier.
	blacklistedDoc = `{"type":"/v1/api/probs/contracts/blacklisted-supplier","subtype":"contracts/blacklisted-supplier","status":409,"title":"Supplier is blacklisted","description":"The supplier is on the procurement blacklist."}`
)

// type-discovery / Scenario: Documentation served.
func TestDiscovery_Documentation(t *testing.T) {
	t.Parallel()

	h := documented()

	tests := []struct {
		name        string
		accept      string
		contentType string
		contains    []string
		exact       string
	}{
		{
			name:        "no accept",
			contentType: htmlType,
			contains:    []string{"Supplier is blacklisted", "409", "The supplier is on the procurement blacklist."},
		},
		{name: "html", accept: "text/html", contentType: htmlType, contains: []string{"Supplier is blacklisted"}},
		{name: "anything", accept: "*/*", contentType: htmlType, contains: []string{"Supplier is blacklisted"}},
		{name: "json less preferred", accept: "application/json;q=0.5, text/html", contentType: htmlType, contains: []string{"Supplier is blacklisted"}},
		{name: "tie", accept: "application/json, text/html", contentType: htmlType, contains: []string{"Supplier is blacklisted"}},
		{
			name:        "a browser",
			accept:      "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			contentType: htmlType,
			contains: []string{
				"<!DOCTYPE html>", "<title>Supplier is blacklisted</title>", "409", "The supplier is on the procurement blacklist.",
			},
		},
		{name: "json", accept: "application/json", contentType: jsonType, exact: blacklistedDoc},
		{name: "json preferred", accept: "text/html;q=0.5, application/json", contentType: jsonType, exact: blacklistedDoc},
		{name: "malformed", accept: ";;;", contentType: htmlType, contains: []string{"Supplier is blacklisted"}},
		{name: "unrelated", accept: "image/png", contentType: htmlType, contains: []string{"Supplier is blacklisted"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var header http.Header
			if tt.accept != "" {
				header = accept(tt.accept)
			}

			rec := discover(t, h, http.MethodGet, "/v1/api/probs/contracts/blacklisted-supplier", header)
			body := rec.Body.String()

			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != tt.contentType {
				t.Fatalf("response %d %q, want 200 %q", rec.Code, rec.Header().Get("Content-Type"), tt.contentType)
			}

			if tt.exact != "" && body != tt.exact {
				t.Errorf("body = %s, want %s", body, tt.exact)
			}

			for _, want := range tt.contains {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q:\n%s", want, body)
				}
			}
		})
	}
}

// type-discovery / Scenario: HEAD request.
func TestDiscovery_Head(t *testing.T) {
	t.Parallel()

	h := documented()

	tests := []struct {
		path        string
		contentType string
		status      int
	}{
		{path: "/v1/api/probs/contracts/blacklisted-supplier", status: http.StatusOK, contentType: htmlType},
		{path: "/v1/api/probs/contracts/unknown", status: http.StatusNotFound, contentType: problemJSONType},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			rec := discover(t, h, http.MethodHead, tt.path, nil)

			if rec.Code != tt.status || rec.Header().Get("Content-Type") != tt.contentType || rec.Body.Len() != 0 {
				t.Errorf("response %d %q with %d body bytes, want %d %q and no body",
					rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len(), tt.status, tt.contentType)
			}
		})
	}
}

// type-discovery / Scenario: Index served.
func TestDiscovery_Index(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		types []*prblm.Type
		path  string
		json  string
	}{
		{
			name:  "sorted by sub-type",
			types: []*prblm.Type{outOfCredit, blacklistedSupplier},
			path:  "/v1/api/probs",
			json: `{"types":[` +
				`{"type":"/v1/api/probs/contracts/blacklisted-supplier","subtype":"contracts/blacklisted-supplier","status":409,"title":"Supplier is blacklisted"},` +
				`{"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit"}]}`,
		},
		{
			name:  "trailing slash",
			types: []*prblm.Type{outOfCredit},
			path:  "/v1/api/probs/",
			json:  `{"types":[{"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(tt.types...))
			rec := discover(t, h, http.MethodGet, tt.path, accept(jsonType))

			if rec.Code != http.StatusOK || rec.Body.String() != tt.json {
				t.Errorf("response %d %s, want 200 %s", rec.Code, rec.Body.String(), tt.json)
			}
		})
	}
}

// type-discovery / Scenario: HTML index links.
func TestDiscovery_IndexLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		namespace string
		path      string
		href      string
	}{
		{namespace: "/v1/api/probs", path: "/v1/api/probs", href: "/v1/api/probs/out-of-credit"},
		{namespace: "https://kartala.id/probs", path: "/probs", href: "https://kartala.id/probs/out-of-credit"},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithNamespace(tt.namespace), prblm.WithTypes(outOfCredit))
			rec := discover(t, h, http.MethodGet, tt.path, accept("text/html"))

			if want := `href="` + tt.href + `"`; rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
				t.Errorf("response %d without %s:\n%s", rec.Code, want, rec.Body.String())
			}
		})
	}
}

// type-discovery / Scenario: Unknown type.
func TestDiscovery_Unknown(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/v1/api/probs/contracts/unknown",
		"/v1/api/probs/contracts/blacklisted-supplier/",
		"/v1/api/probs/contracts/blacklisted-supplier/extra",
		"/v1/api/probs/Contracts/Blacklisted-Supplier",
		"/v1/api/probs/contracts",
		"/v1/api/probs/contracts/%2e%2e/out-of-credit",
		"/v1/api/probs/contracts/blacklisted-supplier%00",
		// Design D4 Murphy rows: encodings of a registered sub-type are not
		// its URL, so every document has exactly one.
		"/v1/api/probs/contracts%2Fblacklisted-supplier",
		"/v1/api/probs/out%2Dof%2Dcredit",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			var r recorder

			rec := discover(t, documented(prblm.OnError(r.hook)), http.MethodGet, path, nil)
			m := decodeMembers(t, rec.Body.Bytes())

			if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != problemJSONType || m["title"] != "Not Found" {
				t.Errorf("response %d %q %v, want a 404 problem titled Not Found", rec.Code, rec.Header().Get("Content-Type"), m)
			}

			if calls := r.recorded(); len(calls) != 0 {
				t.Errorf("hook calls = %+v, want none", calls)
			}
		})
	}
}

// type-discovery / Scenario: Discovery problems never reach the hook.
func TestDiscovery_SilentHook(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/contracts/blacklisted-supplier"

	panicking := func(context.Context, *prblm.Problem) { panic("enricher") }
	unencodable := func(_ context.Context, p *prblm.Problem) { _ = p.Set("bad", make(chan int)) }
	badType := func(_ context.Context, p *prblm.Problem) { p.Type = "Bad Type" }

	tests := []struct {
		enricher func(context.Context, *prblm.Problem)
		name     string
		method   string
		path     string
		status   int
	}{
		{name: "panicking enricher on a 404", enricher: panicking, method: http.MethodGet, path: "/v1/api/probs/contracts/unknown", status: http.StatusNotFound},
		{name: "panicking enricher on a 405", enricher: panicking, method: http.MethodPost, path: doc, status: http.StatusMethodNotAllowed},
		{name: "unencodable extension", enricher: unencodable, method: http.MethodGet, path: "/v1/api/probs/contracts/unknown", status: http.StatusInternalServerError},
		{name: "invalid type", enricher: badType, method: http.MethodGet, path: "/v1/api/probs/contracts/unknown", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := documented(prblm.OnError(r.hook), prblm.WithEnrichers(tt.enricher))
			rec := discover(t, h, tt.method, tt.path, nil)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			if calls := r.recorded(); len(calls) != 0 {
				t.Errorf("hook calls = %+v, want none", calls)
			}
		})
	}
}

// type-discovery / Scenario: Wrong method.
func TestDiscovery_Method(t *testing.T) {
	t.Parallel()

	h := documented()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			rec := discover(t, h, method, "/v1/api/probs/contracts/blacklisted-supplier", nil)

			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" ||
				rec.Header().Get("Content-Type") != problemJSONType {
				t.Errorf("response %d Allow %q %q, want a 405 problem with Allow: GET, HEAD",
					rec.Code, rec.Header().Get("Allow"), rec.Header().Get("Content-Type"))
			}
		})
	}
}

// type-discovery / Scenario: Hostile definition text.
func TestDiscovery_Hostile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		title       string
		description string
		accept      string
		contains    string
		absent      string
	}{
		{
			name:        "script in the description",
			title:       "Locked",
			description: "<script>alert(1)</script>",
			accept:      "text/html",
			contains:    "&lt;script&gt;alert(1)&lt;/script&gt;",
			absent:      "<script>",
		},
		{
			name:        "markup in the title",
			title:       "<img src=x onerror=alert(1)>",
			description: "x",
			accept:      "text/html",
			contains:    "&lt;img src=x onerror=alert(1)&gt;",
			absent:      "<img",
		},
		{
			name:        "quotes and markup in json",
			title:       "Locked",
			description: `"quoted" & <tagged>`,
			accept:      jsonType,
			contains:    `"\"quoted\" \u0026 \u003ctagged\u003e"`,
			absent:      "<tagged>",
		},
		{
			name:        "line breaks",
			title:       "Locked",
			description: "line one\nline two",
			accept:      "text/html",
			contains:    "line one",
			absent:      "<script>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hostile := prblm.DefineType("contracts/locked", http.StatusConflict, tt.title, tt.description)
			h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(hostile))

			rec := discover(t, h, http.MethodGet, "/v1/api/probs/contracts/locked", accept(tt.accept))
			body := rec.Body.String()

			if !strings.Contains(body, tt.contains) || strings.Contains(body, tt.absent) {
				t.Errorf("body should contain %q and not %q:\n%s", tt.contains, tt.absent, body)
			}

			if tt.accept == jsonType {
				if got := decodeMembers(t, rec.Body.Bytes())["description"]; got != tt.description {
					t.Errorf("decoded description = %q, want %q", got, tt.description)
				}
			}
		})
	}
}

// type-discovery / Scenario: Security headers.
func TestDiscovery_Headers(t *testing.T) {
	t.Parallel()

	const csp = "default-src 'none'; style-src 'unsafe-inline'"

	h := documented()

	tests := []struct {
		name   string
		path   string
		accept string
		csp    string
	}{
		{name: "html", path: "/v1/api/probs/out-of-credit", accept: "text/html", csp: csp},
		{name: "json", path: "/v1/api/probs/out-of-credit", accept: jsonType},
		{name: "index html", path: "/v1/api/probs", accept: "text/html", csp: csp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := discover(t, h, http.MethodGet, tt.path, accept(tt.accept)).Header()

			if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Vary") != "Accept" ||
				header.Get("Content-Security-Policy") != tt.csp {
				t.Errorf("headers %v, want nosniff, Vary: Accept and CSP %q", header, tt.csp)
			}
		})
	}
}

const cacheControl = "public, max-age=3600"

// creditHandler returns a handler with namespace /v1/api/probs and only the
// type out-of-credit registered, with description.
func creditHandler(description string) *prblm.Handler {
	credit := prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit", description)

	return prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(credit))
}

// isQuoted reports whether tag is a quoted strong entity tag.
func isQuoted(tag string) bool {
	return len(tag) > 2 && strings.HasPrefix(tag, `"`) && strings.HasSuffix(tag, `"`)
}

// type-discovery / Scenario: Caching headers.
func TestDiscovery_Caching(t *testing.T) {
	t.Parallel()

	h := creditHandler("Balance too low.")
	get := func(path, acceptValue string) *httptest.ResponseRecorder {
		return discover(t, h, http.MethodGet, path, accept(acceptValue))
	}

	htmlTag := get("/v1/api/probs/out-of-credit", "text/html").Header().Get("ETag")

	tests := []struct {
		name   string
		path   string
		accept string
		status int
		// cacheable responses have the public Cache-Control and a quoted
		// ETag; the others neither.
		cacheable bool
	}{
		{name: "html", path: "/v1/api/probs/out-of-credit", accept: "text/html", status: http.StatusOK, cacheable: true},
		{name: "json", path: "/v1/api/probs/out-of-credit", accept: jsonType, status: http.StatusOK, cacheable: true},
		{name: "index", path: "/v1/api/probs", accept: "text/html", status: http.StatusOK, cacheable: true},
		{name: "unknown", path: "/v1/api/probs/unknown", accept: "text/html", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := get(tt.path, tt.accept)
			cc, tag := rec.Header().Get("Cache-Control"), rec.Header().Get("ETag")

			switch {
			case rec.Code != tt.status:
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			case tt.cacheable && (cc != cacheControl || !isQuoted(tag)):
				t.Errorf("Cache-Control %q, ETag %q; want %q and a quoted ETag", cc, tag, cacheControl)
			case !tt.cacheable && (cc != "no-store" || tag != ""):
				t.Errorf("Cache-Control %q, ETag %q; want the problem default no-store and no ETag", cc, tag)
			case tt.accept == jsonType && tag == htmlTag:
				t.Errorf("JSON ETag %q equals the HTML one", tag)
			}
		})
	}
}

// type-discovery / Scenario: Conditional requests.
func TestDiscovery_Conditional(t *testing.T) {
	t.Parallel()

	h := creditHandler("Balance too low.")

	const path = "/v1/api/probs/out-of-credit"

	full := discover(t, h, http.MethodGet, path, accept("text/html"))
	e := full.Header().Get("ETag")
	jsonTag := discover(t, h, http.MethodGet, path, accept(jsonType)).Header().Get("ETag")

	tests := []struct {
		name        string
		method      string
		accept      string
		ifNoneMatch string
		status      int
		etag        string
		body        string
	}{
		{name: "get matches", method: http.MethodGet, accept: "text/html", ifNoneMatch: e, status: http.StatusNotModified, etag: e},
		{name: "head matches", method: http.MethodHead, accept: "text/html", ifNoneMatch: e, status: http.StatusNotModified, etag: e},
		{name: "list", method: http.MethodGet, accept: "text/html", ifNoneMatch: `"other", ` + e, status: http.StatusNotModified, etag: e},
		{name: "weak", method: http.MethodGet, accept: "text/html", ifNoneMatch: "W/" + e, status: http.StatusNotModified, etag: e},
		{name: "any", method: http.MethodGet, accept: "text/html", ifNoneMatch: "*", status: http.StatusNotModified, etag: e},
		{name: "other", method: http.MethodGet, accept: "text/html", ifNoneMatch: `"other"`, status: http.StatusOK, etag: e, body: full.Body.String()},
		{name: "json with the html tag", method: http.MethodGet, accept: jsonType, ifNoneMatch: e, status: http.StatusOK, etag: jsonTag},
		{name: "malformed", method: http.MethodGet, accept: "text/html", ifNoneMatch: ";;;garbage", status: http.StatusOK, etag: e, body: full.Body.String()},
		{name: "empty", method: http.MethodGet, accept: "text/html", status: http.StatusOK, etag: e, body: full.Body.String()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := accept(tt.accept)
			if tt.ifNoneMatch != "" {
				header.Set("If-None-Match", tt.ifNoneMatch)
			}

			rec := discover(t, h, tt.method, path, header)

			if rec.Code != tt.status || rec.Header().Get("ETag") != tt.etag {
				t.Errorf("response %d ETag %q, want %d %q", rec.Code, rec.Header().Get("ETag"), tt.status, tt.etag)
			}

			if tt.status == http.StatusNotModified && (rec.Body.Len() != 0 || rec.Header().Get("Cache-Control") != cacheControl) {
				t.Errorf("304 with %d body bytes and Cache-Control %q, want no body and %q",
					rec.Body.Len(), rec.Header().Get("Cache-Control"), cacheControl)
			}

			if tt.body != "" && rec.Body.String() != tt.body {
				t.Errorf("body differs from the full page")
			}

			if tt.accept == jsonType && rec.Header().Get("Content-Type") != jsonType {
				t.Errorf("Content-Type = %q, want the JSON document", rec.Header().Get("Content-Type"))
			}
		})
	}
}

// type-discovery / Scenario: ETag changes with the definition.
func TestDiscovery_ETag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		first, second string
		isEqual       bool
	}{
		{first: "Balance too low.", second: "Balance too low.", isEqual: true},
		{first: "Balance too low.", second: "Balance is below the price."},
	}

	for _, tt := range tests {
		t.Run(tt.second, func(t *testing.T) {
			t.Parallel()

			etag := func(description string) string {
				return discover(t, creditHandler(description), http.MethodGet, "/v1/api/probs/out-of-credit", accept("text/html")).Header().Get("ETag")
			}

			if first, second := etag(tt.first), etag(tt.second); (first == second) != tt.isEqual || first == "" {
				t.Errorf("ETags %q and %q, want equal: %v", first, second, tt.isEqual)
			}
		})
	}
}

// type-discovery / Scenario: Absolute namespace.
func TestDiscovery_AbsoluteNamespace(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithNamespace("https://kartala.id/probs"), prblm.WithTypes(outOfCredit))

	tests := []struct {
		path   string
		status int
		typ    string
	}{
		{path: "/probs/out-of-credit", status: http.StatusOK, typ: "https://kartala.id/probs/out-of-credit"},
		{path: "/v1/api/probs/out-of-credit", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			rec := discover(t, h, http.MethodGet, tt.path, accept(jsonType))
			if typ := typeMember(t, rec); rec.Code != tt.status || typ != tt.typ {
				t.Errorf("response %d with type %q, want %d %q", rec.Code, typ, tt.status, tt.typ)
			}
		})
	}
}

// type-discovery / Scenario: net/http mount.
func TestDiscovery_Mount(t *testing.T) {
	t.Parallel()

	h := documented()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/contracts/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("contract"))
	})
	mux.Handle("GET "+h.DiscoveryPath()+"/", h.Discovery())

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		path string
		body string
	}{
		{path: "/v1/api/probs/out-of-credit"},
		{path: "/v1/api/probs/"},
		{path: "/v1/contracts/7", body: "contract"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			status, body := fetchURL(t, srv.Client(), srv.URL+tt.path, "")

			if status != http.StatusOK || (tt.body != "" && body != tt.body) {
				t.Errorf("response %d %q, want 200 %q", status, body, tt.body)
			}
		})
	}
}

// fetchURL sends GET url with Accept set to acceptValue, when not empty,
// and returns the status and body. It reports errors with t.Errorf only, so
// goroutines may call it.
func fetchURL(t *testing.T, client *http.Client, url, acceptValue string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Errorf("new request: %v", err)

		return 0, ""
	}

	if acceptValue != "" {
		req.Header.Set("Accept", acceptValue)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)

		return 0, ""
	}

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("reading %s: %v", url, err)
	}

	return res.StatusCode, string(body)
}

// type-discovery / Scenario: Concurrent documentation requests.
func TestDiscovery_Concurrent(t *testing.T) {
	t.Parallel()

	const requests = 300

	h := documented()
	srv := httptest.NewServer(h.Discovery())
	t.Cleanup(srv.Close)

	// Each request is one of the paths with one of the representations; the
	// expected body is its single-request answer.
	paths := []string{"/v1/api/probs/contracts/blacklisted-supplier", "/v1/api/probs/out-of-credit", "/v1/api/probs"}
	accepts := []string{"text/html", jsonType}
	want := make(map[[2]string]string, len(paths)*len(accepts))

	for _, path := range paths {
		for _, a := range accepts {
			want[[2]string{path, a}] = discover(t, h, http.MethodGet, path, accept(a)).Body.String()
		}
	}

	var wg sync.WaitGroup

	for i := range requests {
		path, a := paths[i%len(paths)], accepts[i%len(accepts)]

		wg.Go(func() {
			status, body := fetchURL(t, srv.Client(), srv.URL+path, a)

			if status != http.StatusOK || body != want[[2]string{path, a}] {
				t.Errorf("request %d %s %s: status %d with a different body", i, path, a, status)
			}
		})
	}

	wg.Wait()
}

// Murphy row, as for Wrap, Write and Recover: Discovery, DiscoveryPath and
// RespondDiscovery on a nil *Handler panic where they are called, naming the mistake, rather
// than a nil dereference inside a request.
func TestDiscovery_NilHandler(t *testing.T) {
	t.Parallel()

	var h *prblm.Handler

	tests := map[string]func(){
		"Discovery":         func() { h.Discovery() },
		"DiscoveryPath":     func() { h.DiscoveryPath() },
		"RespondDiscovery":  func() { h.RespondDiscovery(t.Context(), prblm.DiscoveryRequest{}) },
		"NoteShadowedRoute": func() { h.NoteShadowedRoute(t.Context(), "/", prblm.DiscoveryRequest{}) },
	}

	for method, call := range tests {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			if msg := mustPanic(t, call); !strings.Contains(msg, "Handler."+method) || !strings.Contains(msg, "problem handler") {
				t.Errorf("panic %q does not name Handler.%s and the problem handler", msg, method)
			}
		})
	}
}

// creditOnly returns a handler with namespace /v1/api/probs and the type
// out-of-credit registered, plus opts.
func creditOnly(opts ...prblm.Option) *prblm.Handler {
	return prblm.NewHandler(append([]prblm.Option{
		prblm.WithNamespace("/v1/api/probs"),
		prblm.WithTypes(outOfCredit),
	}, opts...)...)
}

// type-discovery / Scenario: Discovery response for a request.
func TestRespondDiscovery(t *testing.T) {
	t.Parallel()

	const (
		doc      = "/v1/api/probs/out-of-credit"
		docJSON  = `{"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit","description":"Balance too low."}`
		indexDoc = `{"types":[{"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit"}]}`
	)

	ask := func(t *testing.T, h *prblm.Handler, method, path, ifNoneMatch string) (prblm.Response, bool) {
		t.Helper()

		return h.RespondDiscovery(t.Context(), prblm.DiscoveryRequest{
			Method:      method,
			Path:        path,
			Accept:      jsonType,
			IfNoneMatch: ifNoneMatch,
		})
	}

	first, _ := ask(t, creditOnly(), http.MethodGet, doc, "")
	etag := first.Header.Get("ETag")

	served := []struct {
		name        string
		method      string
		path        string
		ifNoneMatch string
		status      int
		contentType string
		body        string
	}{
		{name: "document", method: http.MethodGet, path: doc, status: http.StatusOK, contentType: jsonType, body: docJSON},
		{name: "head", method: http.MethodHead, path: doc, status: http.StatusOK, contentType: jsonType},
		{name: "index", method: http.MethodGet, path: "/v1/api/probs", status: http.StatusOK, contentType: jsonType, body: indexDoc},
		{name: "index with slash", method: http.MethodGet, path: "/v1/api/probs/", status: http.StatusOK, contentType: jsonType, body: indexDoc},
		{name: "revalidated", method: http.MethodGet, path: doc, ifNoneMatch: etag, status: http.StatusNotModified},
		{name: "unknown", method: http.MethodGet, path: "/v1/api/probs/unknown", status: http.StatusNotFound, contentType: problemJSONType},
		{name: "escaped sub-type", method: http.MethodGet, path: "/v1/api/probs/out%2Dof-credit", status: http.StatusNotFound, contentType: problemJSONType},
		{name: "malformed escape", method: http.MethodGet, path: "/v1/api/probs/%zz", status: http.StatusNotFound, contentType: problemJSONType},
	}

	for _, tt := range served {
		t.Run("served "+tt.name, func(t *testing.T) {
			t.Parallel()

			res, ok := ask(t, creditOnly(), tt.method, tt.path, tt.ifNoneMatch)

			if !ok || res.Status != tt.status || res.Header.Get("Content-Type") != tt.contentType {
				t.Fatalf("result %v, %d %q, want served %d %q", ok, res.Status, res.Header.Get("Content-Type"), tt.status, tt.contentType)
			}

			assertDiscoveryBody(t, res, tt.path, tt.body)
		})
	}

	notServed := []struct {
		handler *prblm.Handler
		name    string
		method  string
		path    string
	}{
		{name: "post", handler: creditOnly(), method: http.MethodPost, path: doc},
		{name: "outside the namespace", handler: creditOnly(), method: http.MethodGet, path: "/v1/other/out-of-credit"},
		{name: "namespace prefix only", handler: creditOnly(), method: http.MethodGet, path: "/v1/api/probsx"},
		{name: "discovery off", handler: creditOnly(prblm.WithoutDiscovery()), method: http.MethodGet, path: doc},
		{name: "no types", handler: prblm.NewHandler(prblm.WithNamespace("/v1/api/probs")), method: http.MethodGet, path: doc},
	}

	for _, tt := range notServed {
		t.Run("not served "+tt.name, func(t *testing.T) {
			t.Parallel()

			if res, ok := ask(t, tt.handler, tt.method, tt.path, ""); ok {
				t.Errorf("result %d %s, want not for discovery", res.Status, res.Body)
			}
		})
	}
}

// shadowRequest is one request an application route answers, reported to
// NoteShadowedRoute.
type shadowRequest struct {
	route, method, path string
}

// noteShadowed reports every request in reqs to h.NoteShadowedRoute.
func noteShadowed(t *testing.T, h *prblm.Handler, reqs ...shadowRequest) {
	t.Helper()

	for _, req := range reqs {
		h.NoteShadowedRoute(t.Context(), req.route, prblm.DiscoveryRequest{Method: req.method, Path: req.path})
	}
}

// assertShadowNotes checks that the hook received notes notes, each
// wrapping ErrShadowedDocumentation, with no problem, and naming one of the
// routes of reqs, each route at most once.
func assertShadowNotes(t *testing.T, calls []hookCall, notes int, reqs ...shadowRequest) {
	t.Helper()

	if len(calls) != notes {
		t.Fatalf("hook calls = %+v, want %d notes", calls, notes)
	}

	named := map[string]bool{}

	for _, call := range calls {
		if !errors.Is(call.err, prblm.ErrShadowedDocumentation) || call.status != 0 {
			t.Errorf("note %v (status %d), want one wrapping ErrShadowedDocumentation without a problem", call.err, call.status)
		}

		route := ""

		for _, req := range reqs {
			if strings.Contains(call.err.Error(), "route "+strconv.Quote(req.route)) {
				route = req.route
			}
		}

		if route == "" || named[route] {
			t.Errorf("note %q names no route of %+v, or one already named", call.err, reqs)
		}

		named[route] = true
	}
}

// type-discovery / Scenario: Shadowed documentation.
func TestNoteShadowedRoute(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	get := func(route, path string) shadowRequest {
		return shadowRequest{route: route, method: http.MethodGet, path: path}
	}

	tests := []struct {
		name     string
		options  []prblm.Option
		requests []shadowRequest
		notes    int
	}{
		// Examples: noted.
		{name: "noted get", requests: []shadowRequest{get(doc, doc)}, notes: 1},
		{name: "noted get twice", requests: []shadowRequest{get(doc, doc), get(doc, doc)}, notes: 1},
		{name: "noted head", requests: []shadowRequest{{route: doc, method: http.MethodHead, path: doc}}, notes: 1},
		{name: "noted index", requests: []shadowRequest{get("/v1/api/probs", "/v1/api/probs")}, notes: 1},
		{
			name:     "noted pattern then unknown",
			requests: []shadowRequest{get("/v1/api/probs/:name", doc), get("/v1/api/probs/:name", "/v1/api/probs/unknown")},
			notes:    1,
		},
		{name: "noted two routes", requests: []shadowRequest{get(doc, doc), get("/v1/api/probs", "/v1/api/probs")}, notes: 2},
		// Examples: not noted.
		{name: "not noted unknown", requests: []shadowRequest{get("/v1/api/probs/:name", "/v1/api/probs/unknown")}},
		{name: "not noted outside", requests: []shadowRequest{get("/v1/other/:name", "/v1/other/out-of-credit")}},
		{name: "not noted post", requests: []shadowRequest{{route: doc, method: http.MethodPost, path: doc}}},
		{name: "not noted discovery off", options: []prblm.Option{prblm.WithoutDiscovery()}, requests: []shadowRequest{get(doc, doc)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			noteShadowed(t, creditOnly(append(tt.options, prblm.OnError(r.hook))...), tt.requests...)
			assertShadowNotes(t, r.recorded(), tt.notes, tt.requests...)
		})
	}
}

// Murphy rows: a panicking hook, more conflicting routes than the cap, and
// concurrent first requests to one route.
func TestNoteShadowedRoute_Faulty(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	t.Run("panicking hook", func(t *testing.T) {
		t.Parallel()

		calls := 0
		h := creditOnly(prblm.OnError(func(context.Context, error, *prblm.Problem) {
			calls++

			panic("hook")
		}))

		noteShadowed(t, h, shadowRequest{route: doc, method: http.MethodGet, path: doc})

		if calls != 1 {
			t.Errorf("hook calls = %d, want 1, its panic contained", calls)
		}
	})

	t.Run("more routes than the cap", func(t *testing.T) {
		t.Parallel()

		var r recorder

		h := creditOnly(prblm.OnError(r.hook))
		for i := range 300 {
			noteShadowed(t, h, shadowRequest{route: doc + "/" + strconv.Itoa(i), method: http.MethodGet, path: doc})
		}

		if got := len(r.recorded()); got != 256 {
			t.Errorf("notes = %d, want 256, the cap", got)
		}
	})

	t.Run("concurrent first requests", func(t *testing.T) {
		t.Parallel()

		var (
			r  recorder
			wg sync.WaitGroup
		)

		h := creditOnly(prblm.OnError(r.hook))

		for range 100 {
			wg.Go(func() { noteShadowed(t, h, shadowRequest{route: doc, method: http.MethodGet, path: doc}) })
		}

		wg.Wait()

		if got := len(r.recorded()); got != 1 {
			t.Errorf("notes = %d, want 1", got)
		}
	})
}

// assertDiscoveryBody checks the body of a served discovery response: a
// problem names path as its instance, and any other body is body, which is
// empty for a HEAD or a 304.
func assertDiscoveryBody(t *testing.T, res prblm.Response, path, body string) {
	t.Helper()

	if res.Header.Get("Content-Type") == problemJSONType {
		if m := decodeMembers(t, res.Body); m["instance"] != path {
			t.Errorf("problem %s, want the instance %q", res.Body, path)
		}

		return
	}

	if string(res.Body) != body {
		t.Errorf("body = %q, want %q", res.Body, body)
	}
}

// type-discovery / Scenario: Pages rendered on first use.
func TestDiscovery_FirstUse(t *testing.T) {
	t.Parallel()

	for _, requests := range []int{1, 100} {
		t.Run(strconv.Itoa(requests), func(t *testing.T) {
			t.Parallel()

			h := creditOnly()

			var (
				wg        sync.WaitGroup
				responses = make([]prblm.Response, requests)
			)

			for i := range requests {
				wg.Go(func() {
					responses[i], _ = h.RespondDiscovery(t.Context(), prblm.DiscoveryRequest{
						Method: http.MethodGet,
						Path:   "/v1/api/probs/out-of-credit",
						Accept: jsonType,
					})
				})
			}

			wg.Wait()

			first := responses[0]
			for _, res := range responses {
				if res.Status != http.StatusOK || string(res.Body) != string(first.Body) ||
					res.Header.Get("ETag") == "" || res.Header.Get("ETag") != first.Header.Get("ETag") {
					t.Fatalf("response %d %s %q, want 200 with the same body and ETag as %s %q",
						res.Status, res.Body, res.Header.Get("ETag"), first.Body, first.Header.Get("ETag"))
				}
			}
		})
	}
}
