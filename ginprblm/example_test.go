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

func ExampleRecovery() {
	h := prblm.NewHandler(prblm.OnError(func(_ context.Context, err error, _ *prblm.Problem) {
		if pe, ok := errors.AsType[*prblm.PanicError](err); ok {
			fmt.Println("hook:", pe.Value) // log the value and pe.Stack here
		}
	}))

	engine := gin.New()
	engine.Use(ginprblm.Recovery(h), ginprblm.Middleware(h))
	engine.GET("/v1/vessels/:id", func(*gin.Context) {
		panic("db password=hunter2")
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// hook: db password=hunter2
	// 500 {"title":"Internal Server Error","status":500,"instance":"/v1/vessels/7"}
}

func ExampleScope() {
	errLocked := errors.New("locked")

	engine := gin.New()
	engine.Use(ginprblm.Middleware(prblm.NewHandler()))
	engine.Group("/v1/contracts", ginprblm.Scope(prblm.Status(errLocked, http.StatusLocked))).
		GET("/:id", func(c *gin.Context) { _ = c.Error(errLocked) })
	engine.GET("/v1/vessels/:id", func(c *gin.Context) { _ = c.Error(errLocked) })

	for _, target := range []string{"/v1/contracts/1", "/v1/vessels/1"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		fmt.Println(target, rec.Code)
	}
	// Output:
	// /v1/contracts/1 423
	// /v1/vessels/1 500
}
