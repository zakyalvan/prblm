# Tasks

## 1. Scaffolding for slice 1

- [ ] 1.1 Create `ginprblm/go.mod` (`github.com/kartaladev/prblm/ginprblm`, `go 1.27`, requiring the released core `v0.1.0` and gin) and add `./ginprblm` to `go.work`; verify `go build ./...` succeeds in the module both with the workspace and with `GOWORK=off`
- [ ] 1.2 Extend CI to a per-module matrix (root, `ginprblm`) running `go vet`, `golangci-lint run`, `go test -race ./...` and `GOWORK=off go test ./...`; verify the pipeline passes
- [ ] 1.3 Add a CI check that `go list -m all` in the root and `ginprblm` modules contains no `gofiber` or `fasthttp`; verify it fails when fiber is temporarily required

## 2. Slice: gin turns a recorded sentinel error into a JSON problem

- [ ] 2.1 Resolve the last recorded error after `c.Next()` (gin-adapter / Scenario: Recorded errors); verify `go test -race -run TestMiddleware_RecordedErrors` passes with `httptest`
  - RED: table test with all four rows (status, `Content-Type`, instance, hook error); fails because `Middleware` does not exist
  - GREEN: `Middleware(h)` building `prblm.Request` from `c.Request`, calling `Respond` with `c.Errors.Last().Err`, copying `Response` into `c.Writer`
  - REFACTOR: extract "copy Response to writer" (Extract Function) for reuse by Recovery; /simplify
- [ ] 2.2 Never leak unmapped text (gin-adapter / Scenario: Unmapped error text never leaks); verify `go test -run TestMiddleware_NoLeak` passes
  - RED: both text rows
  - GREEN: none expected beyond 2.1 (core fallback); if red, fix the adapter, not the test
  - REFACTOR: /simplify
- [ ] 2.3 Contain faulty consumer code (gin-adapter / Scenario: Faulty consumer code under gin); verify `go test -race -run TestMiddleware_FaultyConsumer` passes and a follow-up request on the same engine succeeds
  - RED: four faulty rows
  - GREEN: the D2 guard around `Respond`
  - REFACTOR: /simplify
- [ ] 2.4 Reject a nil handler at construction (gin-adapter / Scenario: Nil handler, error-middleware row); verify `go test -run TestMiddleware_NilHandler` passes
- [ ] 2.5 Write the `ginprblm` package godoc with `ExampleMiddleware`; verify `go test -run Example` passes
- [ ] 2.6 Verify slice 2 end to end with an `httptest.Server` and check `go tool cover -func` covers every branch of `Middleware` introduced so far; run gopls `go_diagnostics` on changed files, `-race`, vet, lint and `GOWORK=off go test ./...`

## 3. Slice: gin abort semantics (deferred-header writer, once the early commit is proven)

- [ ] 3.1 Prove that gin commits the response header on abort before deciding to build the deferred writer (CLAUDE.md rule 8; design D3); verify `go test -run TestGinAbort_CommitsHeaderEarly` FAILS against a plain middleware, and record the failing output in the task notes
  - RED: `TestGinAbort_CommitsHeaderEarly` with a middleware that, after `c.Next()`, sets `Content-Type: application/problem+json` and writes a 404 problem body; handler rows `c.AbortWithStatus(401)` and `c.AbortWithError(403, ErrX)`, using `httptest.NewRecorder` and an `httptest.Server`; assert the client sees status 404 and `Content-Type: application/problem+json`
  - Expected: the test fails (client sees 401/403 and no problem `Content-Type`), which proves the defect and gates tasks 3.2 onward
  - If the test passes instead: the claim is not confirmed. Stop the slice, drop the deferred writer (D3 and tasks 3.2 and 3.7), and re-plan the slice with `/opsx:update` before continuing
  - The test stays in the suite and later asserts the fixed behavior through `Middleware`
- [ ] 3.2 Record instead of committing on `WriteHeader`/`WriteHeaderNow` (design D3); verify `go test -run TestDeferredWriter_Record` passes
  - RED: unit test proving `AbortWithStatus(401)` leaves the underlying recorder uncommitted and `Status()` reports 401; fails with gin's writer
  - GREEN: `deferredWriter` embedding `gin.ResponseWriter`, installed before `c.Next()`
  - REFACTOR: /simplify
- [ ] 3.3 Use the abort status as a hint with mapper precedence (gin-adapter / Scenario: AbortWithError); verify `go test -run TestMiddleware_AbortWithError` passes
  - RED: hint-used, mapper-wins and invalid-hint rows, asserting `Content-Type: application/problem+json`
  - GREEN: `WithStatus(err, recorded)` when recorded is 400 or above
  - REFACTOR: /simplify
- [ ] 3.4 Turn status-only aborts into problems and pass other statuses through (gin-adapter / Scenario: AbortWithStatus and plain statuses); verify `go test -run TestMiddleware_StatusOnly` passes
  - RED: problem rows and pass-through rows (204, 304, 200, JSON 200)
  - GREEN: `errStatusOnly` sentinel path and commit-unchanged path
  - REFACTOR: express the post-`Next` decision as a small, ordered switch (Decompose Conditional); /simplify
- [ ] 3.5 Keep written bodies and report errors (gin-adapter / Scenario: Body written before or after an error); verify `go test -run TestMiddleware_BodyWritten` passes
  - RED: three rows with hook assertions
  - GREEN: committed branch calling `Respond` for the hook only
  - REFACTOR: /simplify
- [ ] 3.6 Skip disconnected clients (gin-adapter / Scenario: Client went away); verify `go test -run TestMiddleware_Canceled` passes
- [ ] 3.7 Detect early-committed headers from middleware registered before ours (design D3 failure mode); verify `go test -run TestMiddleware_RegisteredLate` passes: nothing malformed is written and the hook is called
- [ ] 3.8 Document abort semantics and the registration-order rule in godoc and README; verify `ExampleMiddleware_abort` passes
- [ ] 3.9 Verify slice 3 end to end and check `go tool cover -func` covers every `deferredWriter` method and every post-`Next` branch; run gopls diagnostics, `-race`, vet, lint, `GOWORK=off`

## 4. Slice: gin streams and hijacked connections pass through

- [ ] 4.1 Commit on first write and flush (gin-adapter / Scenario: Streaming and hijacking, SSE, flush and `WriteHeaderNow` rows); verify `go test -run TestDeferredWriter_Commit` passes against an `httptest.Server`, asserting `data: 1` arrives before the handler returns
  - RED: the three rows
  - GREEN: commit in `Write`, `WriteString`, `Flush`, `CloseNotify`, `Pusher`, and at the end of the middleware
  - REFACTOR: single `commit()` used by every path (Extract Function); /simplify
- [ ] 4.2 Never write after a hijack (gin-adapter / Scenario: Streaming and hijacking, hijack row); verify `go test -run TestDeferredWriter_Hijack` passes with a real websocket-style upgrade over `httptest.Server`
  - RED: hijack row with hook assertion
  - GREEN: delegate `Hijack`, set `hijacked`, hook-only branch
  - REFACTOR: /simplify
- [ ] 4.3 Isolate concurrent requests (gin-adapter / Scenario: Concurrent gin requests); verify `go test -race -run TestMiddleware_Concurrent` passes with 200 requests
- [ ] 4.4 Verify slice 4 end to end and check `go tool cover -func` covers all commit paths; run gopls diagnostics, `-race`, vet, lint, `GOWORK=off`

## 5. Slice: gin Scope and Recovery

- [ ] 5.1 Add `Scope` (gin-adapter / Scenario: Route group scope, Scenario: Degenerate scopes); verify `go test -run TestScope` passes
  - RED: route rows and degenerate rows
  - GREEN: `Scope(m...)` with nil filtering and pass-through
  - REFACTOR: /simplify
- [ ] 5.2 Add `Recovery` working in any order (gin-adapter / Scenario: Recovery placement); verify `go test -run TestRecovery_Placement` passes
  - RED: all five chain rows
  - GREEN: `Recovery(h)` per design D5, writing through `c.Writer`, `c.Abort()`, never `c.Error`
  - REFACTOR: reuse the copy function from 2.1; /simplify
- [ ] 5.3 Give the hook value and stack (gin-adapter / Scenario: Panic details go to the hook); verify `go test -run TestRecovery_Hook` passes
- [ ] 5.4 Handle recovery edge cases (gin-adapter / Scenario: Recovery edge cases; Scenario: Nil handler, recovery row); verify `go test -run 'TestRecovery_(Edge|NilHandler)'` passes
  - RED: abort re-panic, partial flush, `EPIPE`, panicking enricher rows; nil handler panic
  - GREEN: committed/broken branch, construction check
  - REFACTOR: /simplify
- [ ] 5.5 Document `Scope` and `Recovery` (godoc, `ExampleRecovery`, recommendation to replace `gin.Recovery()`); verify `go test -run Example` passes
- [ ] 5.6 Verify slice 5 end to end (route groups + panics) and check `go tool cover -func` covers `Scope` and every `Recovery` branch; run gopls diagnostics, `-race`, vet, lint, `GOWORK=off`

## 6. Slice: fiber ErrorHandler

- [ ] 6.1 Create `fiberprblm/go.mod` (`github.com/kartaladev/prblm/fiberprblm`, `go 1.27`, requiring the core `v0.1.0` and the pinned fiber v3 minor), add it to `go.work` and the CI matrix; verify `go build ./...` with and without `GOWORK`
- [ ] 6.2 Render returned errors (fiber-adapter / Scenario: Returned errors, Scenario: Unmapped error text never leaks); verify `go test -race -run TestErrorHandler_Returned` passes using `app.Test`
  - RED: four returned-error rows and both leak rows; fails because `ErrorHandler` does not exist
  - GREEN: `ErrorHandler(h)` per design D6 (request facts, `Respond` on `c.Context()`, write status/headers/body)
  - REFACTOR: extract "copy Response to fiber.Ctx" (Extract Function); /simplify
- [ ] 6.3 Use fiber error codes as hints without messages (fiber-adapter / Scenario: Fiber-generated and handler fiber errors, Scenario: Mapper takes precedence over a fiber error code); verify `go test -run TestErrorHandler_FiberErrors` passes
  - RED: routing rows (404, 405), handler-returned rows, precedence row
  - GREEN: `errors.AsType[*fiber.Error]` then `WithStatus`
  - REFACTOR: /simplify
- [ ] 6.4 Describe the request from fiber (fiber-adapter / Scenario: Request facts); verify `go test -run TestErrorHandler_RequestFacts` passes
- [ ] 6.5 Replace buffered bodies and keep streams (fiber-adapter / Scenario: Handler wrote before returning an error); verify `go test -run TestErrorHandler_Buffered` passes
  - RED: three rows with hook assertions
  - GREEN: reset buffered body; streaming detection and hook-only exit
  - REFACTOR: /simplify
- [ ] 6.6 Skip disconnected clients, contain faulty consumer code, reject nil handler (fiber-adapter / Scenario: Client went away, Scenario: Faulty consumer code under fiber, Scenario: Nil handler); verify `go test -race -run 'TestErrorHandler_(Canceled|Faulty|Nil)'` passes
- [ ] 6.7 Isolate concurrent requests (fiber-adapter / Scenario: Concurrent fiber requests); verify `go test -race -run TestErrorHandler_Concurrent` passes over a real listener with 200 requests
- [ ] 6.8 Write the `fiberprblm` package godoc with `ExampleErrorHandler`; verify `go test -run Example` passes
- [ ] 6.9 Verify slice 6 end to end and check `go tool cover -func` covers every `ErrorHandler` branch; run gopls diagnostics, `-race`, vet, lint, `GOWORK=off`

## 7. Slice: fiber Scope and Recovery

- [ ] 7.1 Add `Scope` (fiber-adapter / Scenario: Route group scope, Scenario: Degenerate scopes); verify `go test -run TestScope` passes
  - RED: route rows and degenerate rows
  - GREEN: `Scope(m...)` with `c.SetContext`
  - REFACTOR: /simplify
- [ ] 7.2 Add `Recovery` returning a safe error (fiber-adapter / Scenario: Recovery with the adapter's error handler); verify `go test -run TestRecovery_ErrorHandler` passes
  - RED: four chain rows including value, stack and `io.ErrClosedPipe` matching
  - GREEN: `Recovery()` with named return, `recoveredError` wrapping `*prblm.PanicError`
  - REFACTOR: /simplify
- [ ] 7.3 Never leak the panic through fiber's default error handler (fiber-adapter / Scenario: Recovery with fiber's default error handler); verify `go test -run TestRecovery_DefaultHandler` passes
- [ ] 7.4 Document `Scope` and `Recovery` (godoc, `ExampleRecovery`, pairing with `ErrorHandler`); verify `go test -run Example` passes
- [ ] 7.5 Verify slice 7 end to end and check `go tool cover -func` covers `Scope` and `Recovery` branches; run gopls diagnostics, `-race`, vet, lint, `GOWORK=off`

## 8. Slice: cross-adapter parity and documentation

- [ ] 8.1 Create the unpublished test module `internal/paritytest` (requires core, `ginprblm`, `fiberprblm`) and add it to `go.work`; verify `go build ./...` succeeds in the workspace
- [ ] 8.2 Compare outputs across net/http, gin and fiber (gin-adapter / Scenario: Same output as net/http; fiber-adapter / Scenario: Same output as net/http); verify `go test -race ./internal/paritytest` passes
  - RED: one table test, one row per parity Examples row, three servers sharing one `prblm.Handler`, comparing status, `Content-Type`, `Vary` and body bytes
  - GREEN: fix adapter header or body differences until identical (never special-case the test)
  - REFACTOR: /simplify
- [ ] 8.3 Add README sections for gin and fiber (install, quick start, abort semantics, registration order, recovery, framework differences table); verify each code block is mirrored by a compiling `Example`
- [ ] 8.4 Update `RELEASING.md` with the `ginprblm/vX.Y.Z` and `fiberprblm/vX.Y.Z` tags; verify `GOWORK=off go list -m` resolves each module against a local tag

## Workflow follow-up

- Archive the change after review and once `ginprblm/v0.1.0` and `fiberprblm/v0.1.0` are tagged.
