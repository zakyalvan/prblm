# Design

## Context

Motivation is in proposal.md (Why), and behavior is in `specs/gin-adapter` and `specs/fiber-adapter`. The adapters build only on the core API from `add-problem-details-core` (its design D5, D6, D10):
- `Handler.Respond(ctx, Request, err) (Response, bool)` resolves, calls the hook, negotiates, encodes, falls back on encoding failure and builds the headers. It reports `false` for a nil error or a canceled request. It is panic-safe.
- `WithStatus(err, code)` is the status hint. `ScopeMappers(ctx, ...)` adds request-scoped mappers.
- `PanicError{Value, Stack}` carries a recovered panic.

So an adapter only has to:
1. describe the request (`Request`),
2. find the error and any status hint,
3. decide whether its writer can still be written,
4. copy `Response` into the framework's writer.

**About the config rule for slice 1.** The project rule says slice 1 is the smallest path from an error to a response through net/http. That slice already exists in the core change. Here, slice 1 is the smallest path through gin, the first framework adapter.

## Goals / Non-Goals

**Goals:**
- Adapters thin enough that all problem semantics stay in the core, which gives byte-for-byte parity with net/http.
- No surprises in each framework: successful, streamed and hijacked responses are untouched, and existing recovery middleware keeps working.
- Dependencies isolated per module.

**Non-Goals:**
- Changing the core API. The adapters use it as released.
- Echo, chi or other routers.
- Wrapping or replacing gin's or fiber's own logging and recovery middleware.

## Slices

1. **gin: a recorded sentinel error becomes a JSON problem.**
   - At its end: the `ginprblm` module exists, and `Middleware(h)` resolves `c.Errors.Last()` through `Respond` after `c.Next()` when no body was written.
   - Scenarios: gin-adapter / Recorded errors, Unmapped error text never leaks, Nil handler (middleware row), Faulty consumer code under gin.
   - Hot paths introduced: `Middleware`'s post-`Next` path and the copy of `Response` into `gin.ResponseWriter`.
2. **gin: abort semantics through the deferred-header writer.**
   - At its end: `AbortWithError` and `AbortWithStatus` keep full control of the status and `Content-Type`, the abort status becomes a hint, status-only aborts of 400 or above become problems, and other statuses are committed unchanged.
   - Scenarios: AbortWithError, AbortWithStatus and plain statuses, Body written before or after an error, Client went away.
   - Hot paths: the `deferredWriter` methods and the decision branches.
3. **gin: streams and hijacked connections pass through.**
   - At its end: `Flush`, `Hijack`, `CloseNotify` and the first `Write` commit immediately, and after a hijack only the hook runs.
   - Scenarios: Streaming and hijacking, Concurrent gin requests.
   - Hot paths: the commit paths.
4. **gin: Scope and Recovery.**
   - At its end: `Scope(...)` and `Recovery(h)`, working in any order.
   - Scenarios: Route group scope, Degenerate scopes, Recovery placement, Panic details go to the hook, Recovery edge cases, Nil handler (recovery row).
   - Hot paths: the recover path in `Recovery`.
5. **fiber: ErrorHandler.**
   - At its end: the `fiberprblm` module, and `ErrorHandler(h)` with `*fiber.Error` hints, request facts, buffered-body replacement and stream pass-through.
   - Scenarios: all fiber-adapter requirements except recovery and scope.
   - Hot paths: the `ErrorHandler` body and the hint extraction.
6. **fiber: Scope and Recovery.**
   - At its end: `Scope(...)` and `Recovery()`.
   - Scenarios: fiber-adapter scope and recovery requirements.
   - Hot paths: the recover path.
7. **Parity and documentation.**
   - At its end: a cross-adapter parity test in a test-only module, plus the READMEs.
   - Scenarios: gin-adapter / Same output as net/http, fiber-adapter / Same output as net/http.

## Decisions

### D1. Module layout and dependencies

```
ginprblm/    go.mod  github.com/kartaladev/prblm/ginprblm    requires prblm, gin-gonic/gin
fiberprblm/  go.mod  github.com/kartaladev/prblm/fiberprblm  requires prblm, gofiber/fiber/v3
internal/paritytest/ go.mod (unpublished)  requires prblm, ginprblm, fiberprblm
go.work      adds ./ginprblm ./fiberprblm ./internal/paritytest
```

- The parity test lives in its own unpublished module, so neither adapter module depends on the other framework.
- *Alternatives:*
  - Parity tests inside each adapter module. Rejected, because `ginprblm`'s test graph would then contain fiber.
  - A single `adapters` module. Rejected, because it would force gin users to download fiber.
- **Failure modes:**
  - **Before the core is tagged, `GOWORK=off` builds fail:** the adapters change starts only after the core's `v0.1.0`, and CI pins that version.
  - **A future dependency leaks into a graph:** a CI check runs `go list -m all` in `ginprblm` and asserts there's no `gofiber` or `fasthttp`.

### D2. Shared adapter flow

Each adapter does the same steps:

```
req := prblm.Request{Method, Path (no query), Accept}
err := last error (+ status hint from the framework)
resp, ok := h.Respond(requestCtx, req, err)    // hook already called
if !ok || !canWrite { return }                 // "report but don't write"
copy resp.Status, resp.Header (merge, Vary added), resp.Body to the framework writer
```

- **Failure modes:**
  - **`Respond` panics despite its contract:** the adapter wraps the call in a `recover` that writes a bare 500 with no body when it still can, so a core bug never crashes the server.
  - **Header merging duplicates `Vary`:** this uses the core's merge semantics, and is covered by the parity test.

### D3. gin deferred-header writer

**Problem.** gin's `AbortWithStatus` and `AbortWithError` call `WriteHeaderNow()`, which sends the header to the client immediately. After that, `Content-Type` can't be set and the status can't change.

**Status: unverified.** This claim comes from reading gin's source, not from a failing test. Under the project's proof rule (CLAUDE.md rule 8), task 3.1 must reproduce it with a failing test before the deferred writer is built. If that test passes, this decision is dropped and slice 3 is re-planned.

**Approach.** Before `c.Next()`, `Middleware` replaces `c.Writer` with a `deferredWriter` that embeds `gin.ResponseWriter`:
- `WriteHeader` and `WriteHeaderNow` only record the status.
- Any of these commit the recorded header to the underlying writer and set `committed`: `Write`, `WriteString`, `Flush`, `Hijack`, `CloseNotify`, `Pusher`.
- `Hijack` additionally sets `hijacked`.
- `Status()`, `Size()` and `Written()` report gin-compatible values, so code that reads them behaves as before.

After `c.Next()`, the middleware decides as follows:

```
hijacked                         -> if errors: Respond (hook only); return
committed (body/flush started)   -> if errors: Respond (hook only); return
errors present                   -> err := last; if recorded >= 400: err = WithStatus(err, recorded)
                                    write Respond(...) through the underlying writer
recorded >= 400, no body         -> Respond(WithStatus(errStatusOnly, recorded))   // about:blank problem
otherwise                        -> commit recorded header unchanged (e.g. 204, 304, 200)
```

`errStatusOnly` is an unexported sentinel that maps to nothing, so the hint produces an `about:blank` problem. The hook sees a status-only problem, which documents the abort.

- *Alternatives:*
  - Telling users to use `c.Error` plus `c.Abort` instead of `AbortWithError`. Rejected, because existing gin code uses aborts heavily.
  - Writing after headers were sent. Rejected, because the response would lack `application/problem+json`.
- **Failure modes:**
  - **Middleware registered before ours aborts:** the header is committed early. The adapter detects a committed header with no body (`Written() && Size() == 0` on the original writer), reports to the hook, and writes nothing. Documented as a registration-order rule.
  - **Hijack through our writer:** delegated, so the websocket keeps working. The library never writes after a hijack.
  - **A panic while the deferred writer is installed:** `Recovery` (D5) writes through `c.Writer`, which is still the deferred writer, so its recorded status is replaced.
  - **gin's `ResponseWriter` interface gains methods:** embedding the interface keeps the type compiling. New committing methods would need explicit overrides, so CI runs against the latest gin minor.

### D4. gin Scope

`Scope(m ...prblm.Mapper) gin.HandlerFunc`:
- filters out nil mappers at construction
- returns a pass-through when no mappers are left
- otherwise does `c.Request = c.Request.WithContext(prblm.ScopeMappers(ctx, m...))` and calls `c.Next()`

- **Failure modes:**
  - **Concurrent requests:** each request gets its own context. The core guarantees that scopes are immutable.

### D5. gin Recovery

`Recovery(h)` defers a function that does the following:
1. Calls `recover()`.
2. Re-panics on `http.ErrAbortHandler`.
3. Captures `debug.Stack()` and builds `&prblm.PanicError{Value, Stack}`.
4. If the response is committed or hijacked, or the value wraps `EPIPE` or `ECONNRESET`, calls `Respond` for the hook only and then `c.Abort()`.
5. Otherwise writes `Respond(...)` through `c.Writer` and calls `c.Abort()`.

`Recovery` never adds to `c.Errors`, so `Middleware` sees a written body and passes it through, which avoids a double write.

- *Alternative:* recording the panic with `c.Error`. Rejected, because when `Recovery` sits outside `Middleware`, the error middleware has already unwound and would never see it.
- **Failure modes:**
  - **Nil handler:** panics at construction.
  - **`Respond` panics:** see D2.
  - **Used alongside `gin.Recovery()`:** whichever is innermost handles the panic. The docs recommend replacing `gin.Recovery()`.

### D6. fiber ErrorHandler

`ErrorHandler(h) fiber.ErrorHandler` returns `func(c fiber.Ctx, err error) error`, which:
- Extracts the hint: if `errors.AsType[*fiber.Error](err)` finds one, it uses `err = prblm.WithStatus(err, fe.Code)`. The message isn't used, because the core never puts error text into a response.
- Builds `Request` from `c.Method()`, `c.Path()` (no query) and `c.Get("Accept")`.
- Calls `Respond(c.Context(), req, err)`.
- **If a body stream writer is set:** the response is streaming, so it returns `nil` without touching it (the hook has already been called).
- **Otherwise:** resets the buffered body, sets the status and headers (`c.Set` per value, appending to `Vary`) and the body (`c.Send`), and returns `nil`.
- **When `ok` is false:** returns `nil` without writing.

- *Alternative:* middleware wrapping `c.Next()`. Rejected, because fiber's routing errors (404, 405) only reach the error handler.
- **Failure modes:**
  - **Nil handler:** panics at construction.
  - **The fiber error code is out of range:** the core ignores the hint, giving a 500.
  - **Headers set earlier by the handler:** they stay, except `Content-Type` and `Content-Length`, which the core protects.
  - **Fiber reuses `Ctx` objects after the handler:** nothing is retained beyond the call.

### D7. fiber Scope and Recovery

- `Scope(m ...prblm.Mapper) fiber.Handler`: filters nil mappers, then `c.SetContext(prblm.ScopeMappers(c.Context(), m...))` and `return c.Next()`.
- `Recovery() fiber.Handler`: a deferred `recover` sets a named return error. That error is an unexported `recoveredError{p *prblm.PanicError}`:
  - `Error()` returns the constant `"Internal Server Error"`
  - `Unwrap()` returns the `*PanicError`

  So fiber's **default** error handler, which sends `err.Error()` as the body, can't leak the panic value. The adapter's `ErrorHandler` resolves it through the core like any other error (500, fallback).

  `http.ErrAbortHandler` has no meaning in fasthttp, so it's treated as an ordinary panic value.

  `Recovery` takes no handler, because it doesn't render anything itself.
- **Failure modes:**
  - **Used without the adapter's `ErrorHandler`:** the response is fiber's plain-text 500 `Internal Server Error`, with no leak. Documented.
  - **A panic inside the error handler itself:** that's fiber's own behavior. `Respond` is panic-safe, and D2's guard covers the rest.

### D8. Parity test

The test-only module `internal/paritytest` builds one `prblm.Handler` and three servers (net/http `Wrap` + `Recover`, gin, fiber), sends identical requests, and compares the status, `Content-Type`, `Vary` and body bytes for every row of the parity scenarios.

- **Failure modes:**
  - **Frameworks add their own headers** (e.g. `Date`, `Server`): only the specified headers are compared.

## Defaults

| Behavior | Default | Why | Opt-in |
|---|---|---|---|
| Problem semantics | the core's defaults, unchanged | one source of truth | core options |
| Framework error messages | never exposed (only codes) | messages often carry internals | consumer mapper |
| Successful, streamed and hijacked responses | untouched | adapters must not break working handlers | — |
| Already-written gin bodies | kept, error reported to the hook | can't replace bytes already sent | — |
| Buffered fiber bodies | replaced by the problem | nothing has been sent yet | — |
| Panic recovery | off | don't fight existing recovery middleware | `Recovery` |

## Hot Paths

- **gin `Middleware`:** every post-`Next` branch (hijacked, committed, errors with or without a recorded status, status-only of 400 or above, commit unchanged) and the `Respond` copy.
- **`deferredWriter`:** `WriteHeader`/`WriteHeaderNow` (record), and `Write`/`WriteString`/`Flush`/`Hijack`/`CloseNotify` (commit).
- **gin `Recovery`:** the abort re-panic, the committed or broken-connection branch, the write branch, and the D2 guard.
- **fiber `ErrorHandler`:** hint extraction, the `!ok` exit, the streaming exit, and the write branch.
- **fiber `Recovery`:** the recover branch and `recoveredError`.
- **`Scope` (both):** the empty pass-through and the scoped branch.

## Risks / Trade-offs

- [gin's internals around `WriteHeaderNow` change in a later version] → CI runs against the latest gin minor, and `deferredWriter` tests assert the abort behavior directly.
- [fiber v3 API changes between minors] → Pin a tested minor, run CI against the latest v3, and keep the adapter under about 150 lines.
- [Users register ginprblm after an aborting middleware] → Documented. Detected at runtime and reported to the hook rather than producing a malformed response.
- [fiber body replacement differs from net/http, where a written body is kept] → This is a framework difference: fiber hasn't sent anything yet. Documented, and excluded from the parity rows.

## Migration Plan

New modules, so there's nothing to migrate. Tag `ginprblm/v0.1.0` and `fiberprblm/v0.1.0` after slice 7, against the released core `v0.1.0`.

## Open Questions

- Which fiber v3 minor to pin as the minimum? It doesn't change the specs. Pick the latest stable v3 minor at implementation time.
