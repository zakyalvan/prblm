package gormmap_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/gormmap"
)

const (
	detailNotFound   = "The requested resource was not found."
	detailConflict   = "The resource conflicts with an existing one."
	detailReference  = "The resource references, or is referenced by, another resource."
	detailConstraint = "A value does not satisfy a constraint."
)

// user is a table with a unique constraint named users_email_key.
type user struct {
	Email string `gorm:"uniqueIndex:users_email_key"`
	ID    uint
}

// openDB opens a private in-memory SQLite database with the user table.
// translate sets gorm's TranslateError.
func openDB(t *testing.T, translate bool) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		TranslateError: translate,
		Logger:         logger.Discard,
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() error = %v", err)
	}

	// Every connection to file::memory: is a new, empty database, so keep
	// exactly one.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&user{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	return db
}

// findMissing returns the error of looking up a user that does not exist.
func findMissing(t *testing.T) error {
	t.Helper()

	var u user

	err := openDB(t, true).First(&u, 7).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("First() error = %v, want gorm.ErrRecordNotFound", err)
	}

	return err
}

// response is what a client sees of a problem response.
type response struct {
	problem *prblm.Problem
	body    string
	status  int
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

	body := rec.Body.String()

	p, parseErr := prblm.Parse(rec.Result())
	if parseErr != nil {
		t.Fatalf("Parse() error = %v, body %q", parseErr, body)
	}

	return response{problem: p, body: body, status: rec.Code}
}

// handler returns a problem handler, with the default gorm mapper
// registered when registered is set.
func handler(registered bool) *prblm.Handler {
	if !registered {
		return prblm.NewHandler()
	}

	return prblm.NewHandler(prblm.WithMappers(gormmap.Mapper()))
}

func TestMapper_RecordNotFound(t *testing.T) {
	t.Parallel()

	missing := findMissing(t)

	tests := []struct {
		err        error
		name       string
		wantDetail string
		wantStatus int
		registered bool
	}{
		// Scenario: Registered or not.
		{name: "registered", registered: true, err: missing, wantStatus: http.StatusNotFound, wantDetail: detailNotFound},
		{name: "not registered", err: missing, wantStatus: http.StatusInternalServerError},
		// Scenario: Missing record.
		{name: "sentinel", registered: true, err: gorm.ErrRecordNotFound, wantStatus: http.StatusNotFound, wantDetail: detailNotFound},
		{
			name:       "wrapped",
			registered: true,
			err:        fmt.Errorf("find: %w", gorm.ErrRecordNotFound),
			wantStatus: http.StatusNotFound,
			wantDetail: detailNotFound,
		},
		{
			name:       "look-alike text",
			registered: true,
			err:        errors.New("record not found"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, handler(tt.registered), tt.err)

			if got.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", got.status, tt.wantStatus)
			}

			if got.problem.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got.problem.Detail, tt.wantDetail)
			}

			if strings.Contains(got.body, "record not found") {
				t.Errorf("body %q contains the error text", got.body)
			}
		})
	}
}

// insertDuplicate inserts the same email twice and returns the error of the
// second insert, translated by gorm when translate is set.
func insertDuplicate(t *testing.T, translate bool) error {
	t.Helper()

	db := openDB(t, translate)

	if err := db.Create(&user{Email: "alice@example.com"}).Error; err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	err := db.Create(&user{Email: "alice@example.com"}).Error
	if err == nil {
		t.Fatal("second Create() succeeded, want a unique violation")
	}

	return err
}

func TestMapper_DuplicatedKey(t *testing.T) {
	t.Parallel()

	err := insertDuplicate(t, true)
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("Create() error = %v, want gorm.ErrDuplicatedKey", err)
	}

	got := serve(t, handler(true), fmt.Errorf("create user: %w", err))

	if got.status != http.StatusConflict {
		t.Errorf("status = %d, want %d", got.status, http.StatusConflict)
	}

	if got.problem.Detail != detailConflict {
		t.Errorf("detail = %q, want %q", got.problem.Detail, detailConflict)
	}

	for _, forbidden := range []string{"users_email_key", "alice@example.com", "UNIQUE", "duplicated key"} {
		if strings.Contains(got.body, forbidden) {
			t.Errorf("body %q contains %q", got.body, forbidden)
		}
	}
}

// vessel is a table whose OwnerID references user, under the foreign key
// constraint fk_vessels_owner.
type vessel struct {
	Owner   user `gorm:"constraint:fk_vessels_owner"`
	ID      uint
	OwnerID uint
}

// insertOrphan inserts a vessel whose owner does not exist, with foreign
// keys enforced and gorm translating errors, and returns the error.
func insertOrphan(t *testing.T) error {
	t.Helper()

	db := openDB(t, true)

	// SQLite enforces foreign keys per connection, and openDB keeps one.
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}

	if err := db.AutoMigrate(&vessel{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	err := db.Omit("Owner").Create(&vessel{OwnerID: 9137}).Error
	if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatalf("Create() error = %v, want gorm.ErrForeignKeyViolated", err)
	}

	return err
}

// TestMapper_ConstraintViolated covers the scenario "Translated constraint
// violations": the two sentinels map like pgxmap maps 23503 and 23514.
func TestMapper_ConstraintViolated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err        func(t *testing.T) error
		name       string
		wantDetail string
		wantStatus int
	}{
		{
			name:       "foreign key from SQLite",
			err:        insertOrphan,
			wantStatus: http.StatusConflict,
			wantDetail: detailReference,
		},
		{
			name:       "wrapped foreign key",
			err:        func(*testing.T) error { return fmt.Errorf("save: %w", gorm.ErrForeignKeyViolated) },
			wantStatus: http.StatusConflict,
			wantDetail: detailReference,
		},
		{
			name:       "wrapped check constraint",
			err:        func(*testing.T) error { return fmt.Errorf("save: %w", gorm.ErrCheckConstraintViolated) },
			wantStatus: http.StatusUnprocessableEntity,
			wantDetail: detailConstraint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, handler(true), tt.err(t))

			if got.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", got.status, tt.wantStatus)
			}

			if got.problem.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got.problem.Detail, tt.wantDetail)
			}

			for _, forbidden := range []string{"fk_vessels_owner", "9137", "FOREIGN KEY", "violated"} {
				if strings.Contains(got.body, forbidden) {
					t.Errorf("body %q contains %q", got.body, forbidden)
				}
			}
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
		{name: "invalid transaction", err: gorm.ErrInvalidTransaction},
		{name: "invalid data", err: gorm.ErrInvalidData},
		{name: "invalid field", err: gorm.ErrInvalidField},
		{name: "typed nil", err: typedNil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if p := gormmap.Mapper().MapError(t.Context(), tt.err); p != nil {
				t.Errorf("MapError() = %+v, want no match", p)
			}
		})
	}
}

// TestMapper_Untranslated covers the row "gorm mapper only" of the scenario
// "gorm without translation": the raw driver error is not gorm's to map.
func TestMapper_Untranslated(t *testing.T) {
	t.Parallel()

	err := insertDuplicate(t, false)
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("Create() error = %v, want an untranslated driver error", err)
	}

	got := serve(t, handler(true), err)

	if got.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", got.status, http.StatusInternalServerError)
	}

	if got.problem.Detail != "" {
		t.Errorf("detail = %q, want none", got.problem.Detail)
	}
}

func TestMapper_Override(t *testing.T) {
	t.Parallel()

	// Scenario: Override results, for both sentinels.
	tests := []struct {
		err       error
		option    func(func(error) *prblm.Problem) gormmap.Option
		override  func(error) *prblm.Problem
		name      string
		wantTitle string
		want      int
		wantPanic bool
	}{
		{
			name:   "record not found: typed problem",
			option: gormmap.WithRecordNotFound,
			err:    gorm.ErrRecordNotFound,
			override: func(error) *prblm.Problem {
				return prblm.New(http.StatusNotFound, prblm.WithTitle("Resource not found"))
			},
			want:      http.StatusNotFound,
			wantTitle: "Resource not found",
		},
		{
			name:      "record not found: nil function",
			option:    gormmap.WithRecordNotFound,
			err:       gorm.ErrRecordNotFound,
			want:      http.StatusNotFound,
			wantTitle: "Not Found",
		},
		{
			name:      "record not found: returns nil",
			option:    gormmap.WithRecordNotFound,
			err:       gorm.ErrRecordNotFound,
			override:  func(error) *prblm.Problem { return nil },
			want:      http.StatusInternalServerError,
			wantTitle: "Internal Server Error",
		},
		{
			name:      "record not found: panics",
			option:    gormmap.WithRecordNotFound,
			err:       gorm.ErrRecordNotFound,
			override:  func(error) *prblm.Problem { panic("override failed") },
			want:      http.StatusInternalServerError,
			wantTitle: "Internal Server Error",
			wantPanic: true,
		},
		{
			name:   "duplicated key: typed problem",
			option: gormmap.WithDuplicatedKey,
			err:    gorm.ErrDuplicatedKey,
			override: func(error) *prblm.Problem {
				return prblm.New(http.StatusConflict, prblm.WithTitle("Already exists"))
			},
			want:      http.StatusConflict,
			wantTitle: "Already exists",
		},
		{
			name:      "duplicated key: nil function",
			option:    gormmap.WithDuplicatedKey,
			err:       gorm.ErrDuplicatedKey,
			want:      http.StatusConflict,
			wantTitle: "Conflict",
		},
		{
			name:      "duplicated key: returns nil",
			option:    gormmap.WithDuplicatedKey,
			err:       gorm.ErrDuplicatedKey,
			override:  func(error) *prblm.Problem { return nil },
			want:      http.StatusInternalServerError,
			wantTitle: "Internal Server Error",
		},
		{
			name:   "foreign key: typed problem",
			option: gormmap.WithForeignKeyViolated,
			err:    gorm.ErrForeignKeyViolated,
			override: func(error) *prblm.Problem {
				return prblm.New(http.StatusUnprocessableEntity, prblm.WithTitle("Unknown owner"))
			},
			want:      http.StatusUnprocessableEntity,
			wantTitle: "Unknown owner",
		},
		{
			name:      "foreign key: nil function",
			option:    gormmap.WithForeignKeyViolated,
			err:       gorm.ErrForeignKeyViolated,
			want:      http.StatusConflict,
			wantTitle: "Conflict",
		},
		{
			name:   "check constraint: typed problem",
			option: gormmap.WithCheckConstraintViolated,
			err:    gorm.ErrCheckConstraintViolated,
			override: func(error) *prblm.Problem {
				return prblm.New(http.StatusBadRequest, prblm.WithTitle("Out of range"))
			},
			want:      http.StatusBadRequest,
			wantTitle: "Out of range",
		},
		{
			name:      "check constraint: returns nil",
			option:    gormmap.WithCheckConstraintViolated,
			err:       gorm.ErrCheckConstraintViolated,
			override:  func(error) *prblm.Problem { return nil },
			want:      http.StatusInternalServerError,
			wantTitle: "Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotPanic bool

			h := prblm.NewHandler(
				prblm.WithMappers(gormmap.Mapper(tt.option(tt.override), nil)),
				prblm.OnError(func(_ context.Context, err error, _ *prblm.Problem) {
					if _, ok := errors.AsType[*prblm.PanicError](err); ok {
						gotPanic = true
					}
				}),
			)

			got := serve(t, h, tt.err)

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}

			if got.problem.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", got.problem.Title, tt.wantTitle)
			}

			if gotPanic != tt.wantPanic {
				t.Errorf("hook received a panic = %v, want %v", gotPanic, tt.wantPanic)
			}
		})
	}
}

// TestMapper_EndToEnd serves real HTTP requests from a server whose
// handlers use gorm on SQLite, with the mapper registered.
func TestMapper_EndToEnd(t *testing.T) {
	t.Parallel()

	db := openDB(t, true)
	if err := db.Create(&user{ID: 1, Email: "alice@example.com"}).Error; err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	h := handler(true)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/users/{id}", h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		var u user
		if err := db.WithContext(r.Context()).First(&u, "id = ?", r.PathValue("id")).Error; err != nil {
			return fmt.Errorf("load user: %w", err)
		}

		_, err := w.Write([]byte(u.Email))

		return err
	}))
	mux.Handle("POST /v1/users", h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		if err := db.WithContext(r.Context()).Create(&user{Email: r.FormValue("email")}).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		w.WriteHeader(http.StatusCreated)

		return nil
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name   string
		method string
		target string
		form   string
		want   int
	}{
		{name: "found", method: http.MethodGet, target: "/v1/users/1", want: http.StatusOK},
		{name: "missing", method: http.MethodGet, target: "/v1/users/7", want: http.StatusNotFound},
		{name: "duplicate", method: http.MethodPost, target: "/v1/users", form: "email=alice%40example.com", want: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), tt.method, srv.URL+tt.target, strings.NewReader(tt.form))
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}

			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()

			if err != nil {
				t.Fatal(err)
			}

			if res.StatusCode != tt.want {
				t.Errorf("status = %d, want %d, body %q", res.StatusCode, tt.want, body)
			}

			if tt.want >= http.StatusBadRequest && strings.Contains(string(body), "alice") {
				t.Errorf("problem body %q leaks the row value", body)
			}
		})
	}
}
