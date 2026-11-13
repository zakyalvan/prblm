package ginprblm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
)

var (
	ErrVesselNotFound = errors.New("vessel not found")
	ErrLocked         = errors.New("locked")
)

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
