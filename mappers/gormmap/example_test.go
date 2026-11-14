package gormmap_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"gorm.io/gorm"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/gormmap"
)

func ExampleMapper() {
	h := prblm.NewHandler(prblm.WithMappers(gormmap.Mapper()))

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		// For example, the error of db.First(&vessel, 7).Error.
		return fmt.Errorf("load vessel 7: %w", gorm.ErrRecordNotFound)
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
