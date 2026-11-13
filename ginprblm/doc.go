// Package ginprblm renders errors in gin handlers as RFC 9457 problem
// details, with the same output as the net/http adapter of
// [github.com/kartaladev/prblm].
//
// # Quick start
//
// Install [Middleware] with a [prblm.Handler] and record errors with
// c.Error, as gin handlers usually do:
//
//	h := prblm.NewHandler(prblm.WithMappers(
//		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
//	))
//
//	engine := gin.New()
//	engine.Use(ginprblm.Middleware(h))
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
// recorded error with the handler, so mappers, enrichers, the OnError hook
// and content negotiation behave exactly as with net/http.
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
// A nil problem handler is a programming mistake: [Middleware] panics when it
// is created, so the mistake shows at startup.
package ginprblm
