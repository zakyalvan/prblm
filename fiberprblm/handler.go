package fiberprblm

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
)

// ErrorHandler returns a fiber error handler, for [fiber.Config], that
// writes every error returned by a handler or middleware, and fiber's own
// routing errors, as a problem response.
//
// opts are the options of [prblm.NewHandler], which builds the problem
// handler once, here: no options give the core's safe defaults, and an
// invalid option panics at startup rather than inside a request.
//
// The code of a [*fiber.Error] in the chain is a status hint; its message is
// never sent. A buffered body is replaced by the problem, while a body stream
// is left alone, as is the response to a client that went away (the request
// context is canceled and the error wraps [context.Canceled]); the error then
// only reaches the [prblm.OnError] hook.
//
// When opts register problem types (see [prblm.WithTypes]), ErrorHandler
// also serves their documentation, exactly as [prblm.Handler.Discovery]
// does, for GET and HEAD requests under the namespace path that reach it as
// fiber's route-not-found error. A route registered at a documentation path
// takes precedence, and a handler that returns fiber.ErrNotFound gets a 404
// problem as usual. A path that has a route only for another method gets
// fiber's 405 as a problem, not the documentation.
func ErrorHandler(opts ...prblm.Option) fiber.ErrorHandler {
	h := prblm.NewHandler(opts...)
	// DiscoveryPath renders the documentation now, so a rendering failure
	// shows at startup.
	isDiscoveryOn := h.DiscoveryPath() != ""

	return func(c fiber.Ctx, err error) error {
		if isDiscoveryOn && isRouteNotFound(c, err) && serveDiscovery(h, c) {
			return nil
		}

		res, ok := respond(h, c, err)
		// Fiber sends nothing before the handler returns, so a buffered
		// body is replaced. A body stream is the handler's own response, and
		// a client that went away reads nothing; in both cases the error has
		// already reached the hook.
		if !ok || c.Response().IsBodyStream() || isClientGone(c, err) {
			return nil
		}

		send(c, res)

		return nil
	}
}

// respond resolves err into the problem response for c. The core contains
// panics in mappers, enrichers and the hook, but not in methods of the error
// itself, such as a panicking Unwrap met while walking its chain: then the
// response is a bare 500 without a body, which cannot fail the same way.
func respond(h *prblm.Handler, c fiber.Ctx, err error) (res prblm.Response, ok bool) {
	defer func() {
		if recover() != nil {
			res, ok = prblm.Response{Status: http.StatusInternalServerError}, true
		}
	}()

	return h.Respond(c.Context(), requestOf(c), withHint(err))
}

// isClientGone reports whether err comes from the client going away: the
// request context is canceled and err wraps context.Canceled. A
// context.Canceled from elsewhere is answered like any other error.
func isClientGone(c fiber.Ctx, err error) bool {
	return c.Context().Err() != nil && errors.Is(err, context.Canceled)
}

// withHint attaches the code of a [*fiber.Error] in the chain of err as a
// status hint. Only the code is used: the core never sends error text, so
// fiber's own messages ("Cannot GET /x") and handler messages stay private.
// Mappers and self-describing errors still take precedence, and a code
// outside 400–599 is ignored by the core.
func withHint(err error) error {
	// A typed-nil *fiber.Error carries no code.
	if fe, ok := errors.AsType[*fiber.Error](err); ok && fe != nil {
		return prblm.WithStatus(err, fe.Code)
	}

	return err
}

// requestOf describes the request of c for [prblm.Handler.Respond]. The
// path leaves out the query string, so it never reaches the problem.
func requestOf(c fiber.Ctx) prblm.Request {
	return prblm.Request{
		Method: c.Method(),
		Path:   c.Path(),
		// Repeated Accept lines form one list (RFC 9110, section 5.3).
		Accept: string(bytes.Join(c.Request().Header.PeekAll(fiber.HeaderAccept), []byte(","))),
	}
}

// send writes res to c: its headers merged into those already set, by the
// rules of [prblm.Response.MergeInto], its status and its body.
func send(c fiber.Ctx, res prblm.Response) {
	header := &c.Response().Header

	// Project the current values of the problem's header names, merge by
	// the core's rules, and write those names back.
	current := make(http.Header, len(res.Header))
	for name := range res.Header {
		for _, v := range header.PeekAll(name) {
			current.Add(name, string(v))
		}
	}

	res.MergeInto(current)

	for name, values := range current {
		header.Del(name)

		for _, v := range values {
			header.Add(name, v)
		}
	}

	c.Status(res.Status)
	// Send only fails for a body stream; this body is in memory.
	_ = c.Send(res.Body)
}
