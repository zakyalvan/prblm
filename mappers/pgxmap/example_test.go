package pgxmap_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/gormmap"
	"github.com/kartaladev/prblm/mappers/pgxmap"
)

// errDuplicateEmail is the error PostgreSQL reports for a second account with
// the same email.
var errDuplicateEmail = &pgconn.PgError{
	Severity:       "ERROR",
	Code:           "23505",
	Message:        `duplicate key value violates unique constraint "users_email_key"`,
	Detail:         "Key (email)=(alice@example.com) already exists.",
	ConstraintName: "users_email_key",
}

// post answers POST /v1/users with err through h and prints the response.
func post(h *prblm.Handler, err error) {
	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("insert user: %w", err)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/users", http.NoBody)
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Body.String())
}

func ExampleMapper() {
	h := prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper()))

	// Neither the constraint name nor the email reaches the client.
	post(h, errDuplicateEmail)
	// Output:
	// 409
	// {"title":"Conflict","status":409,"detail":"The resource conflicts with an existing one.","instance":"/v1/users"}
}

func ExampleWithConstraint() {
	h := prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper(
		pgxmap.WithConstraint("users_email_key", func(*pgconn.PgError) *prblm.Problem {
			return prblm.New(http.StatusConflict,
				prblm.WithType("https://example.com/probs/email-taken"),
				prblm.WithTitle("Email already registered"),
				prblm.WithDetail("Sign in, or use another email address."),
			)
		}),
	)))

	post(h, errDuplicateEmail)
	// Output:
	// 409
	// {"type":"https://example.com/probs/email-taken","title":"Email already registered","status":409,"detail":"Sign in, or use another email address.","instance":"/v1/users"}
}

func ExampleWithCode() {
	h := prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper(
		// An exclusion violation, which the default table leaves unmapped.
		pgxmap.WithCode("23P01", func(*pgconn.PgError) *prblm.Problem {
			return prblm.New(http.StatusConflict, prblm.WithDetail("The booking overlaps another one."))
		}),
	)))

	post(h, &pgconn.PgError{Code: "23P01", ConstraintName: "bookings_no_overlap"})
	// Output:
	// 409
	// {"title":"Conflict","status":409,"detail":"The booking overlaps another one.","instance":"/v1/users"}
}

// Example_gorm registers the PostgreSQL mapper after the gorm mapper, so a
// unique violation is answered whether gorm translated it or not.
func Example_gorm() {
	h := prblm.NewHandler(prblm.WithMappers(
		gormmap.Mapper(),
		pgxmap.Mapper(),
	))

	// gorm with TranslateError: gorm.ErrDuplicatedKey.
	post(h, gorm.ErrDuplicatedKey)
	// gorm without TranslateError: the driver's error.
	post(h, errDuplicateEmail)
	// Output:
	// 409
	// {"title":"Conflict","status":409,"detail":"The resource conflicts with an existing one.","instance":"/v1/users"}
	// 409
	// {"title":"Conflict","status":409,"detail":"The resource conflicts with an existing one.","instance":"/v1/users"}
}
