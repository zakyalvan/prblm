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

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.Status(errVesselNotFound, http.StatusNotFound),
	))

	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(h)})
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
