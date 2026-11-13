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

func ExampleMiddleware_abort() {
	engine := gin.New()
	engine.Use(ginprblm.Middleware(prblm.NewHandler()))
	engine.GET("/v1/vessels/:id", func(c *gin.Context) {
		// The error's text is never sent; 403 is used as the status.
		_ = c.AbortWithError(http.StatusForbidden, errors.New("token abc expired"))
	})
	engine.GET("/v1/reports", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})

	for _, target := range []string{"/v1/vessels/7", "/v1/reports"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		fmt.Println(rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	// Output:
	// 403 application/problem+json {"title":"Forbidden","status":403,"instance":"/v1/vessels/7"}
	// 401 application/problem+json {"title":"Unauthorized","status":401,"instance":"/v1/reports"}
}
