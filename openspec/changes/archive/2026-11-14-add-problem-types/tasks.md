# Tasks

Every slice ends with:
- gopls `go_diagnostics` on the changed files
- `go_symbol_references` checked before any definition is changed
- `go vet`, `golangci-lint run` and `go test -race ./...` in every module the slice touches
- `GOWORK=off go test ./...` for the core, `ginprblm` and `fiberprblm`

## 1. Slice: the gin and fiber adapters are configured with core options

- [x] 1.1 Build the gin middleware from options (gin-adapter / Scenario: Options configure the middleware); verify `go test -race -run TestMiddleware_Options` passes in `ginprblm`
  - RED: `TestMiddleware_Options` with rows (none) → 500, `WithMappers(...)` → 404, nil options around a mapper → 404, a single `nil` → 500; fails to compile because `Middleware` takes `*prblm.Handler`. Before changing the signature, run `go_symbol_references` on `Middleware`.
  - GREEN: `Middleware(opts ...prblm.Option)` calls `prblm.NewHandler(opts...)` once and keeps the rest unchanged. Update every existing test and Example to the options form, and delete `TestMiddleware_NilHandler` and `mustExist`'s Middleware path (the removed requirement).
  - REFACTOR: look for Shotgun Surgery in the test helpers that built handlers (Extract Function `newEngine(opts ...prblm.Option)`), then /simplify
- [x] 1.2 Build the gin recovery from options and share options with the middleware (gin-adapter / Scenario: Shared options across middleware and recovery); verify `go test -race -run 'TestRecovery_Options|TestRecovery_'` passes
  - RED: `TestRecovery_Options` with the rows for a recorded `ErrVesselNotFound` (404, hook called 1 time) and a `"boom"` panic (500, hook called 1 time) through `Recovery(opts...)` and `Middleware(opts...)`; fails to compile on the old signature
  - GREEN: `Recovery(opts ...prblm.Option)`; delete `TestRecovery_NilHandler` and `mustExist`
  - REFACTOR: Inline Function on any wrapper left without a reason, then /simplify
- [x] 1.3 Build the fiber error handler from options (fiber-adapter / Scenario: Options configure the error handler); verify `go test -race -run TestErrorHandler_Options` passes in `fiberprblm`
  - RED: `TestErrorHandler_Options` with the same four rows using `app.Test`; fails to compile on the old signature
  - GREEN: `ErrorHandler(opts ...prblm.Option)`; delete `TestErrorHandler_Nil` and the nil check
  - REFACTOR: align the test helpers with gin's (Extract Function), then /simplify
- [x] 1.4 Build the parity servers from one options slice (gin-adapter and fiber-adapter / Scenario: Same output as net/http, the five existing rows); verify `go test -race ./internal/paritytest` passes
  - RED: change `newHandler()` to `newOptions()` returning `[]prblm.Option` and build all three servers from it; fails to compile until the adapter calls change
  - GREEN: `prblm.NewHandler(opts...)` for net/http, `ginprblm.Middleware(opts...)` and `ginprblm.Recovery(opts...)` for gin, and `fiberprblm.ErrorHandler(opts...)` for fiber
  - REFACTOR: remove the duplicated server setup (Extract Function), then /simplify
- [x] 1.5 Update the godoc, Examples and README gin/fiber sections to the options form; verify `go test -run Example` passes in both modules and every README code block is mirrored by a compiling Example
- [x] 1.6 Verify slice 1 end to end: the full suites pass in all four modules, `go tool cover -func` shows `Middleware`, `Recovery` and `ErrorHandler` fully covered, and diagnostics, vet, lint, `-race` and `GOWORK=off` are clean

## 2. Slice: relative sub-types resolve against a namespace, end to end

- [x] 2.1 Validate the namespace at construction (problem-types / Scenario: Namespace validated); verify `go test -race -run TestWithNamespace_Validation` passes in the core
  - RED: `TestWithNamespace_Validation` with every accepted and rejected row, asserting a panic whose message names `WithNamespace`; fails because `WithNamespace` doesn't exist
  - GREEN: `WithNamespace(ns string) Option` stores the raw value. `NewHandler` validates it after all options with `url.Parse` (design D2), trims one trailing `/` and panics on failure.
  - REFACTOR: look for Long Function in the validator (Extract Function per form: absolute URI, absolute path), then /simplify
- [x] 2.2 Accept or reject sub-types with an allocation-free scanner (problem-types / Scenario: Invalid relative type, the "malformed sub-types" rows as unit cases); verify `go test -run TestIsSubtype` passes
  - RED: `TestIsSubtype`, an internal test table of valid sub-types (`contracts/blacklisted-supplier`, `a/b/c-2`, `v2/limits/rate.exceeded`, `x`) and every malformed row; fails because `isSubtype` doesn't exist
  - GREEN: a byte scanner implementing the grammar in design D2
  - REFACTOR: Replace Nested Conditional with Guard Clauses in the scanner, then /simplify. Add `FuzzIsSubtype`, asserting that an accepted sub-type never contains `..`, `//`, `?`, `#` or uppercase letters.
- [x] 2.3 Resolve relative sub-types and keep everything else (problem-types / Scenario: Type resolved); verify `go test -race -run TestResolve_Namespace` passes
  - RED: `TestResolve_Namespace` with every resolved and kept row through `httptest` and `Wrap`; fails because the type is passed through
  - GREEN: `resolveType` in `complete`, after `enrich`, followed by a second `finalize` (design D2)
  - REFACTOR: look for Feature Envy between `resolveType` and `Problem` (Move Function if the type rules belong on `Problem`), then /simplify
- [x] 2.4 Keep pass-through without a namespace (problem-types / Scenario: No namespace); verify `go test -run TestResolve_NoNamespace` passes
  - RED: `TestResolve_NoNamespace` with the three rows, asserting no hook note. If it already passes, record that and keep it as a regression test.
  - GREEN: early return in `resolveType`
  - REFACTOR: none expected; /simplify
- [x] 2.5 Ignore request host headers (problem-types / Scenario: Hostile host headers); verify `go test -run TestResolve_HostHeaders` passes
  - RED: `TestResolve_HostHeaders` with the three rows
  - GREEN: none expected beyond 2.3. Keep the test as a security regression.
  - REFACTOR: /simplify
- [x] 2.6 Replace invalid relative types with about:blank and note them (problem-types / Scenario: Invalid relative type, both tables); verify `go test -race -run TestResolve_InvalidType` passes
  - RED: `TestResolve_InvalidType` with every malformed row and the self-describing, scoped-mapper and enricher rows, asserting status 409, title `Locked`, no `type` member, and a hook note wrapping `ErrInvalidType` before the main call; fails because `ErrInvalidType` doesn't exist
  - GREEN: exported `ErrInvalidType`; `resolveType` clears the type and calls `report`
  - REFACTOR: look for Duplicated Code with the `ErrInvalidStatus` note (Extract Function `note(ctx, p, err)`), then /simplify
- [x] 2.7 Survive a panicking hook on the note (problem-types / Scenario: Faulty hook on a type note); verify `go test -run TestResolve_InvalidType_FaultyHook` passes
  - RED: the two hook rows
  - GREEN: none expected (`report` recovers). Keep the test as a regression.
  - REFACTOR: /simplify
- [x] 2.8 Resolve every problem source on the request's copy (problem-types / Scenario: Problem sources, Scenario: Shared problem stays relative); verify `go test -race -run 'TestResolve_Sources|TestResolve_SharedProblem'` passes
  - RED: the four source rows, then 1 and 200 concurrent requests sharing one package-level problem, asserting it still has type `contracts/locked`
  - GREEN: none expected if 2.3 resolves on the copy; otherwise move resolution after the clone
  - REFACTOR: /simplify
- [x] 2.9 Reject an invalid namespace in the adapters at construction (gin-adapter and fiber-adapter / Scenario: Invalid options fail at construction, the namespace rows); verify `go test -run 'TestMiddleware_InvalidOptions|TestErrorHandler_InvalidOptions'` passes
  - RED: the namespace rows for the error middleware, the recovery middleware and the fiber error handler
  - GREEN: none expected (`NewHandler` panics)
  - REFACTOR: /simplify
- [x] 2.10 Document `WithNamespace` and `ErrInvalidType` with their reasons (godoc, `ExampleWithNamespace` for both forms, README "Problem types" section); verify `go test -run Example` passes
- [x] 2.11 Verify slice 2 end to end through net/http, gin and fiber: add a temporary parity check of the resolved `ErrBlacklisted` row (kept in 7.1). Check `go tool cover -func` covers every branch of `resolveType`, `isSubtype` and the namespace validator, and run diagnostics, vet, lint, `-race` and `GOWORK=off`.

## 3. Slice: type definitions build problems and register with the handler

- [x] 3.1 Define types and validate the definitions (problem-types / Scenario: Definition validated); verify `go test -run TestDefineType_Validation` passes
  - RED: `TestDefineType_Validation` with every accepted and rejected row, asserting panic messages that name `DefineType` and the offending value; fails because `DefineType` doesn't exist
  - GREEN: `Type` with unexported fields, `DefineType` reusing `isSubtype` and `isErrorStatus`, and the read-only accessors
  - REFACTOR: Introduce Parameter Object only if the constructor grows past four parameters; otherwise /simplify
- [x] 3.2 Build problems from a definition (problem-types / Scenario: Problem from a definition, Scenario: Each problem is a fresh copy); verify `go test -race -run 'TestType_New'` passes
  - RED: the four field rows, compared as JSON bodies through `Wrap` with namespace `/v1/api/probs`, plus the fresh-copy row
  - GREEN: `(*Type).New(fields ...Field) *Problem`
  - REFACTOR: /simplify
- [x] 3.3 Answer a nil definition with the safe 500 (problem-types / Scenario: Nil definition); verify `go test -run TestType_New_Nil` passes
  - RED: the two rows; fails with a nil-pointer panic
  - GREEN: a nil-receiver guard returning `New(500)`, with no fields applied, so `WithDetail("secret")` never leaks
  - REFACTOR: /simplify
- [x] 3.4 Register definitions and reject duplicates or a missing namespace (problem-types / Scenario: Registration validated); verify `go test -run TestWithTypes_Validation` passes
  - RED: every accepted and rejected row, including option order and a nil type
  - GREEN: `WithTypes(types ...*Type) Option` appends. `NewHandler` builds the `types` map after all options and panics as specified (design D3).
  - REFACTOR: look for Long Function in `NewHandler` (Extract Function `validate()`), then /simplify
- [x] 3.5 Reject `WithTypes` without a namespace in the adapters (gin-adapter and fiber-adapter / Scenario: Invalid options fail at construction, the `WithTypes` rows); verify the construction tests pass
- [x] 3.6 Document `DefineType`, `Type.New` and `WithTypes` (godoc with the reasons, `ExampleDefineType` showing a package-level definition used in `prblm.Is`); verify `go test -run Example` passes
- [x] 3.7 Verify slice 3 end to end with a mapper returning `BlacklistedSupplier.New()` through net/http, gin and fiber. Check `go tool cover -func` covers `DefineType`, `Type.New` and the `WithTypes` validation, and run diagnostics, vet, lint, `-race` and `GOWORK=off`.

## 4. Slice: discovery serves type documentation from the core, mounted in net/http

- [x] 4.1 Turn discovery on only with registered types (type-discovery / Scenario: Discovery state); verify `go test -run TestDiscovery_State` passes
  - RED: every on and off row, asserting `DiscoveryPath()` and the status from `Discovery()`; fails because neither method nor `WithoutDiscovery` exists
  - GREEN: `WithoutDiscovery() Option`, `DiscoveryPath()`, and `Discovery()` returning a handler that answers 404 problems when off
  - REFACTOR: /simplify
- [x] 4.2 Pre-render and serve a type's documentation (type-discovery / Scenario: Documentation served, all three tables); verify `go test -race -run TestDiscovery_Documentation` passes
  - RED: every HTML, JSON and malformed-Accept row against `Discovery()` over `httptest`
  - GREEN: `html/template` and JSON pages rendered once in `NewHandler`, plus an HTML-or-JSON negotiator over the existing Accept parser (design D4)
  - REFACTOR: look for Divergent Change between the page rendering and the routing (Split Phase: render at construction, route at request), then /simplify
- [x] 4.3 Answer HEAD with headers only (type-discovery / Scenario: HEAD request); verify `go test -run TestDiscovery_Head` passes
- [x] 4.4 Serve the index sorted by sub-type with links (type-discovery / Scenario: Index served, Scenario: HTML index links); verify `go test -run TestDiscovery_Index` passes
  - RED: both JSON rows and both link rows, including the absolute-namespace `href`
  - GREEN: index pages rendered once, from definitions sorted by sub-type
  - REFACTOR: /simplify
- [x] 4.5 Answer unknown paths with 404 problems without calling the hook (type-discovery / Scenario: Unknown type); verify `go test -run TestDiscovery_Unknown` passes
  - RED: every path row, including `%2e%2e` and `%00`, asserting the hook was called 0 times
  - GREEN: exact lookup after the prefix; reject when `EscapedPath` differs from `Path`; render the 404 without `report`
  - REFACTOR: /simplify
- [x] 4.6 Refuse other methods with 405 (type-discovery / Scenario: Wrong method); verify `go test -run TestDiscovery_Method` passes
- [x] 4.7 Escape hostile definition text and set the security headers (type-discovery / Scenario: Hostile definition text, Scenario: Security headers); verify `go test -run 'TestDiscovery_(Hostile|Headers)'` passes
  - RED: every hostile row, and every header row for doc HTML, doc JSON and index HTML
  - GREEN: rely on `html/template` contextual escaping and `encoding/json` HTML escaping; set the headers in one place
  - REFACTOR: Extract Function for the header set, then /simplify
- [x] 4.8 Make documentation cacheable by browsers (type-discovery / Scenario: Caching headers, Scenario: Conditional requests, Scenario: ETag changes with the definition); verify `go test -race -run 'TestDiscovery_(Caching|Conditional|ETag)'` passes
  - RED: every cacheable and not-cacheable row; every revalidated and full-response `If-None-Match` row, including weak, `*`, list and malformed values; the two ETag-relation rows
  - GREEN: `Cache-Control` and a SHA-256-based strong `ETag` computed with each pre-rendered page; an `If-None-Match` matcher using weak comparison; 304 without a body
  - REFACTOR: look for Data Clumps (body, content type, ETag), then Introduce Parameter Object `page`, then /simplify
- [x] 4.9 Serve the namespace path for absolute namespaces (type-discovery / Scenario: Absolute namespace); verify `go test -run TestDiscovery_AbsoluteNamespace` passes
- [x] 4.10 Mount discovery on an `http.ServeMux` next to other routes (type-discovery / Scenario: net/http mount); verify `go test -run TestDiscovery_Mount` passes
- [x] 4.11 Stay correct under concurrency (type-discovery / Scenario: Concurrent documentation requests); verify `go test -race -run TestDiscovery_Concurrent` passes with 300 requests
- [x] 4.12 Note undocumented sub-types (problem-types / Scenario: Undocumented sub-type); verify `go test -run TestResolve_Undocumented` passes
  - RED: the five rows; fails because `ErrUndocumentedType` doesn't exist
  - GREEN: exported `ErrUndocumentedType`; the check in `resolveType` when discovery is on
  - REFACTOR: reuse the note helper from 2.6, then /simplify
- [x] 4.13 Document discovery (godoc, `ExampleHandler_Discovery` mounting on a `ServeMux`, README "Discovering problem types", including the absolute-namespace routing note and why types are never built from `Host`); verify `go test -run Example` passes
- [x] 4.14 Verify slice 4 end to end over `httptest.Server`. Check `go tool cover -func` covers every branch of the discovery handler, the negotiator and the page rendering, and run diagnostics, vet, lint, `-race` and `GOWORK=off`.

## 5. Slice: the gin middleware serves discovery for unclaimed paths

- [x] 5.1 Prove that gin runs `engine.Use` middleware for unmatched paths with an empty `c.FullPath()`, including when `engine.NoRoute` is set (CLAUDE.md rule 8; design D5); verify `go test -run TestGinNoRoute_RunsGlobalMiddleware` passes, and record the result in the task notes. If it fails, stop and re-plan slice 5 (fallback in D5).
  - Result (gin v1.12.0): confirmed. `TestGinNoRoute_RunsGlobalMiddleware` in `ginprblm/noroute_test.go` passes: `engine.Use` middleware runs for unmatched paths with an empty `c.FullPath()`, with no `NoRoute` handler and with one registered before or after `Use`; a matched route has a non-empty `FullPath()`. Slice 5 proceeds as designed; the D5 fallback is not needed.
- [x] 5.2 Serve discovery from `Middleware` (gin-adapter / Scenario: Discovery through gin, all rows); verify `go test -race -run TestMiddleware_Discovery` passes
  - RED: every served and not-served row, including the `NoRoute` rows, the route-wins row and the group-only row
  - GREEN: the pre-`Next` branch from design D5, serving through the core `Discovery()` handler and calling `c.Abort()`
  - REFACTOR: look for Long Function in `Middleware` (Extract Function `serveDiscovery`), then /simplify
- [x] 5.3 Document gin discovery (godoc, `ExampleMiddleware_discovery`, README note that the middleware must be installed with `engine.Use`); verify `go test -run Example` passes
- [x] 5.4 Verify slice 5 end to end. Check `go tool cover -func` covers every discovery branch in `Middleware`, and run diagnostics, vet, lint, `-race` and `GOWORK=off` in `ginprblm`.

## 6. Slice: the fiber error handler serves discovery on route-not-found

- [x] 6.1 Prove how fiber v3 lets the error handler tell a router 404 from a handler returning `fiber.ErrNotFound` (CLAUDE.md rule 8; design D6); verify `go test -run TestFiberNotFound_Distinguishable` passes, and record the signal used in the task notes. If none is reliable, apply the D6 fallback and record why.
  - Result (fiber v3.7.0): the router returns the sentinel `fiber.ErrNotFound` itself (router.go), so the error value cannot tell a router 404 from a handler returning `fiber.ErrNotFound`, and the identity half of the D6 fallback would not work. The reliable signal is `c.Matched()`, true only when an endpoint route (not `app.Use` middleware) matched: `TestFiberNotFound_Distinguishable` in `fiberprblm/notfound_test.go` passes for a router 404 with and without middleware, and for a handler returning `fiber.ErrNotFound` with and without middleware. `ErrorHandler` serves discovery when `!c.Matched()` and the error is `fiber.ErrNotFound`.
- [x] 6.2 Serve discovery from `ErrorHandler` (fiber-adapter / Scenario: Discovery through fiber, all rows); verify `go test -race -run TestErrorHandler_Discovery` passes using `app.Test`
  - RED: every served and not-served row, including the handler-returned `fiber.ErrNotFound` row
  - GREEN: the discovery branch from design D6, with a minimal recording writer copied to `c`
  - REFACTOR: Extract Function `serveDiscovery`, then /simplify
- [x] 6.3 Document fiber discovery (godoc, `ExampleErrorHandler_discovery`); verify `go test -run Example` passes
- [x] 6.4 Verify slice 6 end to end. Check `go tool cover -func` covers every discovery branch in `ErrorHandler` and the recording writer, and run diagnostics, vet, lint, `-race` and `GOWORK=off` in `fiberprblm`.

## 7. Slice: parity across net/http, gin and fiber, and documentation

- [x] 7.1 Extend the parity tables (gin-adapter and fiber-adapter / Scenario: Same output as net/http, all nine rows); verify `go test -race ./internal/paritytest` passes
  - RED: add the `ErrBlacklisted`, documentation (JSON and HTML) and unknown-type rows, with net/http mounting `Discovery()` at `"GET " + DiscoveryPath() + "/"`
  - GREEN: fix any byte difference in the adapter that diverges. Each difference found is first proven by the failing row (rule 8).
  - REFACTOR: /simplify
- [x] 7.2 Add a README "Problem types" walkthrough (namespace, definitions, mapper, discovery for net/http, gin and fiber) and a framework-differences row for discovery serving; verify every code block is mirrored by a compiling Example
- [x] 7.3 Run `go_vulncheck` and confirm no new findings; run all four modules' suites with `-race`, and `GOWORK=off` for the three published modules

## 8. Slice: review fixes (verify and PR #3 code review)

Each task fixes one finding in the working system. Each starts from a failing test that reproduces the finding (rule 8), and the test stays as the RED step.

- [x] 8.1 Prove that gin records 404 for an unmatched path and 405 for a path with a route only for another method under `engine.HandleMethodNotAllowed`, before the global middleware runs (CLAUDE.md rule 8; design D5); verify `go test -run TestGinNoMethod_RecordsStatus` passes in `ginprblm`, and record the result in design D5. If it fails, stop and re-plan with the D5 fallback.
- [x] 8.2 Reject invalid definitions in `WithTypes` (problem-types / Scenario: Registration validated, the `&Type{}` rows); verify `go test -run TestWithTypes_Validation` passes
  - RED: add the two `&Type{}` rows; they fail because construction succeeds.
  - GREEN: extract `validateType(subtype, status, title) error` from `DefineType` and call it for every definition in `checkTypes`.
  - REFACTOR: Extract Function done in GREEN; check the `DefineType` messages are unchanged; /simplify
- [x] 8.3 Make nil and zero-value definitions safe (problem-types / Scenario: Nil definition, the zero-value rows; Scenario: Nil definition accessors); verify `go test -run 'TestType_New_Nil|TestType_NilAccessors'` passes
  - RED: the zero-value rows fail (status 0 normalized, detail sent); the accessor rows panic.
  - GREEN: `New` returns the fallback when `t == nil || t.subtype == ""`; the accessors return zero values on a nil receiver.
- [x] 8.4 Use the last `WithNamespace` and validate it in `NewHandler` (problem-types / Scenario: Namespace option order); verify `go test -race -run 'TestWithNamespace_Validation|TestWithNamespace_Order'` passes in the core, and the adapters' invalid-option tests still pass
  - RED: the `WithNamespace("bad"), WithNamespace("/v1/api/probs")` row fails with a panic (reproduced during verify).
  - GREEN: `WithNamespace` stores the raw value and sets a "namespace given" flag; `NewHandler` runs `mustParseNamespace` once, after all options, before `checkTypes`.
  - REFACTOR: update the `WithNamespace` and `NewHandler` godoc; /simplify
- [x] 8.5 Reset the title when an invalid type becomes about:blank (problem-types / Scenario: Invalid relative type, all rows now expecting `Conflict` and the detail); verify `go test -race -run 'TestResolve_InvalidType'` passes
  - RED: change the expected title to `Conflict` and add the detail; the rows fail with `Locked`.
  - GREEN: clear `p.Title` with `p.Type` in `resolveType`, so the second `finalize` sets the status phrase.
- [x] 8.6 Note relative types without a namespace (problem-types / Scenario: No namespace, Scenario: Problem from a definition without a namespace); verify `go test -run 'TestResolve_NoNamespace|TestType_New_NoNamespace'` passes
  - RED: the "receives" rows fail because no note is sent.
  - GREEN: in `resolveType`, without a namespace, report `ErrInvalidType` for a type that `isKeptType` does not keep, and keep the type. Update the `ErrInvalidType` godoc to name both cases and the reason.
- [x] 8.7 Build type notes only when a hook is set, and resolve sub-types without rebuilding them (problem-types / Scenario: Type notes cost nothing without a hook; design D2); verify `go test -run 'TestResolve_NoHookAllocs|TestTypeCache'` passes
  - RED: `testing.AllocsPerRun` shows the extra allocations of `fmt.Errorf` for each row.
  - GREEN: check `h.onError != nil` before building any note in `resolveType` (`noteType`).
  - Found while going GREEN: the undocumented row still allocated once more than `about:blank`, because resolving any sub-type concatenates `ns + "/" + subtype` (measured: a documented sub-type with no note also allocates 3, against 2 for `about:blank`). Decided with the user: keep the scenario as written. Resolve registered sub-types once in `NewHandler`, and cache other valid sub-types in a bounded, copy-on-write cache (`TestTypeCache_Resolve`, `_Bounded`, `_Concurrent`, `_NoAllocs`).
- [x] 8.8 Keep discovery problems away from the hook (type-discovery / Scenario: Discovery problems never reach the hook); verify `go test -race -run TestDiscovery_SilentHook` passes
  - RED: the panicking-enricher and unencodable-extension rows record a note.
  - GREEN: discovery completes and renders its problems under an unexported context marker that `report` honors.
- [x] 8.9 Add `DiscoveryRequest` and `Handler.RespondDiscovery`, and rebuild `Discovery()` on it (type-discovery / Scenario: Discovery response for a request); verify `go test -race -run 'TestRespondDiscovery|TestDiscovery_'` passes, all existing discovery tests included
  - RED: `TestRespondDiscovery` with one case per row; it fails to compile because the method doesn't exist. Run `go_symbol_references` on `serveDiscovery` and `lookup` first.
  - GREEN: implement the method-, path- and `%`-rules in `RespondDiscovery`; `Discovery()` answers 405 for other methods, 404 for a `false` result, and writes the result otherwise.
  - REFACTOR: Inline the old `lookup` path checks into the single rule; /simplify
- [x] 8.10 Render discovery pages on first use (type-discovery / Scenario: Pages rendered on first use; design D1 and D4); verify `go test -race -run 'TestDiscovery_FirstUse|TestDiscovery_Concurrent'` passes, and that `ginprblm.Recovery` with `WithTypes` renders nothing (an internal test counting renders)
  - RED: the render-count test fails because `NewHandler` renders eagerly.
  - GREEN: keep the documented sub-type set eager; render the pages under a `sync.Once` triggered by `DiscoveryPath`, `Discovery` and `RespondDiscovery`.
- [x] 8.11 Serve gin discovery through `RespondDiscovery` only on a recorded 404 (gin-adapter / Scenario: Discovery through gin, the `HandleMethodNotAllowed` rows); verify `go test -race -run TestMiddleware_Discovery` passes
  - RED: the 405 rows return the documentation (reproduced in the PR #3 code review).
  - GREEN: check `c.FullPath() == ""` and `c.Writer.Status() == http.StatusNotFound`, then call `RespondDiscovery` with `c.Request.URL.EscapedPath()`; delete the adapter's own method and path checks.
- [x] 8.12 Serve fiber discovery through `RespondDiscovery` (fiber-adapter / Scenario: Discovery through fiber, the method-not-allowed and `%zz` rows); verify `go test -race -run 'TestErrorHandler_Discovery|TestFiberNotFound_Distinguishable'` passes
  - RED: add the two rows; check whether they fail before the refactor, and record the result.
  - Result: both rows pass before the refactor, so neither is a defect in fiber (the `%zz` row already existed as "malformed escape"; the new "route for another method" row gets fiber's `ErrMethodNotAllowed`, which never reached discovery). They stay as regression rows, and the change is a pure refactor: `isDiscovery`, `recorder` and the `http.Request` conversion are deleted, and `ErrorHandler` calls `RespondDiscovery` with the original path.
  - GREEN: call `RespondDiscovery` with the raw request path and the two headers; delete `recorder`, the `http.Request` conversion and the adapter's own path checks.
- [x] 8.13 Fix the stale `Resolve` godoc (type resolution now runs after the enrichers) and document `RespondDiscovery`, `DiscoveryRequest`, the no-namespace note and the title reset (godoc, README "Problem types", CHANGELOG); verify `go test -run Example` passes in all modules
- [x] 8.14 Cover the `xmlenc` decode guard: extract `decodeTree(b []byte) (any, error)` from `Encode` and test it with malformed JSON; verify `go tool cover -func` shows `internal/xmlenc` at 100%
- [x] 8.15 Verify slice 8 end to end: the parity test still passes byte for byte, `go tool cover -func` covers every new branch (`resolveType`, `validateType`, `RespondDiscovery`, the gin and fiber discovery branches), and diagnostics, vet, lint, `-race` and `GOWORK=off` are clean in all four modules

### Review findings: shadowed documentation and HEAD (from `/opsx:verify` and `/code-review high`)

- [x] 8.16 Prove that a fiber `app.Use` middleware sees the endpoint route after `c.Next()` returns: `c.Matched()` is true and `c.Route().Path` is the route's pattern for a matched GET and HEAD, and `c.Matched()` is false for an unmatched path (CLAUDE.md rule 8; design D6); verify `go test -run TestFiberMiddleware_SeesRoute` passes in `fiberprblm`, and record the result in design D6. If it fails, stop and re-plan the fiber middleware.
- [x] 8.17 Note routes that hide documentation in the core (type-discovery / Scenario: Shadowed documentation, both tables); verify `go test -race -run TestNoteShadowedRoute` passes
  - RED: `TestNoteShadowedRoute` with one case per row, calling `NoteShadowedRoute` with the route and a `DiscoveryRequest` per request; it fails to compile because neither the method nor `ErrShadowedDocumentation` exists.
  - GREEN: exported `ErrShadowedDocumentation` and `(*Handler).NoteShadowedRoute`; the on/off, method and prefix checks first, then the lookup (a page or the index, not a 404), then the once-per-route set under a mutex, capped at 256, and the note through `isReporting`/`callHook` with a nil problem.
  - Murphy rows (internal tests): no hook (nothing built), a nil `*Handler` panics naming the method, a panicking hook, more than 256 conflicting routes (at most 256 notes), and 100 concurrent first requests to one route (one note) under `-race`.
  - REFACTOR: share the path checks with `RespondDiscovery` (Extract Function if they duplicate), then /simplify.
- [x] 8.18 Report shadowed documentation from gin, and keep the HEAD behavior (gin-adapter / Scenario: Discovery through gin, the HEAD row; Scenario: Shadowed documentation through gin); verify `go test -race -run 'TestMiddleware_Discovery|TestMiddleware_Shadowed'` passes
  - The HEAD row: the review's reproducing test, turned into a row that asserts the specified behavior (status 200, `application/json`, empty body). It passes before any change; record that and keep it as a regression row.
  - RED: `TestMiddleware_Shadowed` with one case per row; the "1" rows fail because no note is sent.
  - GREEN: in `Middleware`, when discovery is on and `c.FullPath()` is non-empty, call `NoteShadowedRoute` with the pattern and the request.
  - Code review claim (unverified): a global middleware before `Middleware` that calls `c.Status(...)` stops discovery being served. Write the test first (rule 8). If it fails as claimed, document the ordering rule (install `Middleware` first) in the godoc and README and keep the test; if it passes, drop the claim and record that in design D5.
- [x] 8.19 Add the fiber discovery middleware (fiber-adapter / Scenario: Shadowed documentation through fiber; Scenario: Discovery through fiber, the HEAD row; Scenario: Invalid options fail at construction, the discovery middleware row); verify `go test -race -run 'TestDiscovery_Shadowed|TestErrorHandler_Discovery|TestErrorHandler_InvalidOptions'` passes in `fiberprblm`
  - RED: the rows fail to compile because `fiberprblm.Discovery` doesn't exist. The HEAD row passes already (verified during the review); keep it as a regression row.
  - GREEN: `Discovery(opts ...prblm.Option) fiber.Handler` builds its handler with `prblm.NewHandler(opts...)`, calls `c.Next()`, reports `c.Route().Path` to `NoteShadowedRoute` when `c.Matched()`, and returns the error of `c.Next()` unchanged.
  - REFACTOR: share the `DiscoveryRequest` building with `serveDiscovery` (Extract Function), then /simplify.
- [x] 8.20 Document shadowed documentation: the `ErrShadowedDocumentation` and `NoteShadowedRoute` godoc with the reasons; the `OnError` godoc (a note can arrive on a request that succeeded, with no answered error after it); the gin `Middleware` godoc (the HEAD behavior and the note); `fiberprblm.Discovery` godoc and `ExampleDiscovery`; README "Discovering problem types" (the conflict, the note, net/http documented only) and the framework-differences table (a row for reporting shadowed documentation); CHANGELOG; verify `go test -run Example` passes in all modules and every README code block is mirrored by a compiling Example
- [x] 8.21 Verify the review-findings additions end to end: the parity test still passes byte for byte; `go tool cover -func` covers every branch of `NoteShadowedRoute`, the gin report and the fiber `Discovery` middleware; diagnostics, vet, lint, `-race` and `GOWORK=off` are clean in all four modules

## Workflow follow-up

- Run `/opsx:verify add-problem-types` again.
- Merge PR #3 (`feat/problem-types`) into `main` before archiving, so the archived `tasks.md` shows every task done.
- Then archive.
- Implement `add-mapper-plugins` next. Its release slice tags the adapter API this change introduces.
