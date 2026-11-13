package ginprblm

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
)

// Middleware returns gin middleware that writes the last error recorded with
// c.Error as a problem response, resolved by h, once the handlers after it
// have returned.
func Middleware(h *prblm.Handler) gin.HandlerFunc {
	mustExist(h, "Middleware")

	return func(c *gin.Context) {
		c.Next()

		last := c.Errors.Last()
		if last == nil {
			return
		}

		res, ok := respond(h, c.Request, last.Err)
		if !ok {
			return
		}

		send(c.Writer, res)
	}
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
