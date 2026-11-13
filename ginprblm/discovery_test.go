package ginprblm_test

import (
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

// gin-adapter / Scenario: Discovery through gin.
func TestMiddleware_Discovery(t *testing.T) {
	t.Parallel()

	const doc = "/v1/api/probs/out-of-credit"

	custom := func(status int) gin.HandlerFunc {
		return func(c *gin.Context) { c.String(status, "custom") }
	}
	used := func(opts ...prblm.Option) func(*gin.Engine) {
		return func(e *gin.Engine) { e.Use(ginprblm.Middleware(discoveryOptions(opts...)...)) }
	}
	withNoRoute := func(e *gin.Engine) {
		used()(e)
		e.NoRoute(custom(http.StatusNotFound))
	}

	// etag is the ETag of the JSON documentation, for the revalidation row.
	etagEngine := gin.New()
	used()(etagEngine)
	etag := serve(t, etagEngine, request(t, http.MethodGet, doc, "")).Header().Get("ETag")

	tests := []struct {
		name   string
		setup  func(*gin.Engine)
		method string
		path   string
		header http.Header
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{name: "served", setup: used(), path: doc, check: isDocumentation},
		{name: "head", setup: used(), method: http.MethodHead, path: doc, check: func(t *testing.T, rec *httptest.ResponseRecorder) {
			t.Helper()

			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" || rec.Body.Len() != 0 {
				t.Errorf("response %d %q with %d body bytes, want 200 JSON headers only", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
			}
		}},
		{name: "index", setup: used(), path: "/v1/api/probs", check: func(t *testing.T, rec *httptest.ResponseRecorder) {
			t.Helper()

			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"subtype":"out-of-credit"`) {
				t.Errorf("response %d %s, want the index with out-of-credit", rec.Code, rec.Body.String())
			}
		}},
		{name: "unknown", setup: used(), path: "/v1/api/probs/unknown", check: isNotFoundProblem},
		{
			name: "revalidated", setup: used(), path: doc, header: http.Header{"If-None-Match": {etag}},
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()

				if rec.Code != http.StatusNotModified || rec.Header().Get("ETag") != etag ||
					rec.Header().Get("Cache-Control") != "public, max-age=3600" || rec.Body.Len() != 0 {
					t.Errorf("response %d ETag %q Cache-Control %q with %d body bytes, want 304 %q",
						rec.Code, rec.Header().Get("ETag"), rec.Header().Get("Cache-Control"), rec.Body.Len(), etag)
				}
			},
		},
		{name: "served before NoRoute", setup: withNoRoute, path: doc, check: isDocumentation},
		{
			name: "route wins",
			setup: func(e *gin.Engine) {
				used()(e)
				e.GET(doc, custom(http.StatusOK))
			},
			path:  doc,
			check: hasBody(http.StatusOK, "custom"),
		},
		{name: "post", setup: used(), method: http.MethodPost, path: doc, check: isNotDocumentation},
		{name: "outside the namespace", setup: used(), path: "/v1/other/out-of-credit", check: isNotDocumentation},
		{name: "discovery off", setup: used(prblm.WithoutDiscovery()), path: doc, check: isNotDocumentation},
		{
			name: "middleware only on a group",
			setup: func(e *gin.Engine) {
				e.Group("/v1/vessels", ginprblm.Middleware(discoveryOptions()...)).GET("/:id", custom(http.StatusOK))
			},
			path:  doc,
			check: isNotDocumentation,
		},
		{name: "NoRoute keeps other paths", setup: withNoRoute, path: "/v1/other/out-of-credit", check: hasBody(http.StatusNotFound, "custom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := gin.New()
			tt.setup(engine)

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			req := request(t, method, tt.path, "")
			maps.Copy(req.Header, tt.header)

			tt.check(t, serve(t, engine, req))
		})
	}
}

// request builds a request for target with Accept: application/json and,
// when not empty, body.
func request(t *testing.T, method, target, body string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")

	return req
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
