# Changelog

All notable changes to this project are documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Until v1.0.0 the public API may still change in minor
releases.

Each module has its own tags (see [RELEASING.md](RELEASING.md)). This file covers the root module,
`github.com/kartaladev/prblm`.

## [Unreleased]

The first release, planned as v0.1.0.

### Added

- `Problem`, the RFC 9457 problem details document, built in one expression with `New` and field
  options (`WithType`, `WithTitle`, `WithDetail`): standard members, `about:blank` and title
  defaults, validated extension members (`Set`, `Ext`, `WithExt`), response headers that are never
  serialized (`WithHeader`), and a hidden cause for `errors.Is` and `errors.As` (`WithCause`).
- Deterministic JSON encoding and lenient JSON decoding.
- XML encoding and lenient decoding per RFC 9457 Appendix B, with the same structure as the JSON.
- `Handler` with a fixed resolution order: self-describing errors (`Describer`), request-scoped
  mappers (`ScopeMappers`), consumer mappers (`WithMappers`, `Is`, `As`, `Status`, `MapperFunc`),
  built-in mappers for malformed, truncated and oversized request bodies and deadline errors
  (`WithoutBuiltins`; a bare `io.EOF` is not mapped, because any reader can return it), status hints
  (`WithStatus`) and a safe 500 fallback.
- Enrichers (`WithEnrichers`) with a default instance from the request path, and an `OnError` hook
  for logging. Panics in mappers, enrichers and the hook are contained and reported. Notes about
  mistakes the handler worked around wrap `ErrInvalidStatus` (a status outside 400–599) or
  `ErrEncoding` (a problem that could not be encoded).
- `Respond`, the framework-neutral response step, with `Accept` negotiation between JSON and XML,
  `Vary` and `Cache-Control` headers, protected content headers, and a constant fallback when
  encoding fails. `Response.MergeInto` merges a response's headers into a framework's headers by
  the same rules as the net/http adapter, so every adapter produces the same headers.
- The net/http adapter: `Wrap` for handlers that return errors, `Write` for standard handlers, and
  opt-in panic recovery with `Recover` and `PanicError`. `io.Copy` through `Wrap` keeps the
  underlying writer's `ReadFrom`, so net/http still uses sendfile.
- `Parse`, which decodes a problem from an HTTP response, and `ErrNotProblem`.
- `WithNamespace`, an application-wide namespace for problem types: relative sub-types such as
  `contracts/blacklisted-supplier` are resolved under it, absolute URIs and paths are kept, and an
  invalid relative type becomes `about:blank` with a hook note wrapping `ErrInvalidType`. Types are
  never built from the request's `Host` header. `NewHandler` panics on an invalid namespace.
