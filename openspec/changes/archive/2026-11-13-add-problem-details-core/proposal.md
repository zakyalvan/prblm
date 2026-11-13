# Proposal

## Why

Go HTTP services need to report errors as RFC 9457 problem details. Hand-rolled, per-framework error handlers tend to share the same problems:
- they leak internal error text or stack traces to clients
- mapping order is implicit, and "no match" is signalled with magic values
- database and validation libraries are hard-wired in
- every framework (net/http, gin, fiber) gets its own implementation

This change delivers the foundation of a standalone, framework-agnostic RFC 9457 library, `github.com/kartaladev/prblm`: a core with no dependencies, safe defaults and consumer-injectable error mapping, working end to end over net/http. Framework adapters and mapper plugins build on it in later changes.

## What Changes

- New Go module `github.com/kartaladev/prblm` (Go 1.27) with no third-party dependencies and no external problem-details library.
- RFC 9457 problem model:
  - standard members `type`, `status`, `title`, `detail`, `instance`
  - validated top-level extension members
  - response headers carried with the problem
  - the problem itself usable as a Go `error`
- Encodings: `application/problem+json` and `application/problem+xml` (RFC 9457 Appendix B), plus lenient decoding and a client-side `Parse` helper.
- Error resolution:
  - consumer-injectable mappers, where the first match wins
  - sentinel and typed helpers
  - self-describing errors and the status hint
  - request-scoped mappers and built-in standard-library mappers
  - a safe 500 fallback
  - enrichers and an `OnError` hook
- A framework-neutral response step (`Respond`) that adapters reuse: `Accept` negotiation, headers, and fallback when encoding fails.
- net/http integration: an error-returning handler adapter, a direct write function, and opt-in panic recovery middleware.

## Defaults and Customization

With zero configuration (`prblm.NewHandler()`), a consumer gets:
- **Built-in mappers** for malformed request bodies, oversized bodies and deadline errors, each with a generic detail.
- **A 500 fallback** for anything unmapped, with type `about:blank`, title `Internal Server Error`, and **no detail**. Error text, stack traces and database internals never reach the response.
- **`instance`** set to the request path. The query string is excluded, so tokens in it can't leak.
- **JSON by default:** `application/problem+json`, with XML only when the client prefers it. `Vary: Accept` and `Cache-Control: no-store` are always set.
- **Recovery for faulty consumer code:** a mapper, enricher or hook that panics doesn't crash the request. The response falls back to the safe 500.

Opt-in customization:
- consumer mappers, request-scoped mappers and enrichers
- an `OnError` hook
- disabling the built-in mappers
- extra encoders (postponed to a later change: no scenario needs a public encoder API yet)
- panic recovery middleware

No option loosens a safety default in this change.

## Capabilities

### New Capabilities

- `problem-document`: The RFC 9457 problem model: member semantics and defaults, extension member rules, JSON and XML encoding, lenient decoding and client-side parsing.
- `error-mapping`: How an error becomes a problem: mapper contract, resolution order, self-describing errors, status hint, request-scoped mappers, built-in mappers, safe fallback, enrichers, `OnError` hook and faulty consumer code.
- `problem-response`: How a resolved problem becomes an HTTP response: status consistency, content negotiation, headers, already-started responses and failure fallback.
- `stdlib-adapter`: Integration with net/http: error-returning handlers and direct writes.
- `panic-recovery`: Opt-in conversion of net/http handler panics into safe 500 problem responses.

### Modified Capabilities

None. This is a new project with no existing specs.

## Scope by Gall's Law

**The simplest working version:** a handler adapter that turns a registered sentinel error into an `application/problem+json` response over net/http. Each later slice adds one capability to that working system:
1. the full problem model
2. the full resolution chain
3. hardened response writing
4. XML and negotiation
5. panic recovery
6. client-side parsing

Deliberately left to later changes:
- `add-framework-adapters`: gin and fiber v3 adapters, each with recovery
- `add-mapper-plugins`: sqlmap, pgxmap, gormmap and validatormap

## Impact

- New repository content: the root module (`go.mod`, package `prblm`, `internal/negotiate`, `internal/xmlenc`), CI, lint configuration and a `go.work` that later changes extend.
- **Public API (compatibility decision):** `Problem`, `Mapper`, `MapperFunc`, `Is`, `As`, `Status`, `Describer`, `Handler`, `NewHandler` and its options, `Request`, `Response`, `Respond`, `WithStatus`, `ScopeMappers`, `RequestFrom`, `PanicError`, `Parse`, and the hook-note sentinels `ErrInvalidStatus` and `ErrEncoding`. The `Encoder` interface is postponed to a later change. The library stays on v0 until early adopters have validated the API, so it can still change.
- **Default responses (compatibility decision):** these defaults define what clients of adopting services will see:
  - `type` is `about:blank` unless a mapper sets one
  - unmapped errors never expose their text
  - malformed JSON returns 400 and a wrong JSON type returns 422
  - stack traces are never sent, and there is no debug switch
