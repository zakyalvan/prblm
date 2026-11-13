package ginprblm_test

import (
	"cmp"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

// discoveryOptions are the options of the gin discovery scenarios:
// namespace /v1/api/probs and the type out-of-credit registered.
func discoveryOptions(extra ...prblm.Option) []prblm.Option {
	return append([]prblm.Option{prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(OutOfCredit)}, extra...)
}

// discoveryEngine returns an engine using Middleware via engine.Use with
// the discovery options and extra, after routes, when not nil, registered
// its own handlers.
func discoveryEngine(routes func(*gin.Engine), extra ...prblm.Option) *gin.Engine {
	engine := gin.New()
	engine.Use(ginprblm.Middleware(discoveryOptions(extra...)...))

	if routes != nil {
		routes(engine)
	}

	return engine
}

// custom answers with status and the body "custom".
func custom(status int) gin.HandlerFunc {
	return func(c *gin.Context) { c.String(status, "custom") }
}

// gin-adapter / Scenario: Discovery through gin.
func TestMiddleware_Discovery(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	etag := serve(t, discoveryEngine(nil), request(t, http.MethodGet, doc)).Header().Get("ETag")
	noRoute := func(e *gin.Engine) { e.NoRoute(custom(http.StatusNotFound)) }

	tests := []struct {
		name   string
		engine *gin.Engine
		method string
		path   string
		header http.Header
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{name: "served", engine: discoveryEngine(nil), path: doc, check: isDocumentation},
		{name: "head", engine: discoveryEngine(nil), method: http.MethodHead, path: doc, check: isHeadersOnly},
		{name: "index", engine: discoveryEngine(nil), path: "/v1/api/probs", check: isIndex},
		{name: "unknown", engine: discoveryEngine(nil), path: "/v1/api/probs/unknown", check: isNotFoundProblem},
		{
			name: "revalidated", engine: discoveryEngine(nil), path: doc,
			header: http.Header{"If-None-Match": {etag}}, check: isRevalidated(etag),
		},
		{name: "served before NoRoute", engine: discoveryEngine(noRoute), path: doc, check: isDocumentation},
		{
			name:   "route wins",
			engine: discoveryEngine(func(e *gin.Engine) { e.GET(doc, custom(http.StatusOK)) }),
			path:   doc,
			check:  hasBody(http.StatusOK, "custom"),
		},
		{name: "post", engine: discoveryEngine(nil), method: http.MethodPost, path: doc, check: isNotFoundProblem},
		{name: "outside the namespace", engine: discoveryEngine(nil), path: "/v1/other/out-of-credit", check: isNotFoundProblem},
		{name: "discovery off", engine: discoveryEngine(nil, prblm.WithoutDiscovery()), path: doc, check: isNotFoundProblem},
		{name: "middleware only on a group", engine: groupOnlyEngine(), path: doc, check: isNotDocumentation},
		{
			name:   "NoRoute keeps other paths",
			engine: discoveryEngine(noRoute),
			path:   "/v1/other/out-of-credit",
			check:  hasBody(http.StatusNotFound, "custom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := cmp.Or(tt.method, http.MethodGet)

			req := request(t, method, tt.path)
			maps.Copy(req.Header, tt.header)

			tt.check(t, serve(t, tt.engine, req))
		})
	}
}

// groupOnlyEngine returns an engine with Middleware only on the group
// /v1/vessels, which never sees unmatched paths.
func groupOnlyEngine() *gin.Engine {
	engine := gin.New()
	engine.Group("/v1/vessels", ginprblm.Middleware(discoveryOptions()...)).GET("/:id", custom(http.StatusOK))

	return engine
}

// request builds a request for target with Accept: application/json.
func request(t *testing.T, method, target string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
	req.Header.Set("Accept", "application/json")

	return req
}

// isHeadersOnly checks that rec is the JSON documentation's headers without
// a body, the answer to HEAD.
func isHeadersOnly(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" || rec.Body.Len() != 0 {
		t.Errorf("response %d %q with %d body bytes, want 200 JSON headers only", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

// isIndex checks that rec is the JSON index with out-of-credit.
func isIndex(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"subtype":"out-of-credit"`) {
		t.Errorf("response %d %s, want the index with out-of-credit", rec.Code, rec.Body.String())
	}
}

// isRevalidated returns a check that rec is a bodiless 304 with etag and
// the public Cache-Control.
func isRevalidated(etag string) func(*testing.T, *httptest.ResponseRecorder) {
	return func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()

		if rec.Code != http.StatusNotModified || rec.Header().Get("ETag") != etag ||
			rec.Header().Get("Cache-Control") != "public, max-age=3600" || rec.Body.Len() != 0 {
			t.Errorf("response %d ETag %q Cache-Control %q with %d body bytes, want 304 %q",
				rec.Code, rec.Header().Get("ETag"), rec.Header().Get("Cache-Control"), rec.Body.Len(), etag)
		}
	}
}

// isDocumentation checks that rec is the JSON documentation of out-of-credit.
func isDocumentation(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	var doc struct {
		Type string `json:"type"`
	}

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" ||
		json.Unmarshal(rec.Body.Bytes(), &doc) != nil || doc.Type != "/v1/api/probs/out-of-credit" {
		t.Errorf("response %d %q %s, want the JSON documentation of out-of-credit", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

// isNotFoundProblem checks that rec is a 404 problem.
func isNotFoundProblem(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("response %d %q, want a 404 problem", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// isNotDocumentation checks that rec is a 404 that is not documentation.
func isNotDocumentation(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") == "application/json" {
		t.Errorf("response %d %s, want a 404 that is not the documentation", rec.Code, rec.Body.String())
	}
}

// hasBody returns a check that rec has status and body.
func hasBody(status int, body string) func(*testing.T, *httptest.ResponseRecorder) {
	return func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()

		if rec.Code != status || rec.Body.String() != body {
			t.Errorf("response %d %q, want %d %q", rec.Code, rec.Body.String(), status, body)
		}
	}
}
