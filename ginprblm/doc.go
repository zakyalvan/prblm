// Package ginprblm renders errors in gin handlers as RFC 9457 problem
// details, with the same output as the net/http adapter of
// [github.com/kartaladev/prblm].
//
// # Quick start
//
// Install [Middleware] with the same options [prblm.NewHandler] takes, and
// record errors with c.Error, as gin handlers usually do:
//
//	engine := gin.New()
//	engine.Use(ginprblm.Middleware(prblm.WithMappers(
//		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
//	)))
//	engine.GET("/v1/vessels/:id", func(c *gin.Context) {
//		vessel, err := loadVessel(c.Request.Context(), c.Param("id"))
//		if err != nil {
//			_ = c.Error(err) // written as a problem response
//			return
//		}
//		c.JSON(http.StatusOK, vessel)
//	})
//
// Once the handlers after it have returned, the middleware resolves the last
// recorded error with a problem handler built from those options, so
// mappers, enrichers, the OnError hook and content negotiation behave
// exactly as with net/http.
//
// # Aborts
//
// gin's aborts send the status at once. [Middleware] holds it back, so an
// abort still gets a complete problem: c.AbortWithError(403, err) answers
// with err resolved by the handler, using 403 only when no mapper
// recognizes err, and c.AbortWithStatus(401) answers with an about:blank
// 401 problem. Responses with a body, and statuses below 400, are sent as
// written.
//
// Register [Middleware] before middleware that aborts or writes: a header
// sent before it runs can no longer be replaced.
//
// # Route groups
//
// [Scope] attaches mappers to the routes of a group only, ahead of the
// handler's own mappers:
//
//	contracts := engine.Group("/v1/contracts", ginprblm.Scope(
//		prblm.Status(ErrLocked, http.StatusLocked),
//	))
//
// # Panic recovery
//
// Recovery is opt-in. [Recovery] turns a panic into the core's safe 500
// problem and hands the panic value and stack to the OnError hook. Use it
// instead of gin.Recovery(), before or after [Middleware], with the same
// options, so panics and errors share one hook and one set of enrichers:
//
//	opts := []prblm.Option{prblm.OnError(logProblem)}
//
//	engine := gin.New()
//	engine.Use(ginprblm.Recovery(opts...), ginprblm.Middleware(opts...))
//
// # Safe defaults
//
// The problem semantics are the core's, unchanged: an error no mapper
// recognizes becomes a 500 problem without its text, the instance is the
// request path without the query string, and the format is JSON unless the
// client prefers XML. Should the core itself panic while resolving an error
// (for example on an error whose Unwrap method panics), the response is a
// bare 500 without a body and the engine keeps serving.
//
// A configuration mistake, such as an invalid option, is a programming
// mistake: [Middleware] and [Recovery] panic when they are created, so it
// shows at startup and never inside a request.
package ginprblm
