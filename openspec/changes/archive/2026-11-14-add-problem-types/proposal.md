# Proposal

## Why

Two things make the library awkward to adopt across many services:

- **Adapter setup needs two steps.** gin and fiber users build a `*prblm.Handler` first and then pass it to the adapter. It's cleaner to configure the adapter directly with the same options `prblm.NewHandler` takes.
- **Problem types have no structure.** Each mapper writes a full type URI by hand, so the URIs drift between teams. Nothing serves documentation at those URIs either, although RFC 9457 (section 3.1.1) expects a dereferenceable type to lead to human-readable docs.

This change adds:
- an application-wide namespace, so business code only names a sub-type such as `contracts/blacklisted-supplier`
- type definitions that carry the type's status, title and description in one place
- opt-out discovery that serves each defined type's documentation at its type URL

It has to land now, before the first release, because it changes the adapter constructors.

## What Changes

- **BREAKING (pre-release, nothing tagged):** the adapter constructors take core options instead of a handler, and build their `*prblm.Handler` internally:
  - `ginprblm.Middleware(opts ...prblm.Option)`
  - `ginprblm.Recovery(opts ...prblm.Option)`
  - `fiberprblm.ErrorHandler(opts ...prblm.Option)`

  With no arguments they give the core's safe defaults. `fiberprblm.Recovery()` and both `Scope` functions are unchanged. Because there is no handler argument any more, the nil-handler construction panic goes away. Nil options are ignored, as in `NewHandler`.
- **New core option `prblm.WithNamespace(ns)`.** `ns` is either an absolute `http`/`https` URI (`https://kartala.id/probs`) or an absolute path (`/v1/api/probs`).
  - A relative sub-type type such as `contracts/blacklisted-supplier` is resolved to `<ns>/contracts/blacklisted-supplier` when the problem is finalized.
  - Absolute URIs, absolute paths and `about:blank` are never rewritten.
  - When several `WithNamespace` options are given, the last one wins. Only that final value is validated, so option order matters only between namespaces.
  - A path namespace produces a path-only `type`, which the client resolves against the request URL. The library never builds a type from the client-controlled `Host` header.
  - An invalid namespace panics at construction.
  - A relative type that is not a valid sub-type becomes `about:blank`, and its title becomes the status phrase (RFC 9457 section 4.2.1), so no type-specific title is left on a problem that lost its type.
- **New type definitions:**
  - `prblm.DefineType(subtype, status, title, description)` returns a `*prblm.Type`.
  - `Type.New(fields...)` builds a problem carrying that type, status and title, so a mapper can do `return BlacklistedSupplier.New()`.
  - `prblm.WithTypes(types...)` registers definitions with the handler.
  - An invalid definition, a duplicate sub-type, or `WithTypes` without `WithNamespace` panics at construction. `WithTypes` re-validates every definition, so a zero-value `&prblm.Type{}`, which skips `DefineType`, is rejected too.
  - A nil or zero-value `*Type` is safe: `New` returns the safe 500 problem, and the accessors return zero values.
- **Discovery, on whenever types are registered:**
  - **What is served:** each registered type's documentation at its type URL's path, plus an index at the namespace path. An unknown sub-type under the namespace path gets a 404 problem.
  - **Format:** a complete HTML page rendered with the standard library's `html/template` for browsers and other `text/html` clients, or JSON when the client prefers it.
  - **Caching:** pages are cacheable in the browser (`Cache-Control: public, max-age=3600`, a strong `ETag`, and 304 on `If-None-Match`).
  - **Core entry point for adapters:** `(*Handler).RespondDiscovery(ctx, prblm.DiscoveryRequest)` returns the discovery response, or `false` when the request is not for discovery. Every adapter uses it, so the "is this a discovery request" rule lives in one place.
  - **Rendering on first use:** pages are rendered once, the first time discovery is used. A handler that never serves discovery, such as the one `ginprblm.Recovery` builds, never renders them.
  - **net/http:** mounts it from `(*Handler).Discovery()` and `(*Handler).DiscoveryPath()`.
  - **gin:** `Middleware` serves it for GET/HEAD requests whose path no route claims for any method. With `engine.HandleMethodNotAllowed`, a path that has a route only for another method still gets gin's 405. gin routes HEAD on its own, so a HEAD to a path with a route only for GET gets the documentation's headers, where gin alone would answer 404.
  - **fiber:** `ErrorHandler` serves it for GET/HEAD requests that hit fiber's route-not-found error. fiber registers HEAD with every GET route, so such a route answers both.
  - **Shadowed documentation:** an application route inside the namespace can hide a documentation page or the index. The hook gets a note wrapping `prblm.ErrShadowedDocumentation`, once per route, at the first request the route answers. Adapters report through `(*Handler).NoteShadowedRoute`: gin's `Middleware` does it for every request, and fiber needs the new opt-in `fiberprblm.Discovery(opts...)` middleware (`app.Use`), because its error handler never sees a request a route answers. net/http has no such hook point, so there it is only documented.
  - **Opting out:** `prblm.WithoutDiscovery()` keeps the definitions but serves nothing.
- **Hook notes for type mistakes.** The hook gets a note wrapping `prblm.ErrInvalidType` in two cases:
  - With a namespace set, a relative type is not a valid sub-type. The type becomes `about:blank`.
  - Without a namespace, the type is relative, for example from `Type.New()`. The type is passed through unchanged, because RFC 9457 allows relative references and this is today's behavior, but a client resolves it against the request URL, so the note makes the mistake visible.

  It gets a note wrapping `prblm.ErrUndocumentedType` when, with discovery on, a resolved sub-type has no definition (the response is unchanged), and a note wrapping `prblm.ErrShadowedDocumentation` when an application route hides a documentation page. That last note arrives on a request that succeeded, so the hook is no longer called only for answered errors. A note's error is built only when a hook is set, so resolution doesn't allocate without one. Discovery's own problem responses (404, 405) never reach the hook, not even as notes, because no application error occurred.

## Defaults and Customization

**With zero configuration** (`prblm.NewHandler()`, `ginprblm.Middleware()`, `fiberprblm.ErrorHandler()`), behavior is exactly what it is today:
- No namespace, and types are passed through as given. A relative type also sends the hook an `ErrInvalidType` note. Without a hook this changes nothing.
- No type definitions, and nothing is served.
- All existing safety defaults are unchanged: no error text on 5xx, no stack traces, no database internals, `instance` without the query string.

**Opt-ins:**
- `WithNamespace` (type resolution)
- `WithTypes` (definitions, which turns discovery on)
- `WithoutDiscovery` (keeps the definitions, serves nothing)

**Safety review of discovery:** it exposes only text the consumer wrote into `DefineType`: title, status and description. It is never derived from errors or requests.
- HTML output is escaped and sent with `X-Content-Type-Options: nosniff` and a restrictive `Content-Security-Policy`.
- Turning on discovery by registering types is the deliberate opt-in. It publishes documentation and loosens no safety default.

**Compatibility decisions:**
- The adapter constructors change signature. This is breaking, but nothing has been released.
- `NewHandler` can now panic, but only for an invalid `WithNamespace` or `WithTypes`. That is a startup misconfiguration, like an invalid regexp passed to `MustCompile`.
- The new exported API is `WithNamespace`, `DefineType`, `Type`, `Type.New`, `WithTypes`, `WithoutDiscovery`, `ErrInvalidType`, `ErrUndocumentedType`, `ErrShadowedDocumentation`, `Handler.Discovery`, `Handler.DiscoveryPath`, `Handler.RespondDiscovery`, `Handler.NoteShadowedRoute` and `DiscoveryRequest` in the core, and `fiberprblm.Discovery` in the fiber adapter. `RespondDiscovery` is added so gin and fiber share one discovery check instead of each writing their own, and so fiber needs no `net/http` request conversion.

## Capabilities

### New Capabilities

- `problem-types`: application namespace for problem types, resolution of relative sub-types, type definitions (`DefineType`, `Type.New`, `WithTypes`) and their construction-time validation, plus the hook notes for invalid and undocumented types.
- `type-discovery`: serving type documentation at type URLs. Covers the HTML and JSON representations and the index, the 404 for unknown types, `WithoutDiscovery`, and mounting it in net/http.

### Modified Capabilities

- `gin-adapter`: `Middleware` and `Recovery` take core options instead of a handler. The nil-handler requirement is removed. Parity is defined over the same options. New: `Middleware` serves discovery for paths no route claims.
- `fiber-adapter`: `ErrorHandler` takes core options instead of a handler. The nil-handler requirement is removed. Parity is defined over the same options. New: `ErrorHandler` serves discovery on route-not-found.

## Scope by Gall's Law

**The simplest version that works end to end (slice 1):** `ginprblm.Middleware(prblm.WithMappers(...))` and `fiberprblm.ErrorHandler(...)` give the same problems as today, built from options, and the parity test passes. Each later slice adds one capability to that working system:

2. Namespace resolution of relative sub-types, end to end through net/http, gin and fiber.
3. Type definitions and `WithTypes`.
4. Discovery in the core, mounted in net/http.
5. Discovery served by gin.
6. Discovery served by fiber.
7. Parity and documentation.
8. Review fixes: the findings of `/opsx:verify` and the PR #3 code review, applied to the working system (shared `RespondDiscovery`, gin 405 handling, zero-value and nil definitions, namespace option order, the no-namespace note, the `about:blank` title, silent discovery problems, discovery rendered on first use, allocation-free notes, the gin HEAD behavior, and the shadowed-documentation note with fiber's discovery middleware).

**Deliberately left for later:**
- **`prblm.Group("contracts")`,** a route-group sub-type prefix. Postponed because a prefix known only at request time can't be in a catalog the handler builds at construction. Full sub-types go in `DefineType` instead.
- **A one-line mapper helper** for type definitions (e.g. `prblm.IsType(err, t)`). For now, use `prblm.Is(err, func(error) *prblm.Problem { return t.New() })`.
- **Localized documentation,** custom HTML templates, and generating static documentation sites.

## Impact

- **Core** (`github.com/kartaladev/prblm`): new options, the `Type` definition API, type resolution during finalization, the discovery `http.Handler`, and two new note sentinels. Still standard library only (`html/template`).
- **`ginprblm` and `fiberprblm`:** the constructor signatures change, discovery serving is added, and their tests, examples, READMEs and package godoc are updated. The fiber and gin dependency versions are unchanged.
- **`internal/paritytest`:** builds all three servers from one options slice, and gains rows for resolved types and discovery documents.
- **Sequencing with `add-mapper-plugins`:** this change must be implemented before that change's release step (its slice 7), because the release tags the adapter API this change alters. The mapper plugins themselves are unaffected.
