# Tasks

## 1. Slice: `ErrNotFound` answers a 404 problem over net/http with zero configuration

- [ ] 1.1 Confirm the starting point: on `main`, `go build ./...`, `go test -race ./...` and `GOWORK=off go test ./...` pass in the root module; run gopls `go_symbol_references` on `builtins` and `WithoutBuiltins` to list every place that depends on the built-in list; verify the commands pass
- [ ] 1.2 Map a returned `ErrNotFound` to its problem (error-sentinels / Scenario: Sentinel returned directly, row `prblm.ErrNotFound`); verify `go test -race -run TestSentinels_Direct` passes
  - RED: `TestSentinels_Direct`, a table test over `NewHandler()` + `Wrap` via `httptest`, asserting status, `Content-Type`, type, title and detail; fails to compile because `ErrNotFound` doesn't exist, then (with the variable alone) fails with 500
  - GREEN: `sentinels.go` with `ErrNotFound`; the `sentinel` table and `mapSentinel` in `builtins.go`, placed first in `builtins`
  - REFACTOR: keep the table the single source of status and detail (no switch on the error); /simplify
- [ ] 1.3 Match the sentinel anywhere in the chain and nowhere else (error-sentinels / Scenario: Wrapped, joined and custom errors); verify `go test -race -run TestSentinels_Chain` passes
  - RED: all matched and not-matched rows, with a test-only `*VesselMissingError` whose `Is` method reports `ErrNotFound`, and the `%v` look-alike row
  - GREEN: nothing beyond `errors.Is`; fix any gap the rows reveal
  - REFACTOR: share the request helper with 1.2 (Extract Function); /simplify
- [ ] 1.4 Keep wrap text out of the response (error-sentinels / Scenario: Context added by the application stays private, `ErrNotFound` rows in JSON and XML, and the `*VesselMissingError` row); verify `go test -race -run TestSentinels_NoLeak` passes
  - RED: assert none of the forbidden strings appear in the body, for both formats
  - GREEN: confirm the detail only ever comes from the table
  - REFACTOR: /simplify
- [ ] 1.5 Hand the original error to the hook (error-sentinels / Scenario: Logging the full error, `ErrNotFound` row); verify `go test -race -run TestSentinels_Hook` passes
  - RED: hook records the error text and problem status
  - GREEN: nothing expected; fix any gap
  - REFACTOR: /simplify
- [ ] 1.6 Give the sentinel its error string (error-sentinels / Scenario: Error text, `ErrNotFound` row); verify `go test -run TestSentinels_ErrorText` passes
  - RED: a table over the exported sentinels, also asserting every string starts with `prblm: ` and is unique
  - GREEN: the `prblm: not found` string, if 1.2 chose another
  - REFACTOR: /simplify
- [ ] 1.7 Let consumer code override the sentinel (error-sentinels / Scenario: Overriding the default problem, all rows); verify `go test -race -run TestSentinels_Override` passes
  - RED: consumer `Is` and `Status` mappers, a scoped mapper, a problem and a describer in the chain; failure rows for a consumer mapper returning nil, a panicking consumer mapper (hook gets the panic, then the error) and a describer returning nil
  - GREEN: nothing expected, since steps 1 to 3 already run first; fix any gap
  - REFACTOR: /simplify
- [ ] 1.8 Let the sentinel beat a status hint (error-sentinels / Scenario: Sentinel and hint together, `ErrNotFound` rows); verify `go test -race -run TestSentinels_Hint` passes
  - RED: hint 400 outside, hint 200 (ignored) and the built-ins-disabled row where the hint wins
  - GREEN: nothing expected; the built-ins already run before `hinted`
  - REFACTOR: /simplify
- [ ] 1.9 Disable the sentinel with `WithoutBuiltins` (error-sentinels / Scenario: Built-ins disabled, `ErrNotFound` rows); verify `go test -race -run TestSentinels_WithoutBuiltins` passes
  - RED: no-mappers row (500, no detail) and consumer-mapper row (404, no detail)
  - GREEN: nothing expected, since the mapper is in `builtins`; update the `WithoutBuiltins` godoc to name the sentinels
  - REFACTOR: /simplify
- [ ] 1.10 Run the sentinel before the standard-library built-ins (error-sentinels / Scenario: Several matches in one chain, row `context.DeadlineExceeded`); verify `go test -race -run TestSentinels_Order` passes and the existing `error-mapping` built-in tests still pass
  - RED: the bare `context.DeadlineExceeded` row stays 504 without detail
  - GREEN: nothing expected; fix the order if it fails
  - REFACTOR: /simplify
- [ ] 1.11 Survive degenerate errors (error-sentinels / Scenario: Degenerate inputs); verify `go test -race -run TestSentinels_Degenerate` passes
  - RED: nil, `WithStatus(nil, 404)`, typed-nil `*MyError` and a panicking `Is` method (hook gets one or more `*PanicError`, then the error; response 500 without detail)
  - GREEN: nothing expected, since `safeCall` contains the panic; fix any gap
  - REFACTOR: /simplify
- [ ] 1.12 Share the sentinel safely across requests (error-sentinels / Scenario: Concurrent requests); verify `go test -race -count=3 -run TestSentinels_Concurrent` passes
  - RED: 1 and 100 concurrent requests with a `trace_id` enricher; each body has its own `trace_id` and no vessel id
  - GREEN: nothing expected; the handler clones resolved problems
  - REFACTOR: /simplify
- [ ] 1.13 Prove adapter parity (error-sentinels / Scenario: Adapter parity, all rows); verify `go test -race ./...` passes in `internal/paritytest`
  - RED: add the `ErrNotFound` rows to `TestParity`, including gin's `c.AbortWithError(400, prblm.ErrNotFound)` answering 404
  - GREEN: nothing expected in the adapters, which resolve through the core; fix any gap
  - REFACTOR: /simplify
- [ ] 1.14 Document `ErrNotFound`: godoc on the sentinel group ("Errors to return", when to use `errors.Is`, not to reassign, the difference from `WithStatus` and from `sql.ErrNoRows`), the built-ins step in `doc.go` and in the `WithStatus` godoc, `ExampleErrNotFound`, a "Returning common errors" README section with the catalog table and a row in the README defaults table, and a CHANGELOG `[Unreleased]` entry; verify `go test -run Example` passes and every README snippet is mirrored by a compiling example
- [ ] 1.15 Verify slice 1 end to end (a `Wrap` server answering `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` with a 404 problem in JSON and XML) and check `go test -coverprofile=c.out ./... && go tool cover -func=c.out` covers `mapSentinel`'s match and no-match paths; run `gofmt`, `go vet`, `golangci-lint run ./...`, `go test -race ./...` and `GOWORK=off go test ./...` in the root module and `go test -race ./...` in `internal/paritytest`; run gopls `go_diagnostics` on changed files

## 2. Slice: client errors (400, 401, 403, 409)

- [ ] 2.1 Map `ErrInvalidInput`, `ErrUnauthenticated`, `ErrPermissionDenied` and `ErrConflict` (error-sentinels / Scenario: Sentinel returned directly, Scenario: Error text, their rows); verify `go test -race -run 'TestSentinels_(Direct|ErrorText)'` passes
  - RED: add the four rows to both tables; they fail to compile, then fail with 500
  - GREEN: the four variables and their table rows, in ascending status order
  - REFACTOR: /simplify
- [ ] 2.2 Keep their wrap text private and log it (error-sentinels / Scenario: Context added by the application stays private, `ErrPermissionDenied` rows in JSON and XML; Scenario: Logging the full error, `ErrConflict` row); verify `go test -race -run 'TestSentinels_(NoLeak|Hook)'` passes
  - RED: the new rows
  - GREEN: nothing expected; fix any gap
  - REFACTOR: /simplify
- [ ] 2.3 Let `ErrConflict` beat a nested hint (error-sentinels / Scenario: Sentinel and hint together, `ErrConflict` row); verify `go test -race -run TestSentinels_Hint` passes
  - RED: the hint-inside row
  - GREEN: nothing expected
  - REFACTOR: /simplify
- [ ] 2.4 Resolve chains with several matches (error-sentinels / Scenario: Several matches in one chain, the `ErrConflict`/`ErrNotFound` join row and the `ErrInvalidInput` + `*json.SyntaxError` row); verify `go test -race -run TestSentinels_Order` passes
  - RED: both rows; the second fails if the sentinel mapper doesn't run before the `*json.SyntaxError` built-in
  - GREEN: confirm the table order and the place of the sentinel mapper
  - REFACTOR: /simplify
- [ ] 2.5 Document the four sentinels (when to use each; `ErrUnauthenticated` has no `WWW-Authenticate` and how to add one with a consumer mapper; `ErrPermissionDenied` versus `ErrNotFound` for hiding existence), add them to the README catalog table and the CHANGELOG entry, with `ExampleErrUnauthenticated` showing the consumer-mapper fix; verify `go test -run Example` passes
- [ ] 2.6 Verify slice 2 end to end and check `go tool cover -func` still covers every branch of `mapSentinel`; run `gofmt`, `go vet`, `golangci-lint run ./...`, `go test -race ./...` and `GOWORK=off go test ./...` in the root module; run gopls `go_diagnostics` on changed files

## 3. Slice: request-state errors (412, 429)

- [ ] 3.1 Map `ErrPreconditionFailed` and `ErrRateLimited` (error-sentinels / Scenario: Sentinel returned directly, Scenario: Error text, their rows); verify `go test -race -run 'TestSentinels_(Direct|ErrorText)'` passes
  - RED: the two rows in both tables
  - GREEN: the two variables and table rows
  - REFACTOR: /simplify
- [ ] 3.2 Document them (`ErrPreconditionFailed` only for conditional request headers, `ErrConflict` for state conflicts; `ErrRateLimited` has no `Retry-After` and how to add one with a consumer mapper or enricher), add them to the README catalog table and the CHANGELOG entry; verify `go test -run Example` passes
- [ ] 3.3 Verify slice 3 end to end and check `go tool cover -func`; run `gofmt`, `go vet`, `golangci-lint run ./...`, `go test -race ./...` and `GOWORK=off go test ./...` in the root module; run gopls `go_diagnostics` on changed files

## 4. Slice: dependency errors (503, 504, then 502)

- [ ] 4.1 Map `ErrUnavailable` and `ErrUpstreamTimeout` (error-sentinels / Scenario: Sentinel returned directly, Scenario: Error text, their rows); verify `go test -race -run 'TestSentinels_(Direct|ErrorText)'` passes
  - RED: the two rows in both tables
  - GREEN: the two variables and table rows
  - REFACTOR: /simplify
- [ ] 4.2 Keep dependency details private (error-sentinels / Scenario: Context added by the application stays private, `ErrUnavailable` row) and disable them with the built-ins (Scenario: Built-ins disabled, `ErrUnavailable` row); verify `go test -race -run 'TestSentinels_(NoLeak|WithoutBuiltins)'` passes
  - RED: the new rows; the body must contain neither the address nor `dial tcp`
  - GREEN: nothing expected
  - REFACTOR: /simplify
- [ ] 4.3 Prefer the sentinel over `context.DeadlineExceeded` and order 4xx before 5xx (error-sentinels / Scenario: Several matches in one chain, the remaining rows); verify `go test -race -run TestSentinels_Order` passes
  - RED: the `ErrUpstreamTimeout` and `ErrUnavailable` rows wrapping `context.DeadlineExceeded`, and the `ErrUnavailable`/`ErrInvalidInput` join row
  - GREEN: confirm the order
  - REFACTOR: /simplify
- [ ] 4.4 Map `ErrBadGateway` (error-sentinels / Scenario: Sentinel returned directly, Scenario: Error text, `ErrBadGateway` rows); verify `go test -race -run 'TestSentinels_(Direct|ErrorText)'` passes
  - RED: the two rows
  - GREEN: the variable and table row
  - REFACTOR: /simplify
- [ ] 4.5 Document the three sentinels (a dependency is down, slow, or answered badly; no `Retry-After` on 503; `ErrUpstreamTimeout` versus a bare `context.DeadlineExceeded`), complete the README catalog table and the CHANGELOG entry; verify `go test -run Example` passes
- [ ] 4.6 Verify the full catalog end to end (every sentinel through net/http in JSON and XML) and check `go tool cover -func` covers `mapSentinel` and the `builtins` order; run `gofmt`, `go vet`, `golangci-lint run ./...`, `go test -race ./...` and `GOWORK=off go test ./...` in the root module, `go test -race ./...` in `ginprblm`, `fiberprblm` and `internal/paritytest`; run gopls `go_diagnostics` on changed files

## Workflow follow-up

- Archive the change after review, once group 4 is done.
- If `add-mapper-plugins` hasn't released yet, its release group includes the sentinels in the core's `v0.1.0` CHANGELOG entry; otherwise they ship in the next minor release.
