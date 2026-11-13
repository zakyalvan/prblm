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
  - A path namespace produces a path-only `type`, which the client resolves against the request URL. The library never builds a type from the client-controlled `Host` header.
  - An invalid namespace panics at construction.
- **New type definitions:**
  - `prblm.DefineType(subtype, status, title, description)` returns a `*prblm.Type`.
  - `Type.New(fields...)` builds a problem carrying that type, status and title, so a mapper can do `return BlacklistedSupplier.New()`.
  - `prblm.WithTypes(types...)` registers definitions with the handler.
  - An invalid definition, a duplicate sub-type, or `WithTypes` without `WithNamespace` panics at construction.
- **Discovery, on whenever types are registered:**
  - **What is served:** each registered type's documentation at its type URL's path, plus an index at the namespace path. An unknown sub-type under the namespace path gets a 404 problem.
  - **Format:** a complete HTML page rendered with the standard library's `html/template` for browsers and other `text/html` clients, or JSON when the client prefers it.
  - **Caching:** pages are cacheable in the browser (`Cache-Control: public, max-age=3600`, a strong `ETag`, and 304 on `If-None-Match`).
  - **net/http:** mounts it from `(*Handler).Discovery()` and `(*Handler).DiscoveryPath()`.
  - **gin:** `Middleware` serves it for GET/HEAD requests that no route claims.
  - **fiber:** `ErrorHandler` serves it for GET/HEAD requests that hit fiber's route-not-found error.
  - **Opting out:** `prblm.WithoutDiscovery()` keeps the definitions but serves nothing.
- **Hook notes for type mistakes.** The hook gets a note wrapping `prblm.ErrInvalidType` when, with a namespace set, a relative type is not a valid sub-type (the type becomes `about:blank`). It gets a note wrapping `prblm.ErrUndocumentedType` when, with discovery on, a resolved sub-type has no definition (the response is unchanged).

## Defaults and Customization

**With zero configuration** (`prblm.NewHandler()`, `ginprblm.Middleware()`, `fiberprblm.ErrorHandler()`), behavior is exactly what it is today:
- No namespace, and types are passed through as given.
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
- The new exported API is `WithNamespace`, `DefineType`, `Type`, `Type.New`, `WithTypes`, `WithoutDiscovery`, `ErrInvalidType`, `ErrUndocumentedType`, `Handler.Discovery` and `Handler.DiscoveryPath`.

## Capabilities

### New Capabilities

- `problem-types`: application namespace for problem types, resolution of relative sub-types, type definitions (`DefineType`, `Type.New`, `WithTypes`) and their construction-time validation, plus the hook notes for invalid and undocumented types.
- `type-discovery`: serving type documentation at type URLs. Covers the HTML and JSON representations and the index, the 404 for unknown types, `WithoutDiscovery`, and mounting it in net/http.

### Modified Capabilities

- `gin-adapter`: `Middleware` and `Recovery` take core options instead of a handler. The nil-handler requirement is removed. Parity is defined over the same options. New: `Middleware` serves discovery for paths no route claims.
- `fiber-adapter`: `ErrorHandler` takes core options instead of a handler. The nil-handler requirement is removed. Parity is defined over the same options. New: `ErrorHandler` serves discovery on route-not-found.

## Scope by Gall's Law

**The simplest version that works end to end:** `ginprblm.Middleware(prblm.WithMappers(...))` and `fiberprblm.ErrorHandler(...)` give the same problems as today, built from options, and the parity test passes. Each later slice adds one capability to that working system:

1. Namespace resolution of relative sub-types, end to end through net/http, gin and fiber.
2. Type definitions and `WithTypes`.
3. Discovery in the core, mounted in net/http.
4. Discovery served by gin.
5. Discovery served by fiber.
6. Parity and documentation.

**Deliberately left for later:**
- **`prblm.Group("contracts")`,** a route-group sub-type prefix. Postponed because a prefix known only at request time can't be in a catalog the handler builds at construction. Full sub-types go in `DefineType` instead.
- **A one-line mapper helper** for type definitions (e.g. `prblm.IsType(err, t)`). For now, use `prblm.Is(err, func(error) *prblm.Problem { return t.New() })`.
- **Localized documentation,** custom HTML templates, and generating static documentation sites.

## Impact

- **Core** (`github.com/kartaladev/prblm`): new options, the `Type` definition API, type resolution during finalization, the discovery `http.Handler`, and two new note sentinels. Still standard library only (`html/template`).
- **`ginprblm` and `fiberprblm`:** the constructor signatures change, discovery serving is added, and their tests, examples, READMEs and package godoc are updated. The fiber and gin dependency versions are unchanged.
- **`internal/paritytest`:** builds all three servers from one options slice, and gains rows for resolved types and discovery documents.
- **Sequencing with `add-mapper-plugins`:** this change must be implemented before that change's release step (its slice 7), because the release tags the adapter API this change alters. The mapper plugins themselves are unaffected.
