package fiberprblm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/fiberprblm"
)

func ExampleErrorHandler() {
	errVesselNotFound := errors.New("vessel not found")

	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.WithMappers(
		prblm.Status(errVesselNotFound, http.StatusNotFound),
	))})
	app.Get("/v1/vessels/:id", func(fiber.Ctx) error {
		return errVesselNotFound // written as a problem response
	})

	for _, target := range []string{"/v1/vessels/7", "/v1/unknown"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)

		res, err := app.Test(req)
		if err != nil {
			fmt.Println(err)

			return
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		fmt.Println(res.StatusCode, res.Header.Get("Content-Type"), string(body))
	}
	// Output:
	// 404 application/problem+json {"title":"Not Found","status":404,"instance":"/v1/vessels/7"}
	// 404 application/problem+json {"title":"Not Found","status":404,"instance":"/v1/unknown"}
}

func ExampleRecovery() {
	logProblem := func(_ context.Context, err error, _ *prblm.Problem) {
		if pe, ok := errors.AsType[*prblm.PanicError](err); ok {
			fmt.Println("hook:", pe.Value) // log the value and pe.Stack here
		}
	}

	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.OnError(logProblem))})
	app.Use(fiberprblm.Recovery())
	app.Get("/v1/vessels/:id", func(fiber.Ctx) error {
		panic("db password=hunter2")
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)

	res, err := app.Test(req)
	if err != nil {
		fmt.Println(err)

		return
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	fmt.Println(res.StatusCode, string(body))
	// Output:
	// hook: db password=hunter2
	// 500 {"title":"Internal Server Error","status":500,"instance":"/v1/vessels/7"}
}

func ExampleScope() {
	errLocked := errors.New("locked")

	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler()})
	app.Group("/v1/contracts", fiberprblm.Scope(prblm.Status(errLocked, http.StatusLocked))).
		Get("/:id", func(fiber.Ctx) error { return errLocked })
	app.Get("/v1/vessels/:id", func(fiber.Ctx) error { return errLocked })

	for _, target := range []string{"/v1/contracts/1", "/v1/vessels/1"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)

		res, err := app.Test(req)
		if err != nil {
			fmt.Println(err)

			return
		}

		_ = res.Body.Close()

		fmt.Println(target, res.StatusCode)
	}
	// Output:
	// /v1/contracts/1 423
	// /v1/vessels/1 500
}
