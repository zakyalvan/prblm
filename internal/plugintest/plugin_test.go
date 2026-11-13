package plugintest

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils/tests"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/gormmap"
	"github.com/kartaladev/prblm/mappers/pgxmap"
)

// account is a table for gorm.
type account struct {
	Email string
	ID    uint
}

// uniqueViolation returns the error PostgreSQL reports for a second account
// with the same email, on the constraint accounts_email_key.
func uniqueViolation() *pgconn.PgError {
	return &pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "accounts_email_key"`,
		Detail:         "Key (email)=(alice@example.com) already exists.",
		ConstraintName: "accounts_email_key",
	}
}

// status returns the status of the problem a handler with the given
// mappers resolves err to.
func status(t *testing.T, err error, mappers ...prblm.Mapper) int {
	t.Helper()

	return prblm.NewHandler(prblm.WithMappers(mappers...)).Resolve(t.Context(), err).Status
}

// untranslatedUniqueViolation returns the error gorm gives for a unique
// violation reported by a PostgreSQL driver when TranslateError is off: the
// driver's *pgconn.PgError, passed through gorm's error path. A create
// callback stands in for the driver, so no database is needed.
func untranslatedUniqueViolation(t *testing.T) error {
	t.Helper()

	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{
		DryRun:         true,
		TranslateError: false,
		Logger:         logger.Discard,
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	err = db.Callback().Create().Before("gorm:create").Register("test:unique_violation", func(tx *gorm.DB) {
		_ = tx.AddError(uniqueViolation())
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	err = db.Create(&account{Email: "alice@example.com"}).Error

	if _, ok := errors.AsType[*pgconn.PgError](err); !ok {
		t.Fatalf("Create() error = %v, want a *pgconn.PgError in the chain", err)
	}

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("Create() error = %v, want it untranslated", err)
	}

	return err
}

// translatedUniqueViolation returns the error gorm gives for a unique
// violation when TranslateError is on: the PostgreSQL driver's translation
// of the *pgconn.PgError, which keeps it in the chain.
func translatedUniqueViolation(t *testing.T) error {
	t.Helper()

	err := postgres.Dialector{}.Translate(uniqueViolation())

	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("Translate() = %v, want gorm.ErrDuplicatedKey in the chain", err)
	}

	if _, ok := errors.AsType[*pgconn.PgError](err); !ok {
		t.Fatalf("Translate() = %v, want a *pgconn.PgError in the chain", err)
	}

	return err
}

// TestGorm covers the gorm-error-mapping scenarios "gorm without
// translation" and "Translated error with a constraint problem": registered
// first, the PostgreSQL mapper answers both, keeping its constraint
// problem; registered second, it never sees a translated error.
func TestGorm(t *testing.T) {
	t.Parallel()

	friendly := pgxmap.Mapper(pgxmap.WithConstraint("accounts_email_key", func(*pgconn.PgError) *prblm.Problem {
		return prblm.New(http.StatusUnprocessableEntity, prblm.WithTitle("Email already registered"))
	}))

	tests := []struct {
		err     func(t *testing.T) error
		name    string
		mappers []prblm.Mapper
		want    int
	}{
		{
			name:    "untranslated: PostgreSQL mapper, gorm mapper",
			err:     untranslatedUniqueViolation,
			mappers: []prblm.Mapper{pgxmap.Mapper(), gormmap.Mapper()},
			want:    http.StatusConflict,
		},
		{
			name:    "untranslated: gorm mapper, PostgreSQL mapper",
			err:     untranslatedUniqueViolation,
			mappers: []prblm.Mapper{gormmap.Mapper(), pgxmap.Mapper()},
			want:    http.StatusConflict,
		},
		{
			name:    "untranslated: gorm mapper only",
			err:     untranslatedUniqueViolation,
			mappers: []prblm.Mapper{gormmap.Mapper()},
			want:    http.StatusInternalServerError,
		},
		{
			name:    "translated: PostgreSQL mapper, gorm mapper",
			err:     translatedUniqueViolation,
			mappers: []prblm.Mapper{friendly, gormmap.Mapper()},
			want:    http.StatusUnprocessableEntity,
		},
		{
			name:    "translated: gorm mapper, PostgreSQL mapper",
			err:     translatedUniqueViolation,
			mappers: []prblm.Mapper{gormmap.Mapper(), friendly},
			want:    http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := fmt.Errorf("create account: %w", tt.err(t))

			if got := status(t, err, tt.mappers...); got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}
