package paritytest_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
	"github.com/kartaladev/prblm/ginprblm"
)

var (
	errVesselNotFound = errors.New("vessel not found")
	errBlacklisted    = errors.New("supplier is blacklisted")
)

// blacklistedSupplier is the one registered type definition.
var blacklistedSupplier = prblm.DefineType(
	"contracts/blacklisted-supplier",
	http.StatusConflict,
	"Supplier is blacklisted",
	"The supplier is on the procurement blacklist.",
)

// returned holds the errors every server raises the same way. A panic and a
// status hint are raised in each framework's own way, which is what parity
// is about, so each server handles those cases itself.
var returned = map[string]error{
	caseMapped:      errVesselNotFound,
	caseUnmapped:    errors.New("pq: password=hunter2"),
	caseBlacklisted: errBlacklisted,
}

// Each case is selected with the query parameter "case", which the
// problem's instance leaves out.
const (
	caseMapped      = "mapped"
	caseUnmapped    = "unmapped"
	casePanic       = "panic"
	caseHint        = "hint"
	caseBlacklisted = "blacklisted"
)

// The paths the servers answer the cases at.
const (
	pathVessel   = "/v1/vessels/7"
	pathContract = "/v1/contracts/7"
)

// newOptions is the one configuration all three servers are built from.
func newOptions() []prblm.Option {
	return []prblm.Option{
		prblm.WithNamespace("/v1/api/probs"),
		prblm.WithTypes(blacklistedSupplier),
		prblm.WithMappers(
			prblm.Status(errVesselNotFound, http.StatusNotFound),
			prblm.Is(errBlacklisted, func(error) *prblm.Problem { return blacklistedSupplier.New() }),
		),
		prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) { _ = p.Set("trace_id", "t1") }),
	}
}

// netHTTPServer serves the cases through the net/http adapter.
func netHTTPServer(t *testing.T, opts []prblm.Option) string {
	t.Helper()

	h := prblm.NewHandler(opts...)

	handler := h.Recover(h.Wrap(func(_ http.ResponseWriter, r *http.Request) error {
		switch name := r.URL.Query().Get("case"); name {
		case casePanic:
			panic("boom")
		case caseHint:
			return prblm.WithStatus(errors.New("x"), http.StatusForbidden)
		default:
			return returned[name]
		}
	}))

	mux := http.NewServeMux()
	mux.Handle("GET /v1/vessels/{id}", handler)
	mux.Handle("GET /v1/contracts/{id}", handler)

	return serve(t, mux)
}

// ginServer serves the cases through the gin adapter.
func ginServer(t *testing.T, opts []prblm.Option) string {
	t.Helper()

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(ginprblm.Recovery(opts...), ginprblm.Middleware(opts...))

	handler := func(c *gin.Context) {
		switch name := c.Query("case"); name {
		case casePanic:
			panic("boom")
		case caseHint:
			_ = c.AbortWithError(http.StatusForbidden, errors.New("x"))
		default:
			_ = c.Error(returned[name])
		}
	}
	engine.GET("/v1/vessels/:id", handler)
	engine.GET("/v1/contracts/:id", handler)

	return serve(t, engine)
}

// serve serves handler over a real server for the test and returns its URL.
func serve(t *testing.T, handler http.Handler) string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL
}

// fiberServer serves the cases through the fiber adapter.
func fiberServer(t *testing.T, opts []prblm.Option) string {
	t.Helper()

	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(opts...)})
	app.Use(fiberprblm.Recovery())

	handler := func(c fiber.Ctx) error {
		switch name := c.Query("case"); name {
		case casePanic:
			panic("boom")
		case caseHint:
			return fiber.NewError(http.StatusForbidden, "x")
		default:
			return returned[name]
		}
	}
	app.Get("/v1/vessels/:id", handler)
	app.Get("/v1/contracts/:id", handler)

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()

	t.Cleanup(func() { _ = app.Shutdown() })

	return "http://" + ln.Addr().String()
}

// observed is the part of a response the parity scenarios compare.
type observed struct {
	contentType string
	vary        string
	body        string
	status      int
}

// fetch sends the case to base at path with accept and records what parity
// covers.
func fetch(t *testing.T, client *http.Client, base, path, caseName, accept string) observed {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path+"?case="+caseName, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	return observed{
		status:      res.StatusCode,
		contentType: res.Header.Get("Content-Type"),
		vary:        strings.Join(res.Header.Values("Vary"), ", "),
		body:        string(body),
	}
}

// gin-adapter / Scenario: Same output as net/http; fiber-adapter / Scenario:
// Same output as net/http.
func TestParity(t *testing.T) {
	t.Parallel()

	opts := newOptions()

	// Idle connections are closed before the servers stop; fiber's
	// shutdown would otherwise wait for them.
	client := &http.Client{Transport: &http.Transport{}}

	servers := map[string]string{
		"net/http": netHTTPServer(t, opts),
		"gin":      ginServer(t, opts),
		"fiber":    fiberServer(t, opts),
	}

	t.Cleanup(client.CloseIdleConnections)

	tests := []struct {
		name        string
		path        string
		caseName    string
		accept      string
		contains    string
		status      int
		contentType string
	}{
		{name: "mapped error", path: pathVessel, caseName: caseMapped, status: http.StatusNotFound, contentType: "application/problem+json"},
		{name: "unmapped error", path: pathVessel, caseName: caseUnmapped, status: http.StatusInternalServerError, contentType: "application/problem+json"},
		{name: "panic under recovery", path: pathVessel, caseName: casePanic, status: http.StatusInternalServerError, contentType: "application/problem+json"},
		{name: "mapped error as xml", path: pathVessel, caseName: caseMapped, accept: "application/problem+xml", status: http.StatusNotFound, contentType: "application/problem+xml"},
		{name: "status hint", path: pathVessel, caseName: caseHint, status: http.StatusForbidden, contentType: "application/problem+json"},
		{
			name:        "resolved type",
			path:        pathContract,
			caseName:    caseBlacklisted,
			contains:    `"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted"`,
			status:      http.StatusConflict,
			contentType: "application/problem+json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := fetch(t, client, servers["net/http"], tt.path, tt.caseName, tt.accept)

			if want.status != tt.status || want.contentType != tt.contentType || want.vary != "Accept" || !strings.Contains(want.body, tt.contains) {
				t.Fatalf("net/http sent %d %q Vary %q %s, want %d %q Vary Accept containing %s",
					want.status, want.contentType, want.vary, want.body, tt.status, tt.contentType, tt.contains)
			}

			for _, name := range []string{"gin", "fiber"} {
				if got := fetch(t, client, servers[name], tt.path, tt.caseName, tt.accept); got != want {
					t.Errorf("%s differs from net/http:\n got  %+v\n want %+v", name, got, want)
				}
			}
		})
	}
}
