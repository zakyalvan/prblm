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
status outside 400–599, or an error wrapping `ErrEncoding` for a problem that could not be
encoded. Tell them apart from the error being answered with `errors.Is` and `errors.As`.
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
and a `Cache-Control` the handler set is kept. Adapters for gin and fiber are planned as separate
modules.

## Defaults

| Behavior | Default | Why | Change it with |
|---|---|---|---|
| Unmapped errors | 500 with title "Internal Server Error" and no detail | never leak internals | consumer mappers |
| Built-in mappers | on | common request body errors get a correct 4xx without setup | `WithoutBuiltins()` |
| `type` | `about:blank`, omitted from the document | the RFC 9457 default | set per problem |
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
