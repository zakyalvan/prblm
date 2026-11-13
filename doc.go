// Package prblm renders Go errors as RFC 9457 problem details
// (application/problem+json and application/problem+xml) for net/http
// services.
//
// # Quick start
//
// Create a [Handler], register [Mapper] values that recognize your errors,
// and adapt handlers that return errors with [Handler.Wrap]:
//
//	var ErrVesselNotFound = errors.New("vessel not found")
//
//	h := prblm.NewHandler(prblm.WithMappers(
//		prblm.Status(ErrVesselNotFound, http.StatusNotFound),
//	))
//
//	mux.Handle("GET /v1/vessels/{id}", h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
//		vessel, err := loadVessel(r.Context(), r.PathValue("id"))
//		if err != nil {
//			return err // written as a problem response
//		}
//		return json.NewEncoder(w).Encode(vessel)
//	}))
//
// # Mapping errors
//
// [Handler.Resolve] tries these steps in order and stops at the first one that
// recognizes the error:
//
//  1. A [*Problem] or [Describer] in the error chain describes itself.
//  2. Mappers attached to the request with [ScopeMappers], for example by a
//     middleware on a group of routes.
//  3. The handler's own mappers, registered with [WithMappers], in order.
//     [Is], [As] and [Status] build mappers for sentinel and typed errors;
//     [MapperFunc] adapts any function.
//  4. Built-in mappers for malformed, truncated or oversized request bodies and
//     for deadline errors (see [WithoutBuiltins]).
//  5. A status hint attached with [WithStatus].
//  6. The safe fallback: a 500 problem without detail.
//
// A mapper returns nil for an error it does not recognize. Enrichers
// registered with [WithEnrichers] then complete every problem, and the
// [OnError] hook receives the original error, which is where to log it.
// Mappers, describers, enrichers and the hook may panic without breaking the
// response: the panic is reported to the hook as a [*PanicError].
//
// # Problem types
//
// [WithNamespace] gives the application one namespace for its problem types,
// so business code names only a sub-type, such as
// "contracts/blacklisted-supplier", which is resolved under the namespace
// when the problem is answered:
//
//	h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), mappers)
//	// a type "contracts/blacklisted-supplier" is sent as
//	// "/v1/api/probs/contracts/blacklisted-supplier"
//
// Absolute URIs, absolute paths and about:blank are kept. Any other relative
// type becomes about:blank, and the [OnError] hook receives a note wrapping
// [ErrInvalidType]. Types are never built from the request's Host header,
// which a client controls.
//
// [DefineType] defines a type once, with its status, title and description,
// and [Type.New] builds its problems. Registered with [WithTypes], the
// definitions are documented at their type URLs by [Handler.Discovery],
// mounted at [Handler.DiscoveryPath]: an HTML page, or JSON for a client
// that prefers it, and an index at the namespace path.
//
// # Safe defaults
//
// A handler created without options is safe to use as is:
//
//   - An error that no mapper recognizes becomes a 500 problem with title
//     "Internal Server Error" and no detail, so error text never reaches the
//     client.
//   - Request body errors from encoding/json and http.MaxBytesReader get a
//     4xx problem with a fixed, generic detail.
//   - The problem's instance is the request path. The query string is left
//     out, so tokens in it cannot leak.
//   - Responses carry "Vary: Accept" and, unless the handler or the problem
//     sets its own, "Cache-Control: no-store", so caches never replay an
//     error.
//   - A problem that cannot be encoded is replaced by the constant
//     {"title":"Internal Server Error","status":500}; nothing is sent in part.
//
// # Formats
//
// Problems are sent as application/problem+json unless the request's Accept
// header prefers XML (application/problem+xml, application/xml or text/xml),
// in which case they are sent as application/problem+xml, the XML format of
// RFC 9457 Appendix B (see [Problem.MarshalXML]). Q-values and specificity
// are honored, and JSON wins ties. A header that accepts neither format, or
// that is malformed or oversized, gets JSON with the problem's status rather
// than a 406: the client still learns what went wrong.
//
// Extension values have the same structure in both formats, because XML is
// produced from the JSON encoding: json struct tags apply to both. Extension
// names and object keys must be ASCII letters, digits and underscores
// starting with a letter for XML; a problem that breaks this rule, or holds a
// value JSON cannot encode, is replaced by the minimal 500 problem.
//
// # net/http
//
// [Handler.Wrap] adapts a function that returns an error. The result is an
// [http.Handler], so it works with [http.ServeMux] and with any router built
// on it, for example chi:
//
//	r := chi.NewRouter()
//	r.Method(http.MethodGet, "/v1/vessels/{id}", h.Wrap(getVessel))
//
// [Handler.Write] serves handlers that keep the standard signature:
//
//	func getVessel(w http.ResponseWriter, r *http.Request) {
//		vessel, err := loadVessel(r.Context(), r.PathValue("id"))
//		if err != nil {
//			h.Write(w, r, err)
//			return
//		}
//		_ = json.NewEncoder(w).Encode(vessel)
//	}
//
// Wrap hands the function a writer that records whether the response has
// started. When the function returns an error after writing a status, body,
// flush or hijack, the error goes to the [OnError] hook and nothing more is
// written, because appending a problem would corrupt the response. Write
// cannot know what was written before it was called and assumes nothing was,
// unless it receives the writer from Wrap.
//
// The writer from Wrap keeps the capabilities of the server's writer
// reachable through [http.ResponseController]: flushing, hijacking,
// deadlines and full duplex. It always implements [http.Flusher] and
// [http.Hijacker]; to find out whether the underlying connection supports
// them, use [http.ResponseController], which returns [http.ErrNotSupported]
// when it does not.
//
// # Panic recovery
//
// Recovery is opt-in, so it never competes with recovery middleware already
// in place. [Handler.Recover] turns a panic into the safe 500 problem,
// rendered like any other (enrichers, format, headers), and hands the panic
// value and stack to the [OnError] hook as a [*PanicError]: log them there.
//
//	mux.Handle("GET /v1/vessels/{id}", h.Recover(h.Wrap(getVessel)))
//
// A panic with [http.ErrAbortHandler] is passed on so net/http aborts the
// response. After the handler has started its response, or when the client
// connection is broken, nothing more is written and only the hook hears of
// the panic.
//
// # Framework adapters
//
// [Handler.Respond] produces a complete response (status, headers, body)
// without writing it. [Handler.Wrap] is the net/http adapter built on it;
// adapters for other frameworks reuse it so that every framework produces the
// same output.
package prblm
