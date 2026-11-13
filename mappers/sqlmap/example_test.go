package sqlmap_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/sqlmap"
)

func ExampleMapper() {
	h := prblm.NewHandler(prblm.WithMappers(sqlmap.Mapper()))

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		// For example, the error of a QueryRowContext(...).Scan(...) call.
		return fmt.Errorf("load vessel 7: %w", sql.ErrNoRows)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Body.String())
	// Output:
	// 404
	// {"title":"Not Found","status":404,"detail":"The requested resource was not found.","instance":"/v1/vessels/7"}
}

func ExampleWithNotFound() {
	h := prblm.NewHandler(prblm.WithMappers(sqlmap.Mapper(
		sqlmap.WithNotFound(func(error) *prblm.Problem {
			return prblm.New(http.StatusNotFound,
				prblm.WithType("https://example.com/probs/not-found"),
				prblm.WithTitle("Resource not found"),
			)
		}),
	)))

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return sql.ErrNoRows
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output:
	// {"type":"https://example.com/probs/not-found","title":"Resource not found","status":404,"instance":"/v1/vessels/7"}
}
