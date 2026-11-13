package fiberprblm

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/kartaladev/prblm"
)

// isDiscovery reports whether c, answered with err, is a request for type
// documentation: a GET or HEAD that reached fiber's route-not-found error,
// for the discovery path or a path under it. It is false when discovery is
// off (an empty path).
//
// fiber's router returns the sentinel fiber.ErrNotFound itself, the same
// value a handler may return, so the error alone cannot tell them apart;
// c.Matched() can, because it is set only when an endpoint route matched.
func isDiscovery(c fiber.Ctx, err error, path string) bool {
	if path == "" || c.Matched() || !errors.Is(err, fiber.ErrNotFound) {
		return false
	}

	method, urlPath := c.Method(), c.Path()
	isRead := method == http.MethodGet || method == http.MethodHead

	return isRead && (urlPath == path || strings.HasPrefix(urlPath, path+"/"))
}

// serveDiscovery answers c with the core discovery handler, exactly as
// net/http would. fiber has no http.ResponseWriter, so the handler writes
// into a recorder, which is then sent like a problem response. Every
// request header is passed on, and the URL is the original one, so the
// core sees the request as net/http would.
func serveDiscovery(c fiber.Ctx, discovery http.Handler) error {
	req, err := http.NewRequestWithContext(c.Context(), c.Method(), c.OriginalURL(), http.NoBody)
	if err != nil {
		// fiber accepts URIs net/http rejects, such as a malformed escape
		// ("%zz"). No documentation lives there, so the caller answers the
		// route-not-found error as usual.
		return err
	}

	for name, value := range c.Request().Header.All() {
		req.Header.Add(string(name), string(value))
	}

	rec := recorder{header: make(http.Header)}
	discovery.ServeHTTP(&rec, req)

	send(c, prblm.Response{Status: rec.status, Header: rec.header, Body: rec.body.Bytes()})

	return nil
}

// recorder is the minimal [http.ResponseWriter] the discovery handler
// writes into: it records the status, headers and body. Discovery pages are
// small and pre-rendered, so the body fits in memory.
type recorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

// Header returns the recorded headers.
func (r *recorder) Header() http.Header { return r.header }

// WriteHeader records the first status.
func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

// Write records b, after the implicit 200 when no status was written.
func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)

	return r.body.Write(b)
}
