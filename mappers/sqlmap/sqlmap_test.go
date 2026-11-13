package sqlmap_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/sqlmap"
)

const (
	detailNotFound    = "The requested resource was not found."
	detailUnavailable = "The service is temporarily unavailable."
)

// response is what a client sees of a problem response.
type response struct {
	problem     *prblm.Problem
	contentType string
	body        string
	status      int
}

// serve answers GET /v1/vessels/7 with err through h and returns the
// response the client receives.
func serve(t *testing.T, h *prblm.Handler, err error) response {
	t.Helper()

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return err
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/vessels/7", http.NoBody))

	res := rec.Result()
	body := rec.Body.String()

	p, parseErr := prblm.Parse(res)
	if parseErr != nil {
		t.Fatalf("Parse() error = %v, body %q", parseErr, body)
	}

	return response{
		problem:     p,
		contentType: res.Header.Get("Content-Type"),
		body:        body,
		status:      res.StatusCode,
	}
}

// handler returns a problem handler, with the default SQL mapper registered
// when registered is set.
func handler(registered bool) *prblm.Handler {
	if !registered {
		return prblm.NewHandler()
	}

	return prblm.NewHandler(prblm.WithMappers(sqlmap.Mapper()))
}

// assertProblem checks the status and detail of an about:blank JSON problem
// and that its body says nothing about database/sql.
func assertProblem(t *testing.T, got response, wantStatus int, wantDetail string) {
	t.Helper()

	if got.status != wantStatus {
		t.Errorf("status = %d, want %d", got.status, wantStatus)
	}

	if got.contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got.contentType)
	}

	if got.problem.Detail != wantDetail {
		t.Errorf("detail = %q, want %q", got.problem.Detail, wantDetail)
	}

	if got.problem.EffectiveType() != "about:blank" {
		t.Errorf("type = %q, want about:blank", got.problem.EffectiveType())
	}

	if strings.Contains(got.body, "sql:") || strings.Contains(got.body, "driver:") {
		t.Errorf("body %q mentions database/sql", got.body)
	}
}

func TestMapper_NoRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err        error
		name       string
		wantDetail string
		wantStatus int
		registered bool
	}{
		// Scenario: Registered or not.
		{name: "registered", registered: true, err: sql.ErrNoRows, wantStatus: http.StatusNotFound, wantDetail: detailNotFound},
		{name: "not registered", err: sql.ErrNoRows, wantStatus: http.StatusInternalServerError},
		// Scenario: Missing row.
		{
			name:       "wrapped",
			registered: true,
			err:        fmt.Errorf("find vessel 7: %w", sql.ErrNoRows),
			wantStatus: http.StatusNotFound,
			wantDetail: detailNotFound,
		},
		{
			name:       "joined",
			registered: true,
			err:        errors.Join(io.EOF, sql.ErrNoRows),
			wantStatus: http.StatusNotFound,
			wantDetail: detailNotFound,
		},
		{
			name:       "look-alike text",
			registered: true,
			err:        errors.New("sql: no rows in result set"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertProblem(t, serve(t, handler(tt.registered), tt.err), tt.wantStatus, tt.wantDetail)
		})
	}
}

func TestMapper_ConnFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
	}{
		{name: "bad connection", err: driver.ErrBadConn},
		{name: "wrapped bad connection", err: fmt.Errorf("ping: %w", driver.ErrBadConn)},
		{name: "connection done", err: sql.ErrConnDone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertProblem(t, serve(t, handler(true), tt.err), http.StatusServiceUnavailable, detailUnavailable)
		})
	}
}

func TestMapper_Unmapped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
	}{
		{name: "transaction done", err: sql.ErrTxDone},
		{name: "wrapped transaction done", err: fmt.Errorf("commit: %w", sql.ErrTxDone)},
		{name: "driver skip", err: driver.ErrSkip},
		{name: "driver remove argument", err: driver.ErrRemoveArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertProblem(t, serve(t, handler(true), tt.err), http.StatusInternalServerError, "")
		})
	}
}

// myError is a consumer error type, used to store a typed nil in an error.
type myError struct{}

func (*myError) Error() string { return "my error" }

func TestMapper_Degenerate(t *testing.T) {
	t.Parallel()

	var typedNil *myError

	tests := []struct {
		err  error
		name string
	}{
		{name: "nil", err: nil},
		{name: "typed nil", err: typedNil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if p := sqlmap.Mapper().MapError(t.Context(), tt.err); p != nil {
				t.Errorf("MapError() = %+v, want no match", p)
			}
		})
	}
}

func TestMapper_Override(t *testing.T) {
	t.Parallel()

	const customType = "https://example.com/probs/custom"

	typed := func(status int) func(error) *prblm.Problem {
		return func(error) *prblm.Problem {
			return prblm.New(status, prblm.WithType(customType))
		}
	}

	// Scenario: Override results. A consumer mapper after sqlmap maps both
	// cases to 410, so a declined override is visible.
	tests := []struct {
		err       error
		option    func(func(error) *prblm.Problem) sqlmap.Option
		override  func(error) *prblm.Problem
		name      string
		wantType  string
		want      int
		wantPanic bool
	}{
		{
			name:     "no rows: typed problem",
			option:   sqlmap.WithNotFound,
			err:      sql.ErrNoRows,
			override: typed(http.StatusNotFound),
			want:     http.StatusNotFound,
			wantType: customType,
		},
		{
			name:     "no rows: returns nil",
			option:   sqlmap.WithNotFound,
			err:      sql.ErrNoRows,
			override: func(error) *prblm.Problem { return nil },
			want:     http.StatusGone,
			wantType: "about:blank",
		},
		{
			name:      "no rows: panics",
			option:    sqlmap.WithNotFound,
			err:       sql.ErrNoRows,
			override:  func(error) *prblm.Problem { panic("override failed") },
			want:      http.StatusGone,
			wantType:  "about:blank",
			wantPanic: true,
		},
		{
			name:     "connection failure: typed problem",
			option:   sqlmap.WithUnavailable,
			err:      driver.ErrBadConn,
			override: typed(http.StatusServiceUnavailable),
			want:     http.StatusServiceUnavailable,
			wantType: customType,
		},
		{
			name:     "connection failure: returns nil",
			option:   sqlmap.WithUnavailable,
			err:      sql.ErrConnDone,
			override: func(error) *prblm.Problem { return nil },
			want:     http.StatusGone,
			wantType: "about:blank",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var panics []*prblm.PanicError

			h := prblm.NewHandler(
				prblm.WithMappers(
					sqlmap.Mapper(tt.option(tt.override)),
					prblm.Status(tt.err, http.StatusGone),
				),
				prblm.OnError(func(_ context.Context, err error, _ *prblm.Problem) {
					if pe, ok := errors.AsType[*prblm.PanicError](err); ok {
						panics = append(panics, pe)
					}
				}),
			)

			got := serve(t, h, tt.err)

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}

			if got.problem.EffectiveType() != tt.wantType {
				t.Errorf("type = %q, want %q", got.problem.EffectiveType(), tt.wantType)
			}

			if gotPanic := len(panics) > 0; gotPanic != tt.wantPanic {
				t.Errorf("hook received a panic = %v, want %v", gotPanic, tt.wantPanic)
			}
		})
	}
}

func TestMapper_OverrideNil(t *testing.T) {
	t.Parallel()

	// Scenario: Nil override option. Nil functions and nil options keep the
	// defaults.
	tests := []struct {
		err  error
		name string
		opts []sqlmap.Option
		want int
	}{
		{name: "no rows", opts: []sqlmap.Option{sqlmap.WithNotFound(nil)}, err: sql.ErrNoRows, want: http.StatusNotFound},
		{
			name: "connection failure",
			opts: []sqlmap.Option{sqlmap.WithUnavailable(nil)},
			err:  driver.ErrBadConn,
			want: http.StatusServiceUnavailable,
		},
		{name: "nil option", opts: []sqlmap.Option{nil}, err: sql.ErrNoRows, want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(sqlmap.Mapper(tt.opts...))), tt.err)

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}
		})
	}
}

// TestMapper_EndToEnd serves real HTTP requests from a server with the
// mapper registered.
func TestMapper_EndToEnd(t *testing.T) {
	t.Parallel()

	h := handler(true)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/vessels/{id}", h.Wrap(func(_ http.ResponseWriter, r *http.Request) error {
		switch r.PathValue("id") {
		case "7":
			return fmt.Errorf("load vessel: %w", sql.ErrNoRows)
		case "8":
			return fmt.Errorf("load vessel: %w", driver.ErrBadConn)
		default:
			return fmt.Errorf("load vessel: %w", sql.ErrTxDone)
		}
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		path string
		want int
	}{
		{path: "/v1/vessels/7", want: http.StatusNotFound},
		{path: "/v1/vessels/8", want: http.StatusServiceUnavailable},
		{path: "/v1/vessels/9", want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+tt.path, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}

			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			// Parse closes the body too; closing it twice is harmless.
			t.Cleanup(func() { _ = res.Body.Close() })

			p, err := prblm.Parse(res)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if res.StatusCode != tt.want || p.Status != tt.want {
				t.Errorf("status = %d (document %d), want %d", res.StatusCode, p.Status, tt.want)
			}
		})
	}
}
