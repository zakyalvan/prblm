package pgxmap_test

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/pgxmap"
	"github.com/kartaladev/prblm/mappers/sqlmap"
)

const (
	detailConflict    = "The resource conflicts with an existing one."
	detailReference   = "The resource references, or is referenced by, another resource."
	detailMissing     = "A required value is missing."
	detailConstraint  = "A value does not satisfy a constraint."
	detailRetry       = "The request conflicted with a concurrent change; retry it."
	detailUnavailable = "The service is temporarily unavailable."
	detailTimeout     = "The request took too long to complete."
)

// response is what a client sees of a problem response.
type response struct {
	header  http.Header
	problem *prblm.Problem
	body    string
	status  int
}

// serve answers GET /v1/members/9 with err through h, with the given Accept
// header, and returns the response the client receives.
func serve(t *testing.T, h *prblm.Handler, accept string, err error) response {
	t.Helper()

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return err
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/members/9", http.NoBody)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	body := rec.Body.String()

	p, parseErr := prblm.Parse(res)
	if parseErr != nil {
		t.Fatalf("Parse() error = %v, body %q", parseErr, body)
	}

	return response{header: res.Header, problem: p, body: body, status: res.StatusCode}
}

// handler returns a problem handler with the PostgreSQL mapper, built with
// opts, registered.
func handler(opts ...pgxmap.Option) *prblm.Handler {
	return prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper(opts...)))
}

// pgError returns a PostgreSQL error with the given SQLSTATE code, wrapped
// the way a repository would return it.
func pgError(code string) error {
	return fmt.Errorf("insert user: %w", &pgconn.PgError{
		Severity: "ERROR",
		Code:     code,
		Message:  "server message for " + code,
	})
}

func TestMapper_Codes(t *testing.T) {
	t.Parallel()

	t.Run("not registered", func(t *testing.T) {
		t.Parallel()

		got := serve(t, prblm.NewHandler(), "", pgError("23505"))
		if got.status != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", got.status, http.StatusInternalServerError)
		}
	})

	tests := []struct {
		code       string
		wantDetail string
		wantStatus int
	}{
		// Examples: integrity violations.
		{code: "23505", wantStatus: http.StatusConflict, wantDetail: detailConflict},
		{code: "23503", wantStatus: http.StatusConflict, wantDetail: detailReference},
		{code: "23502", wantStatus: http.StatusUnprocessableEntity, wantDetail: detailMissing},
		{code: "23514", wantStatus: http.StatusUnprocessableEntity, wantDetail: detailConstraint},
		// Examples: retryable and capacity.
		{code: "40001", wantStatus: http.StatusConflict, wantDetail: detailRetry},
		{code: "40P01", wantStatus: http.StatusConflict, wantDetail: detailRetry},
		{code: "57014", wantStatus: http.StatusGatewayTimeout, wantDetail: detailTimeout},
		{code: "53300", wantStatus: http.StatusServiceUnavailable, wantDetail: detailUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()

			got := serve(t, handler(), "", pgError(tt.code))

			if got.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", got.status, tt.wantStatus)
			}

			if got.problem.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got.problem.Detail, tt.wantDetail)
			}
		})
	}
}

func TestMapper_Unmapped(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"42P01", "42601", "08006", "", "ZZZZZ"} {
		t.Run("code "+code, func(t *testing.T) {
			t.Parallel()

			got := serve(t, handler(), "", pgError(code))

			if got.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", got.status, http.StatusInternalServerError)
			}

			if got.problem.Detail != "" {
				t.Errorf("detail = %q, want none", got.problem.Detail)
			}
		})
	}
}

func TestMapper_Degenerate(t *testing.T) {
	t.Parallel()

	var typedNil *pgconn.PgError

	tests := []struct {
		err        error
		name       string
		wantStatus int
	}{
		{name: "nil", err: nil},
		{name: "typed nil", err: typedNil},
		{name: "wrapped typed nil", err: fmt.Errorf("query: %w", typedNil)},
		{name: "pgx no rows", err: pgx.ErrNoRows},
		{name: "look-alike text", err: errors.New("ERROR: duplicate key (SQLSTATE 23505)")},
		{name: "joined", err: errors.Join(io.EOF, pgError("23505")), wantStatus: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := pgxmap.Mapper().MapError(t.Context(), tt.err)

			var gotStatus int
			if p != nil {
				gotStatus = p.Status
			}

			if gotStatus != tt.wantStatus {
				t.Errorf("MapError() status = %d, want %d (0 = no match)", gotStatus, tt.wantStatus)
			}
		})
	}
}

// TestMapper_PgxNoRows proves the godoc: pgx.ErrNoRows wraps sql.ErrNoRows,
// so it is the SQL mapper's to map, never this one's.
func TestMapper_PgxNoRows(t *testing.T) {
	t.Parallel()

	if !errors.Is(pgx.ErrNoRows, sql.ErrNoRows) {
		t.Fatal("pgx.ErrNoRows does not wrap sql.ErrNoRows")
	}

	tests := []struct {
		name    string
		mappers []prblm.Mapper
		want    int
	}{
		{name: "PostgreSQL mapper, SQL mapper", mappers: []prblm.Mapper{pgxmap.Mapper(), sqlmap.Mapper()}, want: http.StatusNotFound},
		{name: "PostgreSQL mapper only", mappers: []prblm.Mapper{pgxmap.Mapper()}, want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(tt.mappers...)), "", fmt.Errorf("find account: %w", pgx.ErrNoRows))

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}
		})
	}
}

func TestMapper_NoLeak(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		message    string
		detail     string
		constraint string
		forbidden  []string
	}{
		{
			name:       "duplicate email",
			message:    `duplicate key value violates unique constraint "users_email_key"`,
			detail:     "Key (email)=(alice@example.com) already exists.",
			constraint: "users_email_key",
			forbidden:  []string{"alice@example.com", "users_email_key", "duplicate key"},
		},
		{
			name:       "foreign key",
			message:    `insert or update on table "orders" violates foreign key constraint "orders_user_fk"`,
			detail:     `Key (user_id)=(42) is not present in table "users".`,
			constraint: "orders_user_fk",
			forbidden:  []string{"orders", "users", "42"},
		},
	}

	for _, tt := range tests {
		for _, accept := range []string{"application/problem+json", "application/problem+xml"} {
			t.Run(tt.name+" "+accept, func(t *testing.T) {
				t.Parallel()

				err := &pgconn.PgError{
					Severity:       "ERROR",
					Code:           "23505",
					Message:        tt.message,
					Detail:         tt.detail,
					Hint:           "hint from the server",
					SchemaName:     "public",
					TableName:      "users",
					ColumnName:     "email",
					DataTypeName:   "text",
					ConstraintName: tt.constraint,
					Where:          "SQL statement in orders",
				}

				got := serve(t, handler(), accept, err)

				if got.status != http.StatusConflict {
					t.Errorf("status = %d, want %d", got.status, http.StatusConflict)
				}

				for _, f := range append(tt.forbidden, "hint from the server", "public", "email", "text") {
					if strings.Contains(got.body, f) {
						t.Errorf("body %q contains %q", got.body, f)
					}
				}
			})
		}
	}
}

// assertPanics checks that fn panics with a message containing want.
func assertPanics(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		t.Helper()

		r := recover()
		if r == nil {
			t.Fatalf("no panic, want one naming %q", want)
		}

		if msg := fmt.Sprint(r); !strings.Contains(msg, want) {
			t.Errorf("panic %q does not name %q", msg, want)
		}
	}()

	fn()
}

func TestMapper_RetryAfter(t *testing.T) {
	t.Parallel()

	// Scenario: Retry-After header.
	tests := []struct {
		name string
		code string
		want string
		opts []pgxmap.Option
	}{
		{name: "default deadlock", code: "40P01", want: "1"},
		{name: "default too many connections", code: "53300", want: "1"},
		{name: "default serialization failure", code: "40001", want: "1"},
		{name: "query canceled is not retryable", code: "57014", want: ""},
		{name: "5 seconds", code: "40001", opts: []pgxmap.Option{pgxmap.WithRetryAfter(5 * time.Second)}, want: "5"},
		{name: "rounded down", code: "40001", opts: []pgxmap.Option{pgxmap.WithRetryAfter(2500 * time.Millisecond)}, want: "2"},
		{name: "not retryable", code: "23505", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, handler(tt.opts...), "", pgError(tt.code))

			if v := got.header.Get("Retry-After"); v != tt.want {
				t.Errorf("Retry-After = %q, want %q", v, tt.want)
			}
		})
	}
}

func TestMapper_RetryAfterInvalid(t *testing.T) {
	t.Parallel()

	// Scenario: Invalid Retry-After setting.
	for _, d := range []time.Duration{0, -time.Second, 999 * time.Millisecond} {
		t.Run(d.String(), func(t *testing.T) {
			t.Parallel()

			assertPanics(t, "WithRetryAfter", func() {
				pgxmap.Mapper(pgxmap.WithRetryAfter(d))
			})
		})
	}
}

func TestMapper_DatabaseDetail(t *testing.T) {
	t.Parallel()

	const dbDetail = "Key (email)=(alice@example.com) already exists."

	// Scenario: Detail exposure.
	tests := []struct {
		name     string
		pgDetail string
		want     string
		opts     []pgxmap.Option
	}{
		{name: "exposed", opts: []pgxmap.Option{pgxmap.WithDatabaseDetail()}, pgDetail: dbDetail, want: dbDetail},
		{name: "without options", pgDetail: dbDetail, want: detailConflict},
		{name: "exposed but empty", opts: []pgxmap.Option{pgxmap.WithDatabaseDetail()}, want: detailConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &pgconn.PgError{Code: "23505", Message: "duplicate key value", Detail: tt.pgDetail}

			got := serve(t, handler(tt.opts...), "", err)

			if got.problem.Detail != tt.want {
				t.Errorf("detail = %q, want %q", got.problem.Detail, tt.want)
			}

			if strings.Contains(got.body, "duplicate key value") {
				t.Errorf("body %q contains the PostgreSQL message", got.body)
			}
		})
	}
}

func TestMapper_Constraint(t *testing.T) {
	t.Parallel()

	emailTaken := func(*pgconn.PgError) *prblm.Problem {
		return prblm.New(http.StatusConflict, prblm.WithTitle("Email already registered"))
	}

	// Scenario: Friendly duplicate email.
	tests := []struct {
		fn         func(*pgconn.PgError) *prblm.Problem
		name       string
		constraint string
		wantTitle  string
		want       int
	}{
		{name: "matching", fn: emailTaken, constraint: "users_email_key", want: http.StatusConflict, wantTitle: "Email already registered"},
		{name: "other constraint", fn: emailTaken, constraint: "users_phone_key", want: http.StatusConflict, wantTitle: "Conflict"},
		{name: "case mismatch", fn: emailTaken, constraint: "USERS_EMAIL_KEY", want: http.StatusConflict, wantTitle: "Conflict"},
		{
			name:       "returns nil",
			fn:         func(*pgconn.PgError) *prblm.Problem { return nil },
			constraint: "users_email_key",
			want:       http.StatusConflict,
			wantTitle:  "Conflict",
		},
		{
			name:       "panics",
			fn:         func(*pgconn.PgError) *prblm.Problem { panic("constraint function failed") },
			constraint: "users_email_key",
			want:       http.StatusInternalServerError,
			wantTitle:  "Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &pgconn.PgError{Code: "23505", ConstraintName: tt.constraint}

			got := serve(t, handler(pgxmap.WithConstraint("users_email_key", tt.fn)), "", err)

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}

			if got.problem.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", got.problem.Title, tt.wantTitle)
			}
		})
	}

	t.Run("receives the error", func(t *testing.T) {
		t.Parallel()

		var got *pgconn.PgError

		want := &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}
		m := pgxmap.Mapper(pgxmap.WithConstraint("users_email_key", func(pe *pgconn.PgError) *prblm.Problem {
			got = pe

			return prblm.New(http.StatusConflict)
		}))

		_ = m.MapError(t.Context(), fmt.Errorf("insert: %w", want))

		if got != want {
			t.Errorf("function received %p, want %p", got, want)
		}
	})
}

func TestMapper_ConstraintInvalid(t *testing.T) {
	t.Parallel()

	valid := func(*pgconn.PgError) *prblm.Problem { return nil }

	// Scenario: Invalid constraint registration.
	tests := []struct {
		fn       func(*pgconn.PgError) *prblm.Problem
		name     string
		argument string
	}{
		{name: "", fn: valid, argument: "constraint name"},
		{name: "users_email_key", fn: nil, argument: "problem function"},
	}

	for _, tt := range tests {
		t.Run(tt.argument, func(t *testing.T) {
			t.Parallel()

			assertPanics(t, tt.argument, func() {
				pgxmap.Mapper(pgxmap.WithConstraint(tt.name, tt.fn))
			})
		})
	}
}

// declines is a problem function that declines every error.
func declines(*pgconn.PgError) *prblm.Problem { return nil }

func TestMapper_CodeOverride(t *testing.T) {
	t.Parallel()

	status := func(s int) func(*pgconn.PgError) *prblm.Problem {
		return func(*pgconn.PgError) *prblm.Problem { return prblm.New(s) }
	}

	// Scenario: Code overrides.
	tests := []struct {
		fn   func(*pgconn.PgError) *prblm.Problem
		code string
		want int
	}{
		{code: "23503", fn: status(http.StatusUnprocessableEntity), want: http.StatusUnprocessableEntity},
		{code: "23P01", fn: status(http.StatusConflict), want: http.StatusConflict},
		{code: "23503", fn: declines, want: http.StatusConflict},
		{code: "42P01", fn: declines, want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %d", tt.code, tt.want), func(t *testing.T) {
			t.Parallel()

			got := serve(t, handler(pgxmap.WithCode(tt.code, tt.fn)), "", pgError(tt.code))

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}
		})
	}

	t.Run("constraint first", func(t *testing.T) {
		t.Parallel()

		m := pgxmap.Mapper(
			pgxmap.WithCode("23505", status(http.StatusUnprocessableEntity)),
			pgxmap.WithConstraint("users_email_key", status(http.StatusGone)),
		)

		p := m.MapError(t.Context(), &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"})
		if p == nil || p.Status != http.StatusGone {
			t.Errorf("MapError() = %v, want the constraint's 410", p)
		}
	})
}

func TestMapper_CodeOverrideInvalid(t *testing.T) {
	t.Parallel()

	// Scenario: Invalid code registration.
	for _, code := range []string{"", "2350", "235050", "40p01", "23 05", "2350\u00e9"} {
		t.Run("code "+code, func(t *testing.T) {
			t.Parallel()

			assertPanics(t, "SQLSTATE code", func() {
				pgxmap.Mapper(pgxmap.WithCode(code, declines))
			})
		})
	}

	t.Run("nil function", func(t *testing.T) {
		t.Parallel()

		assertPanics(t, "problem function", func() {
			pgxmap.Mapper(pgxmap.WithCode("23505", nil))
		})
	})
}

// TestMapper_EndToEnd serves real HTTP requests, in JSON and XML, from a
// server with the mapper registered.
func TestMapper_EndToEnd(t *testing.T) {
	t.Parallel()

	h := handler()

	mux := http.NewServeMux()
	mux.Handle("POST /v1/members/{code}", h.Wrap(func(_ http.ResponseWriter, r *http.Request) error {
		return pgError(r.PathValue("code"))
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		code       string
		retryAfter string
		want       int
	}{
		{code: "23505", want: http.StatusConflict},
		{code: "23502", want: http.StatusUnprocessableEntity},
		{code: "53300", want: http.StatusServiceUnavailable, retryAfter: "1"},
		{code: "42P01", want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		for _, accept := range []string{"application/problem+json", "application/problem+xml"} {
			t.Run(tt.code+" "+accept, func(t *testing.T) {
				t.Parallel()

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v1/members/"+tt.code, http.NoBody)
				if err != nil {
					t.Fatal(err)
				}

				req.Header.Set("Accept", accept)

				res, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				// Parse closes the body too; closing it twice is harmless.
				t.Cleanup(func() { _ = res.Body.Close() })

				if ct := res.Header.Get("Content-Type"); ct != accept {
					t.Errorf("Content-Type = %q, want %q", ct, accept)
				}

				if v := res.Header.Get("Retry-After"); v != tt.retryAfter {
					t.Errorf("Retry-After = %q, want %q", v, tt.retryAfter)
				}

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
}
