# Design

## Context

Motivation is in proposal.md (Why), and behavior is in `specs/error-sentinels/spec.md`. This change uses the core as it is on `main`:
- `Handler.match` runs the resolution steps: self-describing errors, scoped mappers, consumer mappers, built-ins (`h.builtins`), the status hint (`hinted`), then the fallback.
- `builtins` in `builtins.go` is a package-level `[]Mapper` copied into every handler by `NewHandler` and cleared by `WithoutBuiltins`.
- `firstMatch` calls each mapper through `safeCall`, so a mapper that panics, including through an `Is` method it calls, is treated as no match and reported to the hook.
- A resolved problem is cloned before it is completed, and `finalize` gives an `about:blank` problem without a title the status phrase.
- The package already exports `Err...` values, but they are notes to the `OnError` hook (`ErrInvalidStatus`, `ErrEncoding`, `ErrInvalidType`, ...) or results of `Parse` (`ErrNotProblem`), never errors for an application to return.

Constraints: core module only, standard library only, Go 1.27. No change to the resolution order in the `error-mapping` spec.

## Goals / Non-Goals

**Goals:**
- An application returns or wraps a sentinel and gets a correct problem, with nothing to register.
- The sentinel's mapping is a default, never a lock-in: every earlier resolution step can replace it.
- A small catalog that can grow without breaking anyone.

**Non-Goals:**
- Response headers on sentinel problems (`Retry-After`, `WWW-Authenticate`).
- A typed error carrying a status, a code or a retry delay. `WithStatus` and `*Problem` already cover a status chosen at the call site.
- Changing the mapper plugins of `add-mapper-plugins` to produce these sentinels.
- Errors with a translated or per-request detail.

## Slices

Each slice ends with a releasable core: the sentinels it adds work through net/http with zero configuration, are documented, and are in the CHANGELOG.

1. **`ErrNotFound` through the built-ins over net/http.**
   - At its end: `return fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` from a `Wrap` handler answers a 404 `about:blank` problem with the fixed detail, in JSON and XML, with no option. Consumer mappers, scoped mappers, problems and describers override it; `WithoutBuiltins` disables it; a status hint loses to it; the wrap text never leaks; gin and fiber give the same bytes.
   - Scenarios: every `error-sentinels` scenario, for its `ErrNotFound` rows only. "Each sentinel maps to a fixed problem" and "Error text" get their `ErrNotFound` row; "Several matches in one chain" gets the `context.DeadlineExceeded` row; "Adapter parity" gets all rows.
   - Hot paths: the sentinel mapper's match and no-match paths, its place before the standard-library built-ins, its removal by `WithoutBuiltins`.
2. **Client errors: `ErrInvalidInput` (400), `ErrUnauthenticated` (401), `ErrPermissionDenied` (403), `ErrConflict` (409).**
   - At its end: four more rows in the catalog table; nothing else changes.
   - Scenarios: their rows in "Sentinel returned directly", "Error text", "Context added by the application stays private", "Logging the full error" and "Sentinel and hint together"; the `ErrConflict`/`ErrNotFound` join row and the `*json.SyntaxError` row of "Several matches in one chain".
   - Hot paths: the order between two sentinels in one chain.
3. **Request-state errors: `ErrPreconditionFailed` (412), `ErrRateLimited` (429).**
   - At its end: two more rows.
   - Scenarios: their rows in "Sentinel returned directly" and "Error text".
   - Hot paths: none new.
4. **Dependency errors: `ErrUnavailable` (503) and `ErrUpstreamTimeout` (504), then `ErrBadGateway` (502).**
   - At its end: the full catalog of ten sentinels. 503 and 504 come first because they are the common "dependency down or slow" cases and interact with the `context.DeadlineExceeded` built-in; 502 is added after them in the same slice, as its own task.
   - Scenarios: their rows in "Sentinel returned directly", "Error text", "Context added by the application stays private" and "Built-ins disabled"; the remaining rows of "Several matches in one chain".
   - Hot paths: the sentinel-before-standard-library order with `context.DeadlineExceeded`.

Every slice is useful on its own: slice 1 already ships a public, documented `ErrNotFound`, and each later slice only adds table rows, exported variables, tests and doc lines.

## Decisions

### D1. Plain sentinel values

```go
// Errors an application returns, or wraps, to describe what went wrong.
var (
	ErrInvalidInput       = errors.New("prblm: invalid input")
	ErrUnauthenticated    = errors.New("prblm: unauthenticated")
	ErrPermissionDenied   = errors.New("prblm: permission denied")
	ErrNotFound           = errors.New("prblm: not found")
	ErrConflict           = errors.New("prblm: conflict")
	ErrPreconditionFailed = errors.New("prblm: precondition failed")
	ErrRateLimited        = errors.New("prblm: rate limited")
	ErrBadGateway         = errors.New("prblm: bad gateway")
	ErrUnavailable        = errors.New("prblm: unavailable")
	ErrUpstreamTimeout    = errors.New("prblm: upstream timeout")
)
```

- They live in a new file, `sentinels.go`, each with a godoc line saying when to return it and which problem it becomes.
- Plain `errors.New` values work with `%w`, `errors.Join` and a domain type's `Is` method, which is how most Go code already classifies errors. The `prblm: ` prefix follows the package's other errors and the Go convention of naming the package in an error string.
- *Alternative:* a `Kind` type with an `errors.As` lookup (upspin `errors.Kind`). Rejected: consumers would have to build their errors around a prblm type, and a domain type couldn't report two kinds.
- *Alternative:* a typed error carrying a status (`&prblm.StatusError{...}`). Rejected: `WithStatus` and `*Problem` already cover that, and a sentinel names an outcome, not a status.
- **Failure modes:**
  - **A consumer compares with `==` after wrapping:** no match in their code; godoc says to use `errors.Is`.
  - **A consumer reassigns an exported variable:** the built-in table keeps the original value, so errors built from the new value don't match. Documented as unsupported, as for `io.EOF`.
  - **Confusion with the hook notes** (`ErrInvalidStatus`, ...): the godoc puts the sentinels in their own group, "Errors to return", and the notes keep their "reported to the hook" wording.
  - **A typed-nil error with an `Is` method that doesn't dereference its receiver:** `errors.Is` reports the sentinel and the response is the sentinel's problem. That's the standard library's behavior, not changed here.

### D2. One table-driven built-in mapper

```go
type sentinel struct {
	err    error
	status int
	detail string
}

var sentinels = []sentinel{ /* ascending status */ }

var builtins = []Mapper{
	MapperFunc(mapSentinel),   // new, first
	As(func(*http.MaxBytesError) *Problem { ... }),
	// ... the existing built-ins, unchanged
}
```

- `mapSentinel` walks the error chain once, in `errors.Is` order, and returns `&Problem{Status: s.status, Detail: s.detail}` for the first error that is or reports a sentinel (the table order breaking a tie within one error), or nil (see D5). Type stays empty (`about:blank`) and title stays empty, so `finalize` fills in the status phrase.
- A fresh problem per match, though the handler clones anyway, so nothing shared is ever handed out.
- One mapper, not one per sentinel: a panicking `Is` method is reported once by this mapper instead of ten times, and adding a sentinel is one table row.
- The table is ordered by ascending status, which is also the godoc order.
- *Alternative:* `Status(ErrNotFound, 404)` style entries, one per sentinel. Rejected: no detail, and ten mappers in the panic case above.
- **Failure modes:**
  - **A custom `Is` method panics:** `safeCall` contains it; the sentinel mapper is no match and the hook gets a `*PanicError`. The standard-library `Is` built-ins call the same method and are contained the same way, so the hook can receive more than one panic note (spec row says "one or more").
  - **nil error:** `Resolve` returns before mapping.
  - **A long `errors.Join` tree:** one walk, ten comparisons per error; no allocation unless a sentinel matches.
  - **Concurrent use:** the table is read-only after package init.

### D3. Names

- **`ErrNotFound`, not `ErrResourceNotFound`.** It reads as `prblm.ErrNotFound`, where "resource" adds nothing; it matches `http.StatusNotFound`, gRPC/connect `NotFound`, Twirp `NotFound` and Kubernetes `StatusReasonNotFound`, and the Go convention of short sentinel names (`fs.ErrNotExist`, `sql.ErrNoRows`).
- **Names describe the condition, not the status phrase,** where the phrase is misleading: `ErrUnauthenticated` for 401 (whose phrase, "Unauthorized", is the well-known misnomer) and `ErrPermissionDenied` for 403, both as in gRPC and connect. Where phrase and condition agree, the shorter one wins: `ErrConflict`, `ErrBadGateway`, `ErrUnavailable`.
- **`ErrUpstreamTimeout`, not `ErrDeadlineExceeded`** or `ErrGatewayTimeout`: the 504 means a dependency was too slow, and `context.DeadlineExceeded` already has its own built-in. `ErrRateLimited` names the cause of a 429.
- **Failure modes:**
  - **A consumer defines their own `ErrNotFound`** in their package: no clash, different package; both can be mapped.
  - **Name confusion between `ErrPreconditionFailed` and google.rpc `FAILED_PRECONDITION`:** godoc states that `ErrPreconditionFailed` is only for conditional request headers (`If-Match`, `If-Unmodified-Since`), and points to `ErrConflict` for "the current state forbids it".

### D4. Catalog scope

- In: the ten sentinels of the proposal.
- **`ErrFailedPrecondition` (400) is not shipped.** Its HTTP status is disputed (400 in grpc-gateway and AIP-193, 412 or 422 elsewhere), its meaning, "the system is not in a state required for the operation", is what `ErrConflict` (409) already says for HTTP, and next to `ErrPreconditionFailed` the two names invite mistakes. It can be added later without breaking anyone; removing it couldn't.
- **`ErrPreconditionFailed` (412) is shipped**, for optimistic concurrency with `If-Match`, the one common case where 412 is right and 409 is wrong.
- **`ErrNotImplemented` (501) is not shipped.** It means the server doesn't support the method or feature at all, which an application rarely decides per request; a stub endpoint can use `WithStatus(err, http.StatusNotImplemented)` or `prblm.Status`. Added later if asked for.
- **No 422 sentinel.** Validation failures belong to `validatormap` or to a consumer problem with field errors; a bare 422 without them would be less useful than 400.
- **Failure modes:**
  - **A consumer needs a status that's not in the catalog:** `WithStatus`, `Status` or a problem, as today.

### D5. Place in the resolution order

- Sentinels are built-ins (step 4). The `error-mapping` resolution order doesn't change, and steps 1 to 3 override them for free.
- Inside step 4 the sentinel mapper runs first, before the standard-library built-ins. A sentinel is the application's explicit statement; a standard-library error in the same chain is usually its cause. `fmt.Errorf("%w: %w", ErrUnavailable, context.DeadlineExceeded)` answers 503, not 504.
- Among sentinels, the first one found wins, in the order `errors.Is` and `errors.As` walk the chain (an error before what it wraps, wrapped errors in the order given). The outermost sentinel is the application's classification and an inner one is usually its cause: a gateway that wraps an upstream's `ErrNotFound` with `ErrBadGateway` answers 502, not 404. Within one error whose `Is` method reports several sentinels, the table order (lowest status) breaks the tie.
  - *Alternative (first shipped, replaced after review):* the lowest status wins anywhere in the chain. Rejected: it can blame the client for a server failure, as in the gateway example.
  - *Alternative:* the highest status wins. Rejected: it ignores what the application said, and a joined validation error plus a down dependency would answer 503, inviting a retry that can never succeed.
  - The walk is `mapSentinel`'s own: it visits each error once and tests it with `==` (safe, since every sentinel's dynamic type is a comparable pointer) and its `Is` method, following `Unwrap() error` and `Unwrap() []error` like `errors.Is`. `errors.Is` alone can't tell where in the chain a match is.
- Sentinels beat a `WithStatus` hint (step 5), in either nesting order, because the hint is the lowest-priority signal by design. This keeps gin's `c.AbortWithError(400, prblm.ErrNotFound)` a 404, consistent with "a mapper still takes precedence over the hint" in `gin-adapter`.
- *Alternative:* append the sentinel mapper after the standard-library built-ins. Rejected for the cause-versus-statement reason above.
- *Alternative:* a new resolution step between built-ins and hints. Rejected: it changes a spec'd order and needs its own opt-out, for no behavior a consumer would notice.
- **Failure modes:**
  - **A consumer relied on a hint while also wrapping a sentinel:** can't happen before this change, since the sentinels don't exist yet. Godoc on `WithStatus` and the sentinels explains the precedence.
  - **A consumer mapper declines (nil) or panics:** the sentinel still applies (spec rows).

### D6. One switch: `WithoutBuiltins`

- `WithoutBuiltins` removes the sentinel mapper with the rest, so its godoc changes from "malformed, truncated and oversized request bodies and deadline errors" to also name the sentinels.
- *Alternative:* a separate `WithoutSentinels`. Rejected for now: no known need, and an option can be added later without breaking anyone.
- **Failure modes:**
  - **A consumer disables built-ins and still returns sentinels:** they resolve to the hint or the 500, as the spec shows; they map them with their own mappers if they want.

### D7. No headers from a sentinel

- A sentinel is a value without fields, so it can't carry `Retry-After` (429, 503) or a `WWW-Authenticate` challenge (401).
- The built-in problems carry no extra header. A consumer adds one with a consumer mapper (`prblm.Is(prblm.ErrRateLimited, ...)` returning a problem with `WithHeader`) or an enricher that checks the status.
- RFC 9110 requires `WWW-Authenticate` on a 401. The library can't know the scheme, so the default 401 doesn't have it. The godoc of `ErrUnauthenticated` says so and shows the consumer-mapper fix.
- *Alternative:* a typed `RetryAfter` error. Deferred to a later change, once there is a concrete need; it would be additive.
- **Failure modes:**
  - **A client retries a 429 or 503 immediately:** expected without `Retry-After`; documented.

## Defaults

| Behavior | Default | Why | Opt-in |
|---|---|---|---|
| Sentinel mapping | on, with the other built-ins | a returned sentinel must work without setup | `WithoutBuiltins()` turns all built-ins off |
| Problem type | `about:blank` | the sentinels are generic outcomes, not application-specific types | a consumer mapper or problem with its own type |
| Title | the status phrase | RFC 9457 `about:blank` semantics | a consumer mapper |
| Detail | a fixed sentence per sentinel | never leak wrap text | a consumer mapper |
| Headers | none | a sentinel carries no value | a consumer mapper with `WithHeader`, or an enricher |
| Precedence | after consumer code, before hints and standard-library built-ins | consumers override; the explicit sentinel beats a hint or an incidental cause | — |

No option loosens a safety default.

## Hot Paths

- `mapSentinel`: the chain walk (single and multiple unwrap), the match return, the no-match return (an unrelated error, a look-alike, a `%v`-formatted sentinel).
- `builtins` order: sentinel mapper before the standard-library entries; `WithoutBuiltins` removing it.
- Resolution through `safeCall` with a panicking `Is` method.
- Response writing for a sentinel problem in JSON and XML through net/http, and through gin and fiber v3 by the parity test.

## Risks / Trade-offs

- [Ten exported names are a permanent commitment] → The catalog is limited to outcomes with an unambiguous HTTP status; disputed ones (`ErrFailedPrecondition`, 501, 422) are left out and can be added later.
- [Confusion with the not-found errors of data layers (`sql.ErrNoRows`, `gorm.ErrRecordNotFound`)] → The godoc explains that the sentinel is for the application's own classification; a data-layer not-found is mapped by the `add-mapper-plugins` plugins. Having plugins produce the sentinels is out of scope.
- [Overlap with `WithStatus`] → Godoc on both: a sentinel names an outcome and brings a fixed detail; a hint only sets a status when nothing else knows the error, and loses to a sentinel.
- [The 401 has no `WWW-Authenticate`, which RFC 9110 requires] → Documented in the godoc with the consumer-mapper fix (D7).
- [The detail is English and fixed] → Consumer mapper to replace it; translation is a non-goal.
- [Leaking wrap text] → The detail never comes from `err.Error()`; leak tests in JSON and XML for wrapped, joined and custom errors.

## Migration Plan

No migration: the names are new, and nothing returns them yet. The sentinels ship in the core's `v0.1.0`. If this change merges before `add-mapper-plugins` releases, that change's release group (its slice 7) includes the sentinels in the `v0.1.0` CHANGELOG entry; if it merges after, the sentinels go into the next minor release. Rollback is `WithoutBuiltins()` for a consumer, or reverting the change before a tag.

## Open Questions

- Whether the godoc example for 401 should be a ready-made helper (for example `prblm.Challenge("Bearer")` as a consumer mapper). It's additive, so it can wait for a request.
