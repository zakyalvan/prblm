package fiberprblm_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

var (
	ErrVesselNotFound = errors.New("vessel not found")
	ErrLocked         = errors.New("locked")
)

// OutOfCredit is the type definition of the adapter scenarios.
var OutOfCredit = prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit", "")

// hookRecorder records the errors an OnError hook receives.
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

// newApp returns a fiber app whose error handler is the adapter with a
// problem handler built from opts.
func newApp(opts ...prblm.Option) *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(opts...)})
}

// response is a fiber response read in full.
type response struct {
	header http.Header
	body   string
	status int
}

// test sends req through app.Test and reads the response.
func test(t *testing.T, app *fiber.App, req *http.Request) response {
	t.Helper()

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	return response{status: res.StatusCode, header: res.Header, body: string(body)}
}

// get builds a request for target.
func get(t *testing.T, method, target string) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
}

// problemOf decodes the problem in r, failing the test when it is not one.
func problemOf(t *testing.T, r response) *prblm.Problem {
	t.Helper()

	res := &http.Response{
		StatusCode: r.status,
		Header:     r.header,
		Body:       io.NopCloser(strings.NewReader(r.body)),
	}

	p, err := prblm.Parse(res)
	if err != nil {
		t.Fatalf("Parse: %v (body %q)", err, r.body)
	}

	return p
}

// listen serves app on a real loopback listener for the test and returns a
// client for it and its base URL. The client's idle connections are closed
// before the app shuts down, which otherwise waits for them.
func listen(t *testing.T, app *fiber.App) (*http.Client, string) {
	t.Helper()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()

	client := &http.Client{Transport: &http.Transport{}}

	// Cleanups run last first: idle connections close, then the app stops.
	t.Cleanup(func() { _ = app.Shutdown() })
	t.Cleanup(client.CloseIdleConnections)

	return client, "http://" + ln.Addr().String()
}

// serveX serves handler at GET /x in an app using the adapter, with a
// problem handler built from opts, and sends one request to it.
func serveX(t *testing.T, handler fiber.Handler, opts ...prblm.Option) response {
	t.Helper()

	app := newApp(opts...)
	app.Get("/x", handler)

	return test(t, app, get(t, http.MethodGet, "/x"))
}

// fetchProblem sends GET url with client and decodes the problem. It
// returns errors rather than failing the test, so goroutines can use it.
func fetchProblem(ctx context.Context, client *http.Client, url string) (int, *prblm.Problem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, nil, err
	}

	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}

	defer func() { _ = res.Body.Close() }()

	p, err := prblm.Parse(res)

	return res.StatusCode, p, err
}
