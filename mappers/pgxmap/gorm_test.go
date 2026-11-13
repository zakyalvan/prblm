package pgxmap_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
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
		_ = tx.AddError(&pgconn.PgError{
			Severity:       "ERROR",
			Code:           "23505",
			Message:        `duplicate key value violates unique constraint "accounts_email_key"`,
			ConstraintName: "accounts_email_key",
		})
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

// TestMapper_GormPassThrough covers the row "gorm mapper, PostgreSQL mapper"
// of the gorm-error-mapping scenario "gorm without translation".
func TestMapper_GormPassThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mappers []prblm.Mapper
		want    int
	}{
		{name: "gorm mapper, PostgreSQL mapper", mappers: []prblm.Mapper{gormmap.Mapper(), pgxmap.Mapper()}, want: http.StatusConflict},
		{name: "gorm mapper only", mappers: []prblm.Mapper{gormmap.Mapper()}, want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(tt.mappers...)), "", untranslatedUniqueViolation(t))

			if got.status != tt.want {
				t.Errorf("status = %d, want %d", got.status, tt.want)
			}
		})
	}
}
