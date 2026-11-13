package ginprblm_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

var (
	ErrVesselNotFound = errors.New("vessel not found")
	ErrLocked         = errors.New("locked")
)

// OutOfCredit is the type definition of the adapter scenarios.
var OutOfCredit = prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit", "")

// hookRecorder records the errors an OnError hook receives, leaving out the
// notes about consumer mistakes, which the core reports separately.
type hookRecorder struct {
	errs []error
	mu   sync.Mutex
}

func (r *hookRecorder) option() prblm.Option {
	return prblm.OnError(func(_ context.Context, err error, _ *prblm.Problem) {
		r.mu.Lock()
		defer r.mu.Unlock()

		r.errs = append(r.errs, err)
	})
}

func (r *hookRecorder) received() []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.errs)
}

// last returns the last error the hook received, or nil.
func (r *hookRecorder) last() error {
	errs := r.received()
	if len(errs) == 0 {
		return nil
	}

	return errs[len(errs)-1]
}

// serve sends req to engine and returns the recorded response.
func serve(t *testing.T, engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	return rec
}

// get builds a GET request for target.
func get(t *testing.T, target string) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
}

// parse decodes the problem in res, failing the test when it is not one.
// Parse closes the body.
func parse(t *testing.T, res *http.Response) *prblm.Problem {
	t.Helper()

	p, err := prblm.Parse(res)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	return p
}

// parseRecorded decodes the problem recorded in rec; Parse closes the body.
func parseRecorded(t *testing.T, rec *httptest.ResponseRecorder) *prblm.Problem {
	t.Helper()

	return parse(t, rec.Result())
}

// do sends req to srv and returns the response as a recorder, with the
// body read and closed.
func do(t *testing.T, srv *httptest.Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	req.RequestURI = ""

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	rec := httptest.NewRecorder()
	maps.Copy(rec.Header(), res.Header)
	rec.Code = res.StatusCode

	if _, err := io.Copy(rec.Body, res.Body); err != nil {
		t.Fatalf("reading body: %v", err)
	}

	return rec
}

// assertHook checks that the hook was called once with an error matching
// want, or not at all for a nil want.
func assertHook(t *testing.T, hook *hookRecorder, want error) {
	t.Helper()

	got := hook.received()

	switch {
	case want == nil && len(got) != 0:
		t.Errorf("hook received %v, want no call", got)
	case want != nil && (len(got) != 1 || !errors.Is(got[0], want)):
		t.Errorf("hook received %v, want one call with %v", got, want)
	}
}

// newEngine returns an engine that uses Middleware, with a problem handler
// built from opts, and serves handler at GET /x.
func newEngine(handler gin.HandlerFunc, opts ...prblm.Option) *gin.Engine {
	engine := gin.New()
	engine.Use(ginprblm.Middleware(opts...))
	engine.GET("/x", handler)

	return engine
}

// fetch serves engine over a real server for the test and sends GET path.
func fetch(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	return do(t, srv, get(t, srv.URL+path))
}

// assertPanicHook checks that the hook was called once with an error
// unwrapping to a panic error holding value.
func assertPanicHook(t *testing.T, hook *hookRecorder, value any) {
	t.Helper()

	got := hook.received()
	if len(got) != 1 {
		t.Fatalf("hook received %v, want one call", got)
	}

	if pe, ok := errors.AsType[*prblm.PanicError](got[0]); !ok || pe.Value != value {
		t.Errorf("hook received %v, want a panic error with value %v", got[0], value)
	}
}

// newRecoveryEngine returns an engine that uses only Recovery, built from
// opts, and serves handler at GET /x.
func newRecoveryEngine(handler gin.HandlerFunc, opts ...prblm.Option) *gin.Engine {
	engine := gin.New()
	engine.Use(ginprblm.Recovery(opts...))
	engine.GET("/x", handler)

	return engine
}
