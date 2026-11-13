# Design

## Context

The repository has no code yet, so this change creates the root module. Motivation is in proposal.md (Why), and behavior is in `specs/`. Two later changes depend on the API defined here:
- `add-framework-adapters`: gin and fiber v3
- `add-mapper-plugins`: sqlmap, pgxmap, gormmap and validatormap

So the boundary between the core and the adapters (D5) is the most important decision in this design.

Constraints:
- module `github.com/kartaladev/prblm`, Go 1.27
- standard library only
- the project rules in CLAUDE.md: TDD, Murphy's Law, Gall's Law, safe defaults

## Goals / Non-Goals

**Goals:**
- A small, idiomatic API: errors as values, an interface plus func adapter for mappers, functional options, `nil` meaning "no match".
- One framework-neutral response step that every adapter reuses, so output is identical across frameworks.
- A deterministic encoding, so tests can compare bytes.

**Non-Goals:**
- Media types other than problem JSON and problem XML.
- Logging, metrics or tracing (only the hook and enrichers).
- Localizing `title` (consumers can do it in mappers or enrichers).
- Any framework other than net/http in this change.

## Slices

Each slice ends with a working, tested, releasable library. Later slices only add to it.

1. **Sentinel error to a JSON problem over net/http.**
   - At its end: `NewHandler(WithMappers(Status(ErrX, 404)))` plus `Wrap(fn)` returns `application/problem+json` with the standard members. Anything unmapped returns the safe 500.
   - Scenarios: stdlib-adapter / Handler result (rows 1, 2, 4); error-mapping / Match or no match, Safe fallback; problem-document / Members serialized to JSON.
   - Hot paths introduced: `Resolve` (consumer mappers and fallback), JSON encoding, `Respond`, `Wrap`.
2. **Complete problem model.**
   - At its end: extensions with name validation, `about:blank` and title defaults, `Error`/`Unwrap`, problem headers, deterministic JSON, lenient JSON decoding.
   - Scenarios: all problem-document requirements except the XML and Parse ones.
   - Hot paths: finalization, extension encoding.
3. **Complete resolution chain.**
   - At its end:
     - `Is`, `As[T]`, self-describing errors, status hint, scoped mappers, built-ins
     - result normalization, copy-on-resolve
     - enrichers (default instance), `OnError`, faulty-consumer-code containment
     - nil errors; canceled requests answered like any other
   - Scenarios: all error-mapping requirements.
   - Hot paths: the full `Resolve` order, enricher loop, hook call, panic containment.
4. **Hardened response writing.**
   - At its end: `Vary`, `Cache-Control`, problem headers and protected headers, already-started detection through the tracking writer, `ResponseController` pass-through, encoding-failure fallback, direct `Write`, early misuse panics.
   - Scenarios: problem-response (except negotiation), stdlib-adapter (all).
   - Hot paths: header merge, tracking writer, fallback body.
5. **XML and negotiation.**
   - At its end: unexported JSON and XML formats, XML encoding through the JSON tree, `Accept` negotiation with limits.
   - Scenarios: problem-document XML requirements; problem-response negotiation and fallback requirements.
   - Hot paths: `negotiate.Select`, XML walker.
6. **Panic recovery for net/http.**
   - At its end: `Handler.Recover` and `PanicError`.
   - Scenarios: all panic-recovery requirements.
   - Hot paths: the recover path, second-level recover.
7. **Client-side parsing.**
   - At its end: `UnmarshalXML` and `Parse(*http.Response)`.
   - Scenarios: problem-document / Decode XML, Parse response.
   - Hot paths: decoding both formats.

## Decisions

### D1. Module layout

```
go.mod                       github.com/kartaladev/prblm (go 1.27)
problem.go, problem_json.go  Problem, encoding (JSON)
problem_xml.go               XML encoding/decoding
mapper.go                    Mapper, MapperFunc, Is, As, Status, Describer
handler.go, options.go       Handler, NewHandler, Option, Resolve
respond.go                   Request, Response, Respond, encoders
status.go, scope.go          WithStatus, ScopeMappers, RequestFrom
http.go                      Wrap, Write, Recover, trackingWriter
panic.go                     PanicError
parse.go                     Parse
internal/negotiate           Accept parsing and selection
internal/xmlenc              JSON tree to Appendix B XML
go.work                      lists "." now; later changes add their modules
```

- *Alternative:* sub-packages for the mapping and the encoding. Rejected: the public surface is small, and one package keeps the import path short for consumers.
- **Failure modes:**
  - The package grows too large. → Files stay focused per concept, and `internal/` holds the non-API algorithms.

### D2. Problem type

```go
type Problem struct {
    Type, Title, Detail, Instance string
    Status int
    Header http.Header      // response headers; never serialized
    ext    map[string]any   // set via Set; names validated
    cause  error
}
func New(status int, opts ...Field) *Problem // fields: WithType, WithTitle, WithDetail, WithExt, WithHeader, WithCause
func (p *Problem) Set(name string, v any) error
func (p *Problem) Ext(name string) (any, bool)
func (p *Problem) Error() string    // "Title: Detail" | "Title" | status text; nil-safe
func (p *Problem) Unwrap() error
```

- `New` applies fields in order (a later field replaces an earlier one) and ignores nil fields, so a field chosen conditionally can be left nil.
- Extensions are unexported, so the name check can't be bypassed. A `WithExt` field option for package-level literals panics on an invalid name at construction time, never at request time.
- `MarshalJSON` writes members in the order type, title, status, detail, instance, then the extensions sorted by name. It uses a `bytes.Buffer`, with `json.Marshal` per value.
- *Alternative:* an exported `Extensions map[string]any`. Rejected, because invalid names would only fail at encode time.
- **Failure modes:**
  - **Nil receiver:** `Error()` and the getters check for nil.
  - **Unencodable value:** `MarshalJSON` returns the error, and D7 falls back.
  - **Concurrent mutation of a shared problem:** prevented by copy-on-resolve (D4).
  - **Map aliasing between copies:** `clone()` copies the `ext` map and `Header` (shallow values).

### D3. Mapper contract

```go
type Mapper interface{ MapError(ctx context.Context, err error) *Problem } // nil = no match
type MapperFunc func(ctx context.Context, err error) *Problem
func Is(target error, fn func(err error) *Problem) Mapper
func As[T error](fn func(t T) *Problem) Mapper   // errors.AsType[T]
func Status(target error, status int) Mapper
type Describer interface{ Problem() *Problem }
```

- *Alternative:* a `(Problem, bool)` return. Rejected: a nil pointer is the common Go idiom for "nothing".
- **Failure modes:**
  - **Nil mapper in the list:** a nil `Mapper` or a nil `MapperFunc` is skipped when resolving, in one place (`isNilMapper` in `firstMatch`) that covers consumer, scoped and built-in mappers. Calling a nil `MapperFunc` would panic and send a panic note to the hook on every request. Other typed-nil mappers are called, since a method on a nil pointer may be valid, and a panic there is contained like any other (no `reflect`).
  - **Mapper panics:** contained (D4).
  - **`Is` with a nil target:** `Is` panics at construction with a clear message.

### D4. Handler and resolution

```go
type Handler struct{ mappers, enrichers []…; onError func(context.Context, error, *Problem); builtins []Mapper }
func NewHandler(opts ...Option) *Handler   // immutable after return; nil options ignored
func (h *Handler) Resolve(ctx context.Context, err error) *Problem   // nil for nil err
```

Resolution order:

```
err == nil (after unwrapping a status hint) --> nil
1. describer / *Problem: the outermost one in the chain (errors.As walks outer to inner)
2. ScopeMappers(ctx), newest scope first
3. consumer mappers, in registration order
4. built-ins (unless WithoutBuiltins)
5. status hint, if 400..599
6. fallback 500
then: clone --> normalize the status (outside 400..599 becomes 500 + hook note)
      --> finalize the title --> enrichers (default instance first) --> problem
```

- Hook notes wrap exported sentinels so hooks can tell them apart with `errors.Is`: `ErrInvalidStatus` for a mapper status outside 400–599, `ErrEncoding` for a problem that could not be encoded. A panic note stays a `*PanicError`.
- Each consumer call (describer, mapper, enricher, hook) runs through `safeCall`, which recovers a panic into a `*PanicError` that is reported to the hook. A panicking hook is recovered and dropped.
- **Failure modes:**
  - Panics in consumer code: contained, as above.
  - Concurrent requests: `Handler` is immutable, and the problem is cloned per request.
  - A huge mapper list: linear, documented. This is not a hot-path concern at realistic sizes.
  - A mapper returning the same pointer for every request: cloned.

### D5. Framework-neutral seam

```go
type Request struct{ Method, Path, Accept string }
type Response struct{ Status int; Header http.Header; Body []byte }
func (h *Handler) Respond(ctx context.Context, req Request, err error) (Response, bool)
func RequestFrom(ctx context.Context) (Request, bool)
```

`Respond` does the whole job and reports `false` only when there is nothing to write, which is a nil error. A canceled request context is answered like any other: a client still connected gets the problem, and a write to a client that went away is dropped by the server.
- puts `req` into `ctx`
- calls `Resolve` and then the hook
- negotiates the format
- encodes into a buffer, falling back to the minimal 500 if that fails
- builds the headers

Adapters only check whether their writer has started, and copy the `Response` into it. When it has started, the hook has still been called, so the "report but don't write" rule holds in every adapter.

- *Alternative:* adapters calling `Resolve` and encoding themselves. Rejected, because the negotiation and fallback logic would be duplicated per framework, and the outputs would drift.
- **Failure modes:**
  - **The adapter can't tell whether the response started:** documented per adapter. The net/http adapter uses the tracking writer (D8).
  - **Respond is called with a zero `Request`:** the instance falls back to empty, and `Accept` is treated as missing.

### D6. Status hint and scope

- `WithStatus(err, code)` returns `nil` for a nil `err`. Otherwise it returns an unexported `statusError{err, code}` with `Unwrap`.
- The hint is read with `errors.AsType[*statusError]`, which finds the outermost hint, so the outermost one wins.
- `ScopeMappers(ctx, m...)` stores an immutable slice in the context. Nesting builds a new slice with the newer mappers first, so a parent context is never mutated.
- **Failure modes:**
  - **Hints out of range:** ignored at step 5.
  - **Concurrent nested scopes:** they don't share backing arrays, because every scope allocates a fresh slice.

### D7. Encoding and fallback

- Formats are unexported: each has a media type, the media types an `Accept` header may name to select it, and an encode function. JSON and XML are built in, and negotiation matches formats by media type. A public `Encoder` interface with `WithEncoders` is postponed (user decision, slice 5): no scenario needs it yet, and an exported API is a commitment.
- **Encoding is buffered.** On error or panic, the body becomes the constant `fallbackBody` (`{"title":"Internal Server Error","status":500}`), with status 500 and `application/problem+json`. The failure goes to the hook.
- **XML:** `internal/xmlenc` marshals each extension value with `encoding/json`, decodes it with `UseNumber` into `any`, and walks the tree:
  - maps become elements sorted by key, after checking the name against XML NameStartChar/NameChar rules (ASCII-restricted, matching the extension name rule)
  - slices become `<i>` elements
  - `nil` becomes an empty element, and scalars become text
  - text is escaped with `xml.EscapeText`
- *Alternative:* `xml` struct tags. Rejected, because types would need double tagging and the two formats would drift.
- **Failure modes:**
  - **NaN, Inf, channels, functions, cycles:** `encoding/json` returns an error, which triggers the fallback.
  - **Invalid XML name:** an error, which triggers the fallback.
  - **Deep nesting:** handled by JSON's own depth limit.

### D8. net/http adapter

```go
func (h *Handler) Wrap(fn func(http.ResponseWriter, *http.Request) error) http.Handler
func (h *Handler) Write(w http.ResponseWriter, r *http.Request, err error)
func (h *Handler) Recover(next http.Handler) http.Handler
```

- `trackingWriter` records `WriteHeader`, `Write` and `Flush` calls and implements `Unwrap() http.ResponseWriter`, so `http.ResponseController` reaches the real writer. It also implements `io.ReaderFrom`, delegating to the underlying writer's `ReadFrom`, so `io.Copy` keeps net/http's sendfile path; a copy marks the response started.
- `Wrap` and `Recover` install the tracking writer, unless the writer passed in already is one, so it's never wrapped twice.
- `Write` assumes nothing was written unless the writer is a `trackingWriter`.
- `Request` is built from `r.Method`, `r.URL.Path` (not `RequestURI`, so the query is excluded) and `r.Header.Get("Accept")`.
- **Failure modes:**
  - **Nil `fn` or nil `*Handler`:** panic at construction.
  - **A handler sets headers without writing:** not "started", so the problem is written. Headers it already set stay, unless they're protected.
  - **The writer doesn't support flushing:** `ResponseController` returns `http.ErrNotSupported`, as it would without the wrapper.

### D9. Negotiation

- `internal/negotiate.Select(accept string, offers []string) string`:
  - parse at most 4 KiB and 32 media ranges, ignoring anything past that
  - media types are matched case-insensitively
  - q-values must be in `0..1` with at most 3 decimals, and a range with an invalid q is dropped
  - specificity: an exact type beats `type/*`, which beats `*/*`
  - the highest q wins, ties go to the first offer (JSON)
  - the result is empty when nothing matches
- The caller falls back to JSON on an empty result.
- **Failure modes:**
  - malformed or oversized headers: bounded parsing, the result falls back to JSON
  - `q=0` excludes a type, which is handled

### D10. Panic recovery

```go
type PanicError struct{ Value any; Stack []byte }
func (e *PanicError) Error() string; func (e *PanicError) Unwrap() error
```

`Recover` defers a function that:
1. calls `recover()`
2. re-panics if the value `== http.ErrAbortHandler`
3. captures `debug.Stack()` (the panic frame is still on the stack inside the deferred call)
4. if the response started, or the value is an error wrapping `EPIPE` or `ECONNRESET`, calls only the hook
5. otherwise uses `Respond`, guarded by a second `recover` that writes `fallbackBody`

- **Failure modes:**
  - `panic(nil)` becomes a `*runtime.PanicNilError`, handled as a normal value.
  - A panic inside the hook during recovery is swallowed.

### D11. Built-in mappers

Unexported `MapperFunc`s for:
- `*json.SyntaxError` and `io.ErrUnexpectedEOF`: 400
- `*json.UnmarshalTypeError`: 422
- `*http.MaxBytesError`: 413
- `context.DeadlineExceeded`: 504

Each detail is a fixed sentence (see the spec). The offset, field and the error text are never used.

- **Failure modes:**
  - A bare `io.EOF` is deliberately not mapped (user decision after review): it can come from any reader, including an upstream service closing early, and a 400 would blame the client for a server failure. It falls to the safe 500; consumers who know it means an empty body add their own mapper.

## Defaults

| Behavior | Default | Why | Opt-in |
|---|---|---|---|
| Built-in mappers | on | common body errors get correct 4xx without setup | `WithoutBuiltins()` |
| Unmapped errors | 500, no detail | never leak internals | consumer mappers |
| `type` | `about:blank` | RFC 9457 default | set per problem |
| `instance` | request path without query | identify the occurrence without leaking query tokens | enricher or set per problem |
| Format | JSON, XML when the client prefers it | most clients use JSON | none yet (`WithEncoders` postponed) |
| `Cache-Control` | `no-store` unless set | errors must not be cached by intermediaries | set header |
| Panic recovery | off | never surprise existing recovery middleware | `Handler.Recover` |
| Logging | none | the library must not choose a logger | `OnError` hook |

## Hot Paths

- `Handler.Resolve`: every step and its no-match path, clone, normalization, title finalization, enricher loop, `safeCall`
- `Handler.Respond`: the nil exit, a canceled request still answered, negotiation, encode, the fallback body, header building
- `Problem.MarshalJSON` and the XML walker, including error returns
- `negotiate.Select`, including the limits and the invalid-q paths
- `Wrap` / `Write` / `trackingWriter`, including the "already started" branch
- `Recover`: the abort re-panic, the started or broken-connection branch, the second-level recover

## Risks / Trade-offs

- [XML goes through a JSON round trip per extension, so it's slower] → XML is the rare format. Benchmark it in slice 5 and keep the JSON path free of it.
- [`Write` without `Wrap` can't detect earlier writes] → Documented. `Wrap` and `Recover` install the tracking writer.
- [Recovering panics in consumer mappers could hide bugs] → Every panic goes to the hook with its stack, and the docs tell consumers to log the hook.
- [Status normalization could surprise a consumer returning 3xx] → Reported to the hook. Problem details for redirects are a misuse.
- [The public API may need changes after the adapter and plugin changes] → Stay on v0, and review the API at the end of slice 3 against the adapter needs described in D5.

## Migration Plan

This is a new library, so there's nothing to migrate. Release `v0.1.0` of the root module after slice 7. The adapter and plugin changes release their modules afterwards.

## Open Questions

- Is `encoding/json/v2` usable without GOEXPERIMENT on Go 1.27? If so, switching later is an internal change with identical output, so the specs don't change.
