package fiberprblm_test

import (
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

// discoveryOptions are the options of the fiber discovery scenarios:
// namespace /v1/api/probs and the type out-of-credit registered, then extra.
func discoveryOptions(extra ...prblm.Option) []prblm.Option {
	return append([]prblm.Option{prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(OutOfCredit)}, extra...)
}

// discoveryApp returns a fiber app whose error handler is the adapter with
// the discovery options and extra.
func discoveryApp(extra ...prblm.Option) *fiber.App {
	return newApp(discoveryOptions(extra...)...)
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
		{name: "route wins", app: withCustomRoute, path: doc, check: isCustom},
		{
			// fiber registers HEAD with GET, so the route answers it.
			name:   "head beside a get route",
			app:    withCustomRoute,
			method: http.MethodHead,
			path:   doc,
			check:  isRouteHead,
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
		{
			name: "route for another method",
			app: func() *fiber.App {
				app := discoveryApp()
				app.Post(doc, func(c fiber.Ctx) error { return c.SendString("custom") })

				return app
			},
			path:  doc,
			check: isMethodNotAllowedProblem,
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

// fiber-adapter / Scenario: Shadowed documentation through fiber.
func TestDiscovery_Shadowed(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	tests := []struct {
		name         string
		isMiddleware bool
		extra        []prblm.Option
		route        string
		method       string
		path         string
		requests     int
		check        func(t *testing.T, r response)
		noted        string
	}{
		{name: "route at a documentation path", isMiddleware: true, route: doc, path: doc, requests: 2, check: isCustom, noted: doc},
		{name: "head", isMiddleware: true, route: doc, method: http.MethodHead, path: doc, requests: 1, check: isRouteHead, noted: doc},
		{name: "route outside the namespace", isMiddleware: true, route: "/v1/vessels/:id", path: "/v1/vessels/7", requests: 1, check: isCustom},
		{name: "no discovery middleware", route: doc, path: doc, requests: 1, check: isCustom},
		{
			name: "discovery off", isMiddleware: true, extra: []prblm.Option{prblm.WithoutDiscovery()},
			route: doc, path: doc, requests: 1, check: isCustom,
		},
		{name: "no routes", isMiddleware: true, path: doc, requests: 1, check: isDocumentation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			opts := discoveryOptions(append(tt.extra, hook.option())...)

			app := newApp(opts...)
			if tt.isMiddleware {
				app.Use(fiberprblm.Discovery(opts...))
			}

			if tt.route != "" {
				app.Get(tt.route, func(c fiber.Ctx) error { return c.SendString("custom") })
			}

			for range tt.requests {
				req := get(t, cmp.Or(tt.method, http.MethodGet), tt.path)
				req.Header.Set("Accept", "application/json")
				tt.check(t, test(t, app, req))
			}

			assertShadowNote(t, &hook, tt.noted)
		})
	}
}

// Murphy rows for the discovery middleware: the error of the handlers
// after it reaches the error handler unchanged, with the route noted, and a
// panic below it is left to Recovery; the panicking request is not noted,
// since the panic unwinds past the report.
func TestDiscovery_Faulty(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	tests := []struct {
		name    string
		handler fiber.Handler
		status  int
		want    error
	}{
		{
			name:    "returned error",
			handler: func(fiber.Ctx) error { return ErrLocked },
			status:  http.StatusLocked,
			want:    ErrLocked,
		},
		{
			name:    "panic",
			handler: func(fiber.Ctx) error { panic("boom") },
			status:  http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			opts := discoveryOptions(prblm.WithMappers(prblm.Status(ErrLocked, http.StatusLocked)), hook.option())

			app := newApp(opts...)
			app.Use(fiberprblm.Recovery(), fiberprblm.Discovery(opts...))
			app.Get(doc, tt.handler)

			if r := test(t, app, get(t, http.MethodGet, doc)); r.status != tt.status ||
				r.header.Get("Content-Type") != "application/problem+json" {
				t.Errorf("response %d %q, want a %d problem", r.status, r.header.Get("Content-Type"), tt.status)
			}

			assertFaultyHook(t, hook.received(), tt.want)
		})
	}
}

// assertFaultyHook checks what the hook received for TestDiscovery_Faulty:
// the shadowed-documentation note, made inside the chain, then the returned
// error want, unchanged, which fiber hands the error handler once the chain
// returns; or, for a nil want, only the panic.
func assertFaultyHook(t *testing.T, got []error, want error) {
	t.Helper()

	if want == nil {
		if len(got) != 1 {
			t.Fatalf("hook received %v, want only the panic", got)
		}

		if _, ok := errors.AsType[*prblm.PanicError](got[0]); !ok {
			t.Errorf("hook received %v, want only the panic", got)
		}

		return
	}

	if len(got) != 2 || !errors.Is(got[0], prblm.ErrShadowedDocumentation) || !errors.Is(got[1], want) {
		t.Errorf("hook received %v, want the shadowed-documentation note, then %v unchanged", got, want)
	}
}

// assertShadowNote checks that the hook received one note wrapping
// ErrShadowedDocumentation that names the route noted, or nothing for an
// empty noted.
func assertShadowNote(t *testing.T, hook *hookRecorder, noted string) {
	t.Helper()

	if noted == "" {
		assertHook(t, hook, nil)

		return
	}

	assertHook(t, hook, prblm.ErrShadowedDocumentation)

	if errs := hook.received(); len(errs) == 1 && !strings.Contains(errs[0].Error(), "route "+strconv.Quote(noted)) {
		t.Errorf("note %q does not name route %q", errs[0], noted)
	}
}

// withCustomRoute returns the discovery app with a route GET
// /v1/api/probs/out-of-credit answering 200 custom.
func withCustomRoute() *fiber.App {
	app := discoveryApp()
	app.Get("/v1/api/probs/out-of-credit", func(c fiber.Ctx) error { return c.SendString("custom") })

	return app
}

// isRouteHead checks that r is the custom route's answer to HEAD: 200, its
// own headers, no body, and not the documentation.
func isRouteHead(t *testing.T, r response) {
	t.Helper()

	if r.status != http.StatusOK || r.header.Get("Content-Type") == "application/json" || r.body != "" {
		t.Errorf("response %d %q %q, want the route's 200 headers only", r.status, r.header.Get("Content-Type"), r.body)
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

// isMethodNotAllowedProblem checks that r is a 405 problem, not the
// documentation.
func isMethodNotAllowedProblem(t *testing.T, r response) {
	t.Helper()

	if r.status != http.StatusMethodNotAllowed || r.header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("response %d %q, want a 405 problem", r.status, r.header.Get("Content-Type"))
	}
}
