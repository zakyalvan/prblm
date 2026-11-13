package fiberprblm

import (
	"slices"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
)

// Scope returns fiber middleware that attaches mappers to the request
// context for the routes it guards, usually a route group. They run before
// the handler's own mappers (see [prblm.ScopeMappers]), and only for those
// routes:
//
//	contracts := app.Group("/v1/contracts", fiberprblm.Scope(
//		prblm.Status(ErrLocked, http.StatusLocked),
//	))
//
// Nil mappers, including a nil [prblm.MapperFunc], are ignored, and a scope
// with no mappers changes nothing.
func Scope(mappers ...prblm.Mapper) fiber.Handler {
	if len(mappers) == 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}

	// The core skips nil mappers; the copy keeps a caller that reuses its
	// slice from changing the scope.
	mappers = slices.Clone(mappers)

	return func(c fiber.Ctx) error {
		c.SetContext(prblm.ScopeMappers(c.Context(), mappers...))

		return c.Next()
	}
}
