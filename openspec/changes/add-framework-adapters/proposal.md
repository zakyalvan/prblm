# Proposal

## Why

The core library (`add-problem-details-core`) renders Go errors as RFC 9457 problems over net/http. Most Go HTTP services, though, run on a framework such as gin or fiber, and each of those reports errors its own way: gin through `c.Errors` and aborts, fiber through the error a handler returns. Without adapters, every consumer would have to reimplement that glue, and the problem output would drift between frameworks. This change adds thin gin and fiber v3 adapters on top of the core's framework-neutral response step, so one configured handler produces the same problems everywhere.

## What Changes

- New Go module `github.com/kartaladev/prblm/ginprblm` (Go 1.27, requires gin):
  - **`Middleware`:** turns the last error in `c.Errors` into a problem. A deferred-header writer keeps full control of the status and `Content-Type`, even after `AbortWithStatus` or `AbortWithError`. The abort status is used as the status hint, a status-only abort of 400 or above becomes an `about:blank` problem, and successful responses, streams and hijacked connections pass through untouched.
  - **`Scope`:** request-scoped mappers for a route group.
  - **`Recovery`:** opt-in panic recovery that works regardless of middleware order.
- New Go module `github.com/kartaladev/prblm/fiberprblm` (Go 1.27, requires gofiber/fiber/v3):
  - **`ErrorHandler`:** for `fiber.Config`. A `*fiber.Error` code (including fiber's own 404 and 405) is used as the status hint, and its message is never exposed.
  - **`Scope`:** request-scoped mappers.
  - **`Recovery`:** opt-in panic recovery that returns the panic to the error handler as an error that never carries the panic value in its message.
- Both adapters guarantee **parity with the net/http adapter:** for the same configured handler, the same status, `Content-Type`, `Vary` and body bytes.
- No change to the core module's API or behavior.

## Defaults and Customization

With zero configuration beyond installing the adapter (`ginprblm.Middleware(prblm.NewHandler())`, or `fiber.Config{ErrorHandler: fiberprblm.ErrorHandler(prblm.NewHandler())}`):
- Errors become RFC 9457 problems with the core's safe defaults: no error text on 5xx, no stack traces, `instance` without the query string, JSON unless the client prefers XML.
- Framework-generated error messages are never exposed. That covers fiber's `Cannot GET /x` and the text of errors passed to gin's `AbortWithError`. Only their status codes are used.
- Successful responses, streamed bodies and hijacked connections (websockets) are never altered.
- Panics are **not** recovered unless the adapter's `Recovery` middleware is installed. Existing recovery middleware keeps working.

Opt-in customization: everything the core offers (mappers, scoped mappers, enrichers, hook, encoders), plus the adapters' `Scope` and `Recovery` middleware. No option loosens a safety default.

## Capabilities

### New Capabilities

- `gin-adapter`: Integration with gin: error collection from `c.Errors`, abort semantics, status-only aborts, pass-through of successful, streamed and hijacked responses, scoped mappers, panic recovery, and parity with net/http.
- `fiber-adapter`: Integration with fiber v3: the error handler, fiber error codes as status hints, request information, scoped mappers, panic recovery, and parity with net/http.

### Modified Capabilities

None. The core capabilities (`error-mapping`, `problem-response`, `stdlib-adapter`, `panic-recovery`) are used unchanged.

## Scope by Gall's Law

**The simplest working version:** gin `Middleware` turning `c.Error(ErrX)` into the core's JSON problem through `Respond`. Each later slice adds one capability:
1. gin abort semantics (deferred header)
2. gin pass-through for successful, streamed and hijacked responses
3. gin `Scope` and `Recovery`
4. fiber `ErrorHandler`
5. fiber `Scope` and `Recovery`
6. cross-adapter parity and documentation

**Depends on:** `add-problem-details-core` (implemented and archived). Nothing is tagged until all planned changes are complete. Until then each adapter module points at the core in this repository with `replace github.com/kartaladev/prblm => ../`, so `GOWORK=off` builds work without a tag. The release (the core's `v0.1.0` first, then the adapter tags) is the final step of `add-mapper-plugins`, which is implemented after this change.

Deliberately left for later: echo, chi and other routers (chi and `http.ServeMux` users are already served by the net/http adapter), and the mapper plugins (`add-mapper-plugins`).

## Impact

- New modules `ginprblm/` and `fiberprblm/`, each with its own `go.mod` requiring `github.com/kartaladev/prblm`. `go.work` gains both modules. The release tags are `ginprblm/vX.Y.Z` and `fiberprblm/vX.Y.Z`.
- Third-party dependencies stay confined: gin only in `ginprblm`, and fiber and fasthttp only in `fiberprblm`. Neither the root module's graph nor `ginprblm`'s graph contains gofiber or fasthttp.
- **Public API (compatibility decision):**
  - one additive core export, `prblm.Response.MergeInto`, so every adapter merges headers by the core's rules (design D2)
  - `ginprblm.Middleware`, `ginprblm.Scope`, `ginprblm.Recovery`
  - `fiberprblm.ErrorHandler`, `fiberprblm.Scope`, `fiberprblm.Recovery`

  The modules are released as v0 together with the core.
- **Framework behaviors adopters should know about (compatibility decision):**
  - gin's `Middleware` must be registered before middleware that aborts.
  - fiber's `Recovery` should be paired with the adapter's `ErrorHandler`.
  - With fiber, a handler's buffered body is replaced by the problem, because nothing has reached the client yet. A streamed body is never replaced.
