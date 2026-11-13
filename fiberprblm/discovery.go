package fiberprblm

import (
	"bytes"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
)

// Discovery returns fiber middleware, for app.Use, that reports every
// request an endpoint route answers to [prblm.Handler.NoteShadowedRoute],
// so the [prblm.OnError] hook learns when a route hides type
// documentation: such a page cannot be reached, because [ErrorHandler]
// only serves documentation for paths no route claims, and it never sees a
// request a route answers. The note wraps [prblm.ErrShadowedDocumentation]
// and names the route's pattern (c.Route().Path), once per route.
//
// opts are the options of [prblm.NewHandler]; pass the error handler's
// options, so the middleware knows the same types and reports to the same
// hook. An invalid option panics at startup. With discovery off, nothing is
// reported. The middleware never renders the documentation, so it adds no
// second copy of the pages [ErrorHandler] serves.
//
// Discovery is opt-in, since only the hook learns anything from it. It
// changes no response and returns the error of the handlers after it
// unchanged, so serving documentation stays with [ErrorHandler]. A panic
// below it is left to [Recovery], and that request is not reported; the
// route is noted at the next request it answers.
func Discovery(opts ...prblm.Option) fiber.Handler {
	h := prblm.NewHandler(opts...)

	return func(c fiber.Ctx) error {
		err := c.Next()

		// Set only when an endpoint route, not app.Use middleware, matched,
		// and c.Route() is then that route.
		if c.Matched() {
			h.NoteShadowedRoute(c.Context(), c.Route().Path, prblm.DiscoveryRequest{
				Method: c.Method(),
				Path:   escapedPath(c),
			})
		}

		return err
	}
}

// escapedPath returns the path of the request c answers as the client sent
// it, still escaped and without the query string, copied out of fiber's
// buffers.
func escapedPath(c fiber.Ctx) string {
	return string(c.Request().URI().PathOriginal())
}

// isRouteNotFound reports whether err, answering c, is fiber's own
// route-not-found error. fiber's router returns the sentinel
// fiber.ErrNotFound itself, the same value a handler may return, so the
// error alone cannot tell them apart; c.Matched() can, because it is set
// only when an endpoint route matched. A path that has a route only for
// another method gets fiber.ErrMethodNotAllowed instead, so it is not one.
func isRouteNotFound(c fiber.Ctx, err error) bool {
	return !c.Matched() && errors.Is(err, fiber.ErrNotFound)
}

// serveDiscovery answers c with the core's discovery response, exactly as
// net/http would. It reports false, and does nothing, when the request is
// not for discovery. The path is the raw request path, still escaped, so a
// malformed escape such as "%zz" needs no special case: under the discovery
// path it names no type, and gets the discovery 404.
func serveDiscovery(h *prblm.Handler, c fiber.Ctx) bool {
	header := &c.Request().Header

	res, ok := h.RespondDiscovery(c.Context(), prblm.DiscoveryRequest{
		Method: c.Method(),
		Path:   escapedPath(c),
		Accept: requestOf(c).Accept,
		// Repeated lines form one list (RFC 9110, section 5.3).
		IfNoneMatch: string(bytes.Join(header.PeekAll(fiber.HeaderIfNoneMatch), []byte(","))),
	})
	if !ok {
		return false
	}

	send(c, res)

	return true
}
