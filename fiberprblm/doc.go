// Package fiberprblm renders errors returned by fiber v3 handlers as RFC 9457
// problem details, with the same output as the net/http adapter of
// [github.com/kartaladev/prblm].
//
// # Quick start
//
// Install [ErrorHandler], built with the same options [prblm.NewHandler]
// takes, as the application's error handler and return errors from
// handlers, as fiber handlers usually do:
//
//	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.WithMappers(
//		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
//	))})
//	app.Get("/v1/vessels/:id", func(c fiber.Ctx) error {
//		vessel, err := loadVessel(c.Context(), c.Params("id"))
//		if err != nil {
//			return err // written as a problem response
//		}
//		return c.JSON(vessel)
//	})
//
// The error is resolved by a problem handler built from those options,
// within the request's context (c.Context()), so mappers, enrichers, the
// OnError hook and content negotiation behave exactly as with net/http.
//
// # Fiber errors
//
// The code of a [*fiber.Error] in the error chain, including fiber's own 404
// and 405 for unknown routes and methods, is used as a status hint: a mapper
// or a self-describing error still takes precedence, and a code outside
// 400–599 is ignored. Its message is never sent, so "Cannot GET /x" and
// messages such as fiber.NewError(403, "token abc expired") stay private.
//
// # Route groups
//
// [Scope] attaches mappers to the routes of a group only, ahead of the
// handler's own mappers:
//
//	contracts := app.Group("/v1/contracts", fiberprblm.Scope(
//		prblm.Status(ErrLocked, http.StatusLocked),
//	))
//
// # Panic recovery
//
// Recovery is opt-in. [Recovery] turns a panic into an error returned to the
// error handler; pair it with [ErrorHandler], so the client gets the core's
// safe 500 problem and the OnError hook gets the panic value and stack:
//
//	app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.OnError(logProblem))})
//	app.Use(fiberprblm.Recovery())
//
// With fiber's default error handler instead, the client gets a plain-text
// 500 "Internal Server Error": the panic value never leaks either way.
//
// # Safe defaults
//
// The problem semantics are the core's, unchanged: an error no mapper
// recognizes becomes a 500 problem without its text, the instance is the
// request path without the query string, and the format is JSON unless the
// client prefers XML.
//
// Fiber sends nothing before the handler returns, so a body the handler
// buffered (for example with c.JSON) is replaced by the problem, while
// headers it set are kept, except those the problem controls. A body stream
// the handler started is never replaced; the error then only reaches the
// hook. When the request context is canceled and the error wraps
// [context.Canceled], nothing is written and the error only reaches the
// hook. Should the core itself panic while resolving an error (for example
// on an error whose Unwrap method panics), the response is a bare 500
// without a body.
//
// A configuration mistake, such as an invalid option, is a programming
// mistake: [ErrorHandler] panics when it is created, so it shows at startup
// and never inside a request.
package fiberprblm
