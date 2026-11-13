package prblm_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
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

			if tt.status == http.StatusNotFound && rec.Header().Get("Content-Type") != "application/problem+json" {
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
		{path: "/v1/api/probs/contracts/unknown", status: http.StatusNotFound, contentType: "application/problem+json"},
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
