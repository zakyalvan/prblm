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
// c.Error as a problem response, resolved by h, once the handlers after it
// have returned.
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
//
// Middleware panics when h is nil.
func Middleware(h *prblm.Handler) gin.HandlerFunc {
	mustExist(h, "Middleware")

	return func(c *gin.Context) {
		w := newDeferredWriter(c.Writer)
		c.Writer = w
		// Even when a panic unwinds through here, middleware outside this
		// one must find gin's own writer, or its status is only recorded.
		defer func() { c.Writer = w.ResponseWriter }()

		c.Next()

		finish(h, c, w)
	}
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

// mustExist panics when h is nil, naming the constructor, so that a missing
// prblm.NewHandler call fails at startup rather than inside a request.
func mustExist(h *prblm.Handler, constructor string) {
	if h == nil {
		panic("ginprblm: " + constructor + " called with a nil problem handler; create it with prblm.NewHandler")
	}
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
