# prblm

[![Go Reference](https://pkg.go.dev/badge/github.com/kartaladev/prblm.svg)](https://pkg.go.dev/github.com/kartaladev/prblm)

prblm renders Go errors as [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details
(`application/problem+json` and `application/problem+xml`) for net/http services. With no
configuration it answers safely: an error it does not recognize becomes a 500 with no detail, so
error text, stack traces and database internals never reach the client.

The core module uses the standard library only. Adapters for other frameworks reuse the same
framework-neutral response step, so every framework produces the same output.

## Install

```sh
go get github.com/kartaladev/prblm
```

Requires Go 1.27.

## Quick start

Create a `Handler`, register mappers for the errors you know, and adapt handlers that return
errors with `Wrap`. The status, `Content-Type` and body below are the example's real output.

```go
errNotFound := errors.New("vessel not found")

h := prblm.NewHandler(prblm.WithMappers(
	prblm.Status(errNotFound, http.StatusNotFound),
))

handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
	return fmt.Errorf("load vessel 7: %w", errNotFound)
})
```

```http
HTTP/1.1 404 Not Found
Content-Type: application/problem+json
Cache-Control: no-store
Vary: Accept

{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}
```

`Wrap` returns an `http.Handler`, so it works with `http.ServeMux` and any router built on it.
When the handler has already started its response (wrote a status or body, copied a body with
`io.Copy`, flushed, or hijacked the connection), an error it returns afterwards is only reported,
never appended. `io.Copy` still reaches the underlying writer's `ReadFrom`, so net/http keeps
sending files with sendfile. *(Mirrors `ExampleHandler_Wrap`.)*

### Handlers with the standard signature

`Write` answers an error from a plain `http.HandlerFunc`. *(Mirrors `ExampleHandler_Write`.)*

```go
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != "1" {
		h.Write(w, r, errNotFound)

		return
	}

	_, _ = w.Write([]byte(`{"id":1}`))
})
```

## Mapping errors

`Resolve` tries these steps in order, and the first that recognizes the error wins:

1. a `*Problem` or `Describer` in the error chain describes itself
2. mappers scoped to the request with `ScopeMappers`, newest scope first
3. the handler's own mappers (`WithMappers`), in registration order
4. built-in mappers for malformed, truncated or oversized request bodies and for deadline errors
   (`WithoutBuiltins` turns them off)
5. a status hint attached with `WithStatus`
6. the safe fallback: a 500 with no detail

A mapper returns `nil` for an error it does not recognize. `Is`, `As` and `Status` build mappers
for sentinel and typed errors. *(Mirrors `ExampleIs` and `ExampleAs`.)*

```go
h := prblm.NewHandler(prblm.WithMappers(
	prblm.Is(errNotFound, func(error) *prblm.Problem {
		return prblm.New(http.StatusNotFound, prblm.WithDetail("No vessel has that ID."))
	}),
))
```

```go
h := prblm.NewHandler(prblm.WithMappers(
	prblm.As(func(e *RefusedError) *prblm.Problem {
		return prblm.New(http.StatusConflict, prblm.WithExt("reasons", e.Reasons))
	}),
))
```

When no mapper knows an error but the caller knows its status, attach a hint. The error text still
never reaches the client. *(Mirrors `ExampleWithStatus`.)*

```go
// No mapper knows this error, but the caller knows it means "forbidden".
err := prblm.WithStatus(errors.New("tenant 42 may not read vessel 7"), http.StatusForbidden)
```

Mappers that only apply to some routes can be attached by a middleware. *(Mirrors
`ExampleScopeMappers`.)*

```go
// A middleware on the contract routes maps errLocked for them only.
contracts := func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := prblm.ScopeMappers(r.Context(), prblm.Status(errLocked, http.StatusLocked))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

Extension members are validated when they are set: names are ASCII letters, digits and
underscores, start with a letter, and are at least three characters long. *(Mirrors
`ExampleProblem_Set`.)*

```go
p := prblm.New(http.StatusUnprocessableEntity)

fmt.Println(p.Set("trace_id", "abc"))
fmt.Println(p.Set("trace-id", "abc"))
fmt.Println(p.Set("status", 500))
```

## Problem types

Give the application one namespace for its problem types with `WithNamespace`, and let business
code name only a sub-type, such as `contracts/blacklisted-supplier`. When the problem is answered,
the sub-type is resolved under the namespace, so every team's types share one root.
*(Mirrors `ExampleWithNamespace`.)*

```go
blacklisted := prblm.WithMappers(prblm.Is(errBlacklisted, func(error) *prblm.Problem {
	return prblm.New(http.StatusConflict, prblm.WithType("contracts/blacklisted-supplier"))
}))

h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), blacklisted)
// type: "/v1/api/probs/contracts/blacklisted-supplier"

h = prblm.NewHandler(prblm.WithNamespace("https://kartala.id/probs"), blacklisted)
// type: "https://kartala.id/probs/contracts/blacklisted-supplier"
```

- **The namespace** is an absolute `http`/`https` URI with a host, or an absolute path. It needs a
  path other than `/`, and no query, fragment, user info, `.` or `..` segment. An invalid namespace
  panics in `NewHandler`, because it is a startup misconfiguration.
- **A sub-type** is one or more `/`-separated segments of lowercase letters and digits, with single
  `-`, `_` or `.` between them.
- **Kept as given:** an empty type (`about:blank`), `about:blank` itself, an absolute URI such as
  `https://…` or `urn:…`, and an absolute path.
- **Any other relative type** is a mistake, and a client would resolve it against the request URL.
  It becomes `about:blank`, keeping the status, title and detail, and the `OnError` hook receives
  a note wrapping `ErrInvalidType`.
- **A path namespace** gives a path-only `type`, which the client resolves against the URL it
  requested. The library never builds a type from the request's `Host` or `X-Forwarded-*` headers,
  which a client controls. Use an absolute namespace when types must be the same on every host,
  for example across staging and production.
- **Without a namespace**, types are written exactly as given.

## Discovering problem types

RFC 9457 recommends that a problem's type URL lead to documentation people can read. Define each
type once with `DefineType`, register the definitions with `WithTypes`, and the handler serves their
documentation. *(Mirrors `ExampleDefineType` and `ExampleHandler_Discovery`.)*

```go
var OutOfCredit = prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit",
	"The account balance is below the price of the request.")

h := prblm.NewHandler(
	prblm.WithNamespace("/v1/api/probs"),
	prblm.WithTypes(OutOfCredit),
	prblm.WithMappers(prblm.Is(ErrOutOfCredit, func(error) *prblm.Problem {
		return OutOfCredit.New() // type, status and title come from the definition
	})),
)

mux := http.NewServeMux()
if path := h.DiscoveryPath(); path != "" {
	mux.Handle("GET "+path+"/", h.Discovery())
}
```

- **What is served:**
  - each type's documentation at its type URL, such as `/v1/api/probs/out-of-credit`;
  - an index of all types, sorted by sub-type, at the namespace path;
  - a 404 problem for any other path under it (the hook is not called, since no application
    error happened);
  - a 405 problem with `Allow: GET, HEAD` for any other method.
- **Format:** a complete HTML page by default, or JSON (`type`, `subtype`, `status`, `title`,
  `description`) for a client that prefers `application/json`.
- **Safety:** the pages show only the text written into `DefineType`, never anything from errors
  or requests. HTML is escaped by `html/template` and sent with `X-Content-Type-Options: nosniff`
  and `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'`.
- **Caching:** the pages are rendered once, in `NewHandler`, and are cacheable by browsers
  (`Cache-Control: public, max-age=3600` and a strong `ETag`; a matching `If-None-Match` gets
  304). Definitions only change with a redeploy, so a stale page lasts at most an hour.
- **Undocumented types:** with discovery on, a problem whose sub-type has no definition is still
  sent, and the `OnError` hook receives a note wrapping `ErrUndocumentedType`, because its type URL
  leads to a 404.
- **Opting out:** `WithoutDiscovery()` keeps the definitions but serves nothing.
- **An absolute namespace** (`https://kartala.id/probs`) is served at its path (`/probs`): routing
  that host to the application is up to the operator. Types are never built from the request's
  `Host` header, because a client controls it; a path namespace gives a path-only type that the
  client resolves against the URL it requested.

## Logging

The library never logs. The `OnError` hook receives every answered error with the problem being
sent, and is the place to log. *(Mirrors `ExampleOnError`.)*

```go
h := prblm.NewHandler(prblm.OnError(func(_ context.Context, err error, p *prblm.Problem) {
	// Log here; the client never sees err.
	fmt.Printf("answered %d for: %v\n", p.Status, err)
}))
```

Before that call, the hook may also receive notes about mistakes the handler worked around: a
`*PanicError` from consumer code that panicked, an error wrapping `ErrInvalidStatus` for a mapper
status outside 400–599, an error wrapping `ErrInvalidType` for a relative type that is not a valid
sub-type (see [Problem types](#problem-types)), an error wrapping `ErrUndocumentedType` for a
sub-type without a registered definition (see [Discovering problem types](#discovering-problem-types)),
or an error wrapping `ErrEncoding` for a problem that could not be encoded. Tell them apart from the error being answered with `errors.Is` and `errors.As`.
Mappers, enrichers and the hook may panic without breaking the response.

## Panic recovery

Recovery is opt-in, so it never competes with recovery middleware you already have. `Recover`
turns a panic into the safe 500, rendered like any other problem, and hands the panic value and
stack to the hook as a `*PanicError`. *(Mirrors `ExampleHandler_Recover`.)*

```go
h := prblm.NewHandler(prblm.OnError(func(ctx context.Context, err error, _ *prblm.Problem) {
	if pe, ok := errors.AsType[*prblm.PanicError](err); ok {
		// pe.Stack holds the stack at the panic; log it in production.
		logger.ErrorContext(ctx, "handler panicked", "value", pe.Value)

		return
	}

	logger.ErrorContext(ctx, "request failed", "err", err)
}))
```

A panic with `http.ErrAbortHandler` is passed on so net/http aborts the response. After the
handler has started its response, or when the client connection is broken, nothing more is
written and only the hook hears of the panic.

## Formats

Problems are sent as JSON unless the `Accept` header prefers XML (`application/problem+xml`,
`application/xml` or `text/xml`). Q-values and specificity are honored, and JSON wins ties. An
`Accept` header that allows neither format, or that is malformed or oversized, gets JSON with the
problem's status, never a 406. XML follows RFC 9457 Appendix B and has the same structure as the
JSON, because it is produced from it: `json` struct tags apply to both. *(Mirrors
`ExampleProblem_MarshalXML`.)*

```go
p := prblm.New(http.StatusForbidden,
	prblm.WithType("https://example.com/probs/out-of-credit"),
	prblm.WithTitle("You do not have enough credit."),
	prblm.WithExt("balance", 30),
	prblm.WithExt("accounts", []string{"/account/12345", "/account/67890"}),
)

body, err := xml.Marshal(p)
```

## Clients and tests

`Parse` decodes a problem from an `*http.Response` in either format. It reports `ErrNotProblem`
for any other response, reads at most 1 MiB and always closes the body. *(Mirrors
`ExampleParse`.)*

```go
p, err := prblm.Parse(res) // closes the body
if errors.Is(err, prblm.ErrNotProblem) {
	fmt.Println("not a problem response")

	return
}
```

## Other frameworks

`Handler.Respond` turns a request description and an error into a complete response (status,
headers, body) without writing it. Framework adapters copy that response to their own writer, so
every framework produces the same output. `Response.MergeInto` merges the problem headers into
the headers already set on the framework's response by the same rules as the net/http adapter:
problem headers replace existing ones, `Vary` keeps the existing field names and adds `Accept`,
and a `Cache-Control` the handler set is kept.

### gin

```sh
go get github.com/kartaladev/prblm/ginprblm
```

`ginprblm.Middleware` turns the last error recorded with `c.Error` into a problem, once the
handlers after it return. It takes the same options as `prblm.NewHandler` and builds its problem
handler from them; with none it gives the safe defaults (mirrored by `ExampleMiddleware`):

```go
engine := gin.New()
engine.Use(ginprblm.Middleware(prblm.WithMappers(
	prblm.Status(ErrVesselNotFound, http.StatusNotFound),
)))
engine.GET("/v1/vessels/:id", func(c *gin.Context) {
	_ = c.Error(ErrVesselNotFound) // written as a 404 problem
})
```

Aborts keep working, and the problem still sets the status and `Content-Type`, because the
middleware holds back the header gin's aborts would send at once (mirrored by
`ExampleMiddleware_abort`):

```go
engine.GET("/v1/vessels/:id", func(c *gin.Context) {
	// The error's text is never sent; 403 is used as the status.
	_ = c.AbortWithError(http.StatusForbidden, errors.New("token abc expired"))
})
engine.GET("/v1/reports", func(c *gin.Context) {
	c.AbortWithStatus(http.StatusUnauthorized) // an about:blank 401 problem
})
```

- `c.AbortWithError(403, err)`: the abort status is a hint, used only when no mapper or
  self-describing error recognizes `err`. A status below 400 is no hint.
- `c.AbortWithStatus(401)`, or a status of 400 or above with no body: an `about:blank` problem
  for that status.
- Statuses below 400, responses with a body (`c.JSON`, `c.AbortWithStatusJSON`), streams
  (flushed responses, server-sent events) and hijacked connections (websockets) are sent
  unchanged; a recorded error then only reaches the `OnError` hook.

**Registration order:** register `Middleware` before any middleware that aborts or writes.
Middleware that runs earlier and sends the header first leaves nothing to replace: the error then
only reaches the hook.

**Recovery and route groups:** `ginprblm.Recovery(opts...)` turns panics into the safe 500 problem
and gives the panic value and stack to the `OnError` hook. Use it instead of `gin.Recovery()`,
before or after `Middleware`, and pass both the same options so they share the hook, enrichers and
namespace. `ginprblm.Scope(mappers...)` attaches mappers to a route group only
(mirrored by `ExampleRecovery` and `ExampleScope`):

```go
opts := []prblm.Option{prblm.OnError(logProblem)}

engine := gin.New()
engine.Use(ginprblm.Recovery(opts...), ginprblm.Middleware(opts...))
engine.Group("/v1/contracts", ginprblm.Scope(prblm.Status(ErrLocked, http.StatusLocked))).
	GET("/:id", func(c *gin.Context) { _ = c.Error(ErrLocked) }) // a 423 problem
```

### fiber

```sh
go get github.com/kartaladev/prblm/fiberprblm
```

`fiberprblm.ErrorHandler` is the application's error handler: every error a handler returns, and
fiber's own 404 and 405, become problems. It takes the same options as `prblm.NewHandler`
(mirrored by `ExampleErrorHandler`):

```go
app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.WithMappers(
	prblm.Status(ErrVesselNotFound, http.StatusNotFound),
))})
app.Get("/v1/vessels/:id", func(fiber.Ctx) error {
	return ErrVesselNotFound // written as a 404 problem
})
```

The code of a `*fiber.Error` is a status hint, used only when no mapper or self-describing error
recognizes the error; its message (`Cannot GET /x`, or `fiber.NewError(403, "token abc
expired")`) is never sent.

**Recovery and route groups:** `fiberprblm.Recovery()` turns a panic into an error returned to the
error handler; pair it with `ErrorHandler` to get the safe 500 problem and the panic value and
stack in the `OnError` hook. With fiber's default error handler, the client gets a plain-text 500
`Internal Server Error`, and the panic value never leaks either. `fiberprblm.Scope(mappers...)`
attaches mappers to a route group only (mirrored by `ExampleRecovery` and `ExampleScope`):

```go
app := fiber.New(fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.OnError(logProblem))})
app.Use(fiberprblm.Recovery())
app.Group("/v1/contracts", fiberprblm.Scope(prblm.Status(ErrLocked, http.StatusLocked))).
	Get("/:id", func(fiber.Ctx) error { return ErrLocked }) // a 423 problem
```

### Framework differences

For the same handler and request, net/http, gin and fiber send the same status, `Content-Type`,
`Vary` and body bytes; a parity test in `internal/paritytest` checks it. Where the frameworks
themselves differ:

| Situation | net/http | gin | fiber |
|---|---|---|---|
| How an error reaches the library | returned from `Wrap`, or `Write` | `c.Error`, `c.AbortWithError` | returned from a handler |
| Framework status codes | — | abort status is a hint | `*fiber.Error` code is a hint |
| Body written before the error | kept; error only to the hook | kept; error only to the hook | buffered body **replaced** by the problem (nothing was sent yet); a stream is kept |
| Streams and hijacked connections | kept; error only to the hook | kept; error only to the hook | a stream is kept; error only to the hook |
| Client went away (`context.Canceled`) | answered; the write is dropped | nothing written; error only to the hook | nothing written; error only to the hook |
| Panic recovery (opt-in) | `Handler.Recover` | `ginprblm.Recovery(opts...)` | `fiberprblm.Recovery()` with `ErrorHandler` |
| Ordering rule | — | `Middleware` before aborting middleware | — |

## Defaults

| Behavior | Default | Why | Change it with |
|---|---|---|---|
| Unmapped errors | 500 with title "Internal Server Error" and no detail | never leak internals | consumer mappers |
| Built-in mappers | on | common request body errors get a correct 4xx without setup | `WithoutBuiltins()` |
| `type` | `about:blank`, omitted from the document | the RFC 9457 default | set per problem |
| Type namespace | none: types written as given | no change for code that writes full type URIs | `WithNamespace` |
| Invalid relative type (with a namespace) | `about:blank`, and a note to the hook | a broken link is worse than none | — |
| Types from the request host | never | the `Host` header is client-controlled | — |
| Type documentation | served when types are registered, off otherwise | registering types is the intent to document them | `WithTypes`, `WithoutDiscovery()` |
| Documentation format | HTML, JSON when the client prefers it | RFC 9457 asks for docs people can read | `Accept: application/json` |
| Documentation caching | `public, max-age=3600` with a strong `ETag` | definitions change only with a redeploy | — |
| `instance` | the request path, without the query string | identify the occurrence without leaking query tokens | an enricher, or set per problem |
| Format | JSON, XML when the client prefers it | most clients read JSON | — |
| `Cache-Control` | `no-store`, unless the handler or the problem sets one | caches must not replay an error | set the header |
| `Vary` | `Accept` added, existing values kept | the format depends on `Accept` | — |
| Encoding failure | the constant `{"title":"Internal Server Error","status":500}` | never send a partial document | — |
| Panic recovery | off | never surprise existing recovery middleware | `Handler.Recover` |
| Logging | none | the library must not choose a logger | `OnError` |

## RFC 9457 compliance notes

- Members are written in a fixed order (type, title, status, detail, instance), followed by
  extension members at the top level, sorted by name. The output is deterministic.
- A missing `type` means `about:blank`, and an `about:blank` problem without a title gets the HTTP
  reason phrase of its status, as RFC 9457 recommends.
- The response status always equals the `status` member. A mapper status outside 400–599 is
  replaced by 500 and reported to the hook.
- Extension names follow the RFC's recommendation (ASCII letters, digits and underscores,
  starting with a letter, at least three characters), so every problem can also be encoded as
  XML. Standard member names cannot be used as extensions.
- Decoding is lenient, as the RFC asks of consumers: unknown members are kept as extensions, and
  standard members with the wrong type are ignored.
- The XML format follows Appendix B: a `problem` element in the `urn:ietf:rfc:7807` namespace,
  `<i>` elements for array items and empty elements for `null`.

## License

prblm is released under the [MIT License](LICENSE).
