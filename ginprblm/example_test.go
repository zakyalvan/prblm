package ginprblm_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

func ExampleMiddleware() {
	errVesselNotFound := errors.New("vessel not found")

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.Status(errVesselNotFound, http.StatusNotFound),
	))

	engine := gin.New()
	engine.Use(ginprblm.Middleware(h))
	engine.GET("/v1/vessels/:id", func(c *gin.Context) {
		_ = c.Error(errVesselNotFound) // written as a problem response
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	fmt.Println(rec.Code, rec.Header().Get("Content-Type"))
	fmt.Println(rec.Body.String())
	// Output:
	// 404 application/problem+json
	// {"title":"Not Found","status":404,"instance":"/v1/vessels/7"}
}
