package ginprblm

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
)

// Middleware returns gin middleware that writes the last error recorded with
// c.Error as a problem response once the handlers after it have returned.
//
// opts are the options of [prblm.NewHandler], which builds the problem
// handler once, here: no options give the core's safe defaults, and an
// invalid option panics at startup rather than inside a request. Pass
// [Recovery] the same options.
//
// While they run, the middleware holds back the status that gin's aborts
// would otherwise send at once, so the problem keeps full control of the
// status and Content-Type:
//
//   - c.AbortWithError(code, err) uses code as a status hint for err (see
//     [prblm.WithStatus]): a mapper or a self-describing error still takes
//     precedence, and a code below 400 is ignored.
//   - c.AbortWithStatus(code), or any status of 400 or above with no body
//     and no recorded error, becomes an about:blank problem for that status.
//   - Any other response, including statuses below 400 and every response
//     with a body, is sent as the handlers wrote it. A recorded error then
//     only reaches the [prblm.OnError] hook, because appending a problem
//     would corrupt the body.
//   - When the client went away (the request context is canceled and the
//     error wraps [context.Canceled]), nothing is written and the error only
//     reaches the hook.
//
// Register Middleware before middleware that aborts or writes: a header sent
// before it runs cannot be replaced, so the error then only reaches the hook.
// With problem types registered, also register it before middleware that
// sets a status (c.Status): that replaces the 404 gin records for a path no
// route claims, and discovery is then not served.
//
// When opts register problem types (see [prblm.WithTypes]), Middleware also
// serves their documentation, exactly as [prblm.Handler.Discovery] does,
// for GET and HEAD requests under the namespace path that no route claims
// for any method. It must then be installed with engine.Use: gin runs global
// middleware for unmatched paths, even with a NoRoute handler, but not group
// middleware. A route registered at a documentation path takes precedence,
// and so does a route for another method when engine.HandleMethodNotAllowed
// is on: the client gets gin's 405. gin routes HEAD on its own, so with
// only a GET route at a documentation path, a HEAD there finds no route and
// gets the documentation's headers.
//
// With discovery on, Middleware also reports every request a route answers
// to [prblm.Handler.NoteShadowedRoute], with the route's pattern
// (c.FullPath()), so the [prblm.OnError] hook receives a note wrapping
// [prblm.ErrShadowedDocumentation], once per route, when a route hides
// documentation. The response is unchanged.
func Middleware(opts ...prblm.Option) gin.HandlerFunc {
	h := prblm.NewHandler(opts...)
	// DiscoveryPath renders the documentation now, so a rendering failure
	// shows at startup.
	isDiscoveryOn := h.DiscoveryPath() != ""

	return func(c *gin.Context) {
		if isDiscoveryOn && discover(h, c) {
			return
		}

		w := newDeferredWriter(c.Writer)
		c.Writer = w
		// Even when a panic unwinds through here, middleware outside this
		// one must find gin's own writer, or its status is only recorded.
		defer func() { c.Writer = w.ResponseWriter }()

		c.Next()

		finish(h, c, w)
	}
}

// discover handles c for discovery and reports whether it answered it.
// When a route answers c (c.FullPath() is its pattern), the route is
// reported to the shadowed-documentation check and c goes on. Otherwise
// discovery is served only when no route claims the path for any method:
// gin recorded 404 before running the global middleware. With
// engine.HandleMethodNotAllowed on, a path that has a route only for
// another method also has an empty FullPath, but gin recorded 405, and that
// answer stays gin's.
func discover(h *prblm.Handler, c *gin.Context) bool {
	if route := c.FullPath(); route != "" {
		// Only the method and the path are read: the headers matter only
		// to a response.
		h.NoteShadowedRoute(c.Request.Context(), route, prblm.DiscoveryRequest{
			Method: c.Request.Method,
			Path:   c.Request.URL.EscapedPath(),
		})

		return false
	}

	return c.Writer.Status() == http.StatusNotFound && serveDiscovery(h, c)
}

// serveDiscovery answers c with the core's discovery response, exactly as
// net/http would, and stops the chain. It reports false, and does nothing,
// when the request is not for discovery. The status is committed at once:
// a 304 or a HEAD response has no body, so nothing else would send it.
func serveDiscovery(h *prblm.Handler, c *gin.Context) bool {
	base := requestOf(c.Request)

	res, ok := h.RespondDiscovery(c.Request.Context(), prblm.DiscoveryRequest{
		Method: base.Method,
		Path:   c.Request.URL.EscapedPath(),
		Accept: base.Accept,
		// Repeated lines form one list (RFC 9110, section 5.3).
		IfNoneMatch: strings.Join(c.Request.Header.Values("If-None-Match"), ","),
	})
	if !ok {
		return false
	}

	send(c.Writer, res)
	c.Writer.WriteHeaderNow()
	c.Abort()

	return true
}

// errStatusOnly is the error behind a problem for a status-only abort, such
// as c.AbortWithStatus(401). No mapper recognizes it, so the status hint
// makes it an about:blank problem, and the hook sees what happened.
var errStatusOnly = errors.New("ginprblm: response aborted with an error status and no error")

// finish decides, once the handlers have returned, what the response
// becomes. The cases are tried in order.
func finish(h *prblm.Handler, c *gin.Context, w *deferredWriter) {
	last := c.Errors.Last()
	recorded := w.Status()

	switch {
	case w.isCommitted || last != nil && isClientGone(c.Request, last.Err):
		// The body started, and appending a problem would corrupt it; or
		// the client went away, and nobody is left to read one. The error
		// only reaches the hook.
		if last != nil {
			_, _ = respond(h, c.Request, last.Err)
		}
	case last != nil:
		answer(h, c.Request, w, withHint(last.Err, recorded))
	case recorded >= http.StatusBadRequest:
		answer(h, c.Request, w, prblm.WithStatus(errStatusOnly, recorded))
	default:
		w.commitNow()
	}
}

// isClientGone reports whether err comes from the client going away: the
// request context is canceled and err wraps context.Canceled. A
// context.Canceled from elsewhere, with the client still connected, is
// answered like any other error.
func isClientGone(r *http.Request, err error) bool {
	return r.Context().Err() != nil && errors.Is(err, context.Canceled)
}

// withHint attaches an abort status to err as a hint: mappers and
// self-describing errors still take precedence, and a status below 400 is
// no hint at all.
func withHint(err error, status int) error {
	if status < http.StatusBadRequest {
		return err
	}

	return prblm.WithStatus(err, status)
}

// answer writes the problem response for err to w. When there is nothing
// to answer (a recorded error holding a nil error), the recorded response
// is committed unchanged.
func answer(h *prblm.Handler, r *http.Request, w *deferredWriter, err error) {
	res, ok := respond(h, r, err)
	if !ok {
		w.commitNow()

		return
	}

	send(w, res)
}

// respond resolves err into the problem response for r. The core contains
// panics in mappers, enrichers and the hook, but not in methods of the error
// itself, such as a panicking Unwrap: should Respond panic, the response is a
// bare 500 without a body, which cannot fail the same way, and the server
// keeps serving.
func respond(h *prblm.Handler, r *http.Request, err error) (res prblm.Response, ok bool) {
	defer func() {
		if recover() != nil {
			res, ok = prblm.Response{Status: http.StatusInternalServerError}, true
		}
	}()

	return h.Respond(r.Context(), requestOf(r), err)
}

// requestOf describes r for [prblm.Handler.Respond]. It uses the URL path, so
// the query string never reaches the problem.
func requestOf(r *http.Request) prblm.Request {
	return prblm.Request{
		Method: r.Method,
		Path:   r.URL.Path,
		// Repeated Accept lines form one list (RFC 9110, section 5.3).
		Accept: strings.Join(r.Header.Values("Accept"), ","),
	}
}

// send writes res to w: its headers merged into those already set, its
// status and its body.
func send(w http.ResponseWriter, res prblm.Response) {
	res.MergeInto(w.Header())
	w.WriteHeader(res.Status)
	// A failed write means the client is gone; there is nobody left to tell.
	_, _ = w.Write(res.Body)
}
