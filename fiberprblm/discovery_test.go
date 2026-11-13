package fiberprblm_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

// discoveryApp returns a fiber app whose error handler is the adapter with
// namespace /v1/api/probs, the type out-of-credit registered and the extra
// options.
func discoveryApp(extra ...prblm.Option) *fiber.App {
	opts := append([]prblm.Option{prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(OutOfCredit)}, extra...)

	return fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(opts...)})
}

// fiber-adapter / Scenario: Discovery through fiber.
func TestErrorHandler_Discovery(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	etagReq := get(t, http.MethodGet, doc)
	etagReq.Header.Set("Accept", "application/json")
	etag := test(t, discoveryApp(), etagReq).header.Get("ETag")

	tests := []struct {
		name   string
		app    func() *fiber.App
		method string
		path   string
		// rawURI, when set, is sent as the request URI as is, for URIs
		// net/http's request builders reject.
		rawURI string
		header http.Header
		check  func(t *testing.T, r response)
	}{
		{name: "served", app: noRoutes, path: doc, check: isDocumentation},
		{name: "head", app: noRoutes, method: http.MethodHead, path: doc, check: isHeadersOnly},
		{name: "index", app: noRoutes, path: "/v1/api/probs", check: isIndex},
		{name: "unknown", app: noRoutes, path: "/v1/api/probs/unknown", check: isNotFoundProblem},
		{
			name: "revalidated", app: noRoutes, path: doc, header: http.Header{"If-None-Match": {etag}},
			check: isRevalidated(etag),
		},
		{
			name: "route wins",
			app: func() *fiber.App {
				app := discoveryApp()
				app.Get(doc, func(c fiber.Ctx) error { return c.SendString("custom") })

				return app
			},
			path:  doc,
			check: isCustom,
		},
		{
			name: "handler returns ErrNotFound",
			app: func() *fiber.App {
				app := discoveryApp()
				app.Get(doc, func(fiber.Ctx) error { return fiber.ErrNotFound })

				return app
			},
			path:  doc,
			check: isNotFoundProblem,
		},
		{name: "post", app: noRoutes, method: http.MethodPost, path: doc, check: isNotFoundProblem},
		{name: "outside the namespace", app: noRoutes, path: "/v1/other/out-of-credit", check: isNotFoundProblem},
		// Murphy row: a URI fiber accepts but net/http cannot parse is
		// answered as not found.
		{name: "malformed escape", app: noRoutes, path: "/v1/api/probs/x", rawURI: "/v1/api/probs/%zz", check: isNotFoundProblem},
		{
			name:  "discovery off",
			app:   func() *fiber.App { return discoveryApp(prblm.WithoutDiscovery()) },
			path:  doc,
			check: isNotFoundProblem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			req := get(t, method, tt.path)
			req.Header.Set("Accept", "application/json")
			maps.Copy(req.Header, tt.header)

			if tt.rawURI != "" {
				req.URL.Opaque = tt.rawURI
			}

			tt.check(t, test(t, tt.app(), req))
		})
	}
}

// noRoutes returns the discovery app without routes.
func noRoutes() *fiber.App { return discoveryApp() }

// isDocumentation checks that r is the JSON documentation of out-of-credit.
func isDocumentation(t *testing.T, r response) {
	t.Helper()

	var doc struct {
		Type string `json:"type"`
	}

	if r.status != http.StatusOK || r.header.Get("Content-Type") != "application/json" ||
		json.Unmarshal([]byte(r.body), &doc) != nil || doc.Type != "/v1/api/probs/out-of-credit" {
		t.Errorf("response %d %q %s, want the JSON documentation of out-of-credit", r.status, r.header.Get("Content-Type"), r.body)
	}
}

// isNotFoundProblem checks that r is a 404 problem, not documentation.
func isNotFoundProblem(t *testing.T, r response) {
	t.Helper()

	if r.status != http.StatusNotFound || r.header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("response %d %q, want a 404 problem", r.status, r.header.Get("Content-Type"))
	}
}

// isHeadersOnly checks that r is the JSON documentation's headers without a
// body, the answer to HEAD.
func isHeadersOnly(t *testing.T, r response) {
	t.Helper()

	if r.status != http.StatusOK || r.header.Get("Content-Type") != "application/json" || r.body != "" {
		t.Errorf("response %d %q %q, want 200 JSON headers only", r.status, r.header.Get("Content-Type"), r.body)
	}
}

// isIndex checks that r is the JSON index with out-of-credit.
func isIndex(t *testing.T, r response) {
	t.Helper()

	if r.status != http.StatusOK || !strings.Contains(r.body, `"subtype":"out-of-credit"`) {
		t.Errorf("response %d %s, want the index with out-of-credit", r.status, r.body)
	}
}

// isRevalidated returns a check that r is a bodiless 304 with etag and the
// public Cache-Control.
func isRevalidated(etag string) func(*testing.T, response) {
	return func(t *testing.T, r response) {
		t.Helper()

		if r.status != http.StatusNotModified || r.header.Get("ETag") != etag ||
			r.header.Get("Cache-Control") != "public, max-age=3600" || r.body != "" {
			t.Errorf("response %d ETag %q Cache-Control %q body %q, want 304 %q",
				r.status, r.header.Get("ETag"), r.header.Get("Cache-Control"), r.body, etag)
		}
	}
}

// isCustom checks that r is the custom route's 200.
func isCustom(t *testing.T, r response) {
	t.Helper()

	if r.status != http.StatusOK || r.body != "custom" {
		t.Errorf("response %d %q, want 200 custom", r.status, r.body)
	}
}
