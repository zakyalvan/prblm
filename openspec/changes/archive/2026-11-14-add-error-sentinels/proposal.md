# Proposal

## Why

Today a consumer who wants a 404 must first declare its own `ErrVesselNotFound`, then register a mapper for it. Every service repeats that for the same handful of outcomes: invalid input, not authenticated, not allowed, not found, conflict, rate limited, a dependency down. The core can ship those outcomes as exported sentinel errors with a built-in mapping, so a handler can `return fmt.Errorf("vessel %d: %w", id, prblm.ErrNotFound)` and get a correct, leak-free problem with no configuration. gRPC, Twirp, connect, Kubernetes and google.rpc all ship such a catalog; this change brings a small, HTTP-shaped one to prblm before the `v0.1.0` release, while adding exported names is still cheap to get right.

## What Changes

- Exported sentinel errors in the core package, plain `errors.New` values with `prblm: ...` strings, matched with `errors.Is`:

  | Sentinel | Status | Detail |
  |---|---|---|
  | `ErrInvalidInput` | 400 | `The request is invalid.` |
  | `ErrUnauthenticated` | 401 | `Authentication is required.` |
  | `ErrPermissionDenied` | 403 | `You are not allowed to perform this action.` |
  | `ErrNotFound` | 404 | `The requested resource was not found.` |
  | `ErrConflict` | 409 | `The request conflicts with the current state of the resource.` |
  | `ErrPreconditionFailed` | 412 | `A precondition of the request was not met.` |
  | `ErrRateLimited` | 429 | `Too many requests.` |
  | `ErrBadGateway` | 502 | `An upstream service returned an invalid response.` |
  | `ErrUnavailable` | 503 | `The service is temporarily unavailable.` |
  | `ErrUpstreamTimeout` | 504 | `An upstream service did not respond in time.` |

- A built-in mapping for each sentinel, part of the existing built-in mappers (resolution step 4): an `about:blank` problem with the status above, the status phrase as title, and the fixed detail.
- Consumers can return a sentinel directly, wrap it (`fmt.Errorf` with `%w`, `errors.Join`), or give a domain error type an `Is` method that reports the sentinel.
- Deliberately not shipped: a 400 `ErrFailedPrecondition` (google.rpc) next to the 412 one, and a 501 `ErrNotImplemented`. See design D3 and D4.

## Defaults and Customization

- **Zero configuration:** every sentinel produces its problem with no option. The wrap text (`vessel 7: ...`), the sentinel's own error string and any wrapped cause never reach the client; the detail is always the fixed sentence above. 5xx sentinels carry only that generic sentence, never error text.
- **Consumers win:** a problem or describer in the chain (step 1), a scoped mapper (step 2) and a consumer mapper (step 3) all run before the built-ins, so `prblm.Is(prblm.ErrNotFound, ...)` replaces the default problem, for example with a typed problem or a `WWW-Authenticate` header on 401.
- **Sentinels beat status hints:** a sentinel in the chain decides the status even when the error also carries a `WithStatus` hint, because a sentinel names what happened and a hint only says "if nothing else knows".
- **Opt-out:** `WithoutBuiltins()` disables the sentinel mappings together with the other built-ins; the errors then fall to the hint or the safe 500.
- **No opt-in loosens a safety default.** No option exposes the wrap text.

## Capabilities

### New Capabilities

- `error-sentinels`: Exported sentinel errors for common outcomes and their built-in, leak-free mapping to problems.

### Modified Capabilities

None. The `error-mapping` resolution order and its built-in scenarios are unchanged; the sentinels are added to step 4.

## Scope by Gall's Law

**The simplest version that works:** `ErrNotFound` alone, mapped by the built-ins, answered as a 404 problem through net/http, with tests for wrapped, joined and custom-`Is` errors, `WithoutBuiltins`, consumer override, status hints and no wrap-text leak.

Each later slice adds a batch of sentinels to that working system:
1. client errors: 400, 401, 403, 409
2. request-state errors: 412, 429
3. dependency errors: 503 and 504, then 502

Deliberately left out:
- `Retry-After` on 429 and 503 (a sentinel can't carry a value; an enricher or consumer mapper can add it, and a typed error may come later)
- `WWW-Authenticate` on 401 (the library doesn't know the scheme)
- mapping `sql.ErrNoRows` or `gorm.ErrRecordNotFound` to these sentinels in the mapper plugins
- `ErrFailedPrecondition` (400) and `ErrNotImplemented` (501)

## Impact

- Core package only (`github.com/kartaladev/prblm`): a new file for the sentinels, new entries in `builtins.go`, godoc in `doc.go` and `options.go` (`WithoutBuiltins`), README and CHANGELOG. No new dependency, no new module.
- **Public API (compatibility decision):** ten exported `Err...` variables. Removing or renaming one later is a breaking change, so the catalog starts small.
- **Default responses (compatibility decision):** an error wrapping one of the sentinels changes from 500 (or its hint) to the sentinel's status. Nobody can return these sentinels today, so no existing response changes.
- Adapters: gin and fiber resolve through the core, so they get the sentinels unchanged; a parity check covers them.
- **Ordering:** independent of `add-mapper-plugins`. Both ship before the `v0.1.0` release; if this change merges first, the release group of `add-mapper-plugins` (its slice 7) includes these sentinels in the core's `v0.1.0` CHANGELOG entry.
