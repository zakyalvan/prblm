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

- [ ] 4.1 Turn discovery on only with registered types (type-discovery / Scenario: Discovery state); verify `go test -run TestDiscovery_State` passes
  - RED: every on and off row, asserting `DiscoveryPath()` and the status from `Discovery()`; fails because neither method nor `WithoutDiscovery` exists
  - GREEN: `WithoutDiscovery() Option`, `DiscoveryPath()`, and `Discovery()` returning a handler that answers 404 problems when off
  - REFACTOR: /simplify
- [ ] 4.2 Pre-render and serve a type's documentation (type-discovery / Scenario: Documentation served, all three tables); verify `go test -race -run TestDiscovery_Documentation` passes
  - RED: every HTML, JSON and malformed-Accept row against `Discovery()` over `httptest`
  - GREEN: `html/template` and JSON pages rendered once in `NewHandler`, plus an HTML-or-JSON negotiator over the existing Accept parser (design D4)
  - REFACTOR: look for Divergent Change between the page rendering and the routing (Split Phase: render at construction, route at request), then /simplify
- [ ] 4.3 Answer HEAD with headers only (type-discovery / Scenario: HEAD request); verify `go test -run TestDiscovery_Head` passes
- [ ] 4.4 Serve the index sorted by sub-type with links (type-discovery / Scenario: Index served, Scenario: HTML index links); verify `go test -run TestDiscovery_Index` passes
  - RED: both JSON rows and both link rows, including the absolute-namespace `href`
  - GREEN: index pages rendered once, from definitions sorted by sub-type
  - REFACTOR: /simplify
- [ ] 4.5 Answer unknown paths with 404 problems without calling the hook (type-discovery / Scenario: Unknown type); verify `go test -run TestDiscovery_Unknown` passes
  - RED: every path row, including `%2e%2e` and `%00`, asserting the hook was called 0 times
  - GREEN: exact lookup after the prefix; reject when `EscapedPath` differs from `Path`; render the 404 without `report`
  - REFACTOR: /simplify
- [ ] 4.6 Refuse other methods with 405 (type-discovery / Scenario: Wrong method); verify `go test -run TestDiscovery_Method` passes
- [ ] 4.7 Escape hostile definition text and set the security headers (type-discovery / Scenario: Hostile definition text, Scenario: Security headers); verify `go test -run 'TestDiscovery_(Hostile|Headers)'` passes
  - RED: every hostile row, and every header row for doc HTML, doc JSON and index HTML
  - GREEN: rely on `html/template` contextual escaping and `encoding/json` HTML escaping; set the headers in one place
  - REFACTOR: Extract Function for the header set, then /simplify
- [ ] 4.8 Make documentation cacheable by browsers (type-discovery / Scenario: Caching headers, Scenario: Conditional requests, Scenario: ETag changes with the definition); verify `go test -race -run 'TestDiscovery_(Caching|Conditional|ETag)'` passes
  - RED: every cacheable and not-cacheable row; every revalidated and full-response `If-None-Match` row, including weak, `*`, list and malformed values; the two ETag-relation rows
  - GREEN: `Cache-Control` and a SHA-256-based strong `ETag` computed with each pre-rendered page; an `If-None-Match` matcher using weak comparison; 304 without a body
  - REFACTOR: look for Data Clumps (body, content type, ETag), then Introduce Parameter Object `page`, then /simplify
- [ ] 4.9 Serve the namespace path for absolute namespaces (type-discovery / Scenario: Absolute namespace); verify `go test -run TestDiscovery_AbsoluteNamespace` passes
- [ ] 4.10 Mount discovery on an `http.ServeMux` next to other routes (type-discovery / Scenario: net/http mount); verify `go test -run TestDiscovery_Mount` passes
- [ ] 4.11 Stay correct under concurrency (type-discovery / Scenario: Concurrent documentation requests); verify `go test -race -run TestDiscovery_Concurrent` passes with 300 requests
- [ ] 4.12 Note undocumented sub-types (problem-types / Scenario: Undocumented sub-type); verify `go test -run TestResolve_Undocumented` passes
  - RED: the five rows; fails because `ErrUndocumentedType` doesn't exist
  - GREEN: exported `ErrUndocumentedType`; the check in `resolveType` when discovery is on
  - REFACTOR: reuse the note helper from 2.6, then /simplify
- [ ] 4.13 Document discovery (godoc, `ExampleHandler_Discovery` mounting on a `ServeMux`, README "Discovering problem types", including the absolute-namespace routing note and why types are never built from `Host`); verify `go test -run Example` passes
- [ ] 4.14 Verify slice 4 end to end over `httptest.Server`. Check `go tool cover -func` covers every branch of the discovery handler, the negotiator and the page rendering, and run diagnostics, vet, lint, `-race` and `GOWORK=off`.

## 5. Slice: the gin middleware serves discovery for unclaimed paths

- [ ] 5.1 Prove that gin runs `engine.Use` middleware for unmatched paths with an empty `c.FullPath()`, including when `engine.NoRoute` is set (CLAUDE.md rule 8; design D5); verify `go test -run TestGinNoRoute_RunsGlobalMiddleware` passes, and record the result in the task notes. If it fails, stop and re-plan slice 5 (fallback in D5).
- [ ] 5.2 Serve discovery from `Middleware` (gin-adapter / Scenario: Discovery through gin, all rows); verify `go test -race -run TestMiddleware_Discovery` passes
  - RED: every served and not-served row, including the `NoRoute` rows, the route-wins row and the group-only row
  - GREEN: the pre-`Next` branch from design D5, serving through the core `Discovery()` handler and calling `c.Abort()`
  - REFACTOR: look for Long Function in `Middleware` (Extract Function `serveDiscovery`), then /simplify
- [ ] 5.3 Document gin discovery (godoc, `ExampleMiddleware_discovery`, README note that the middleware must be installed with `engine.Use`); verify `go test -run Example` passes
- [ ] 5.4 Verify slice 5 end to end. Check `go tool cover -func` covers every discovery branch in `Middleware`, and run diagnostics, vet, lint, `-race` and `GOWORK=off` in `ginprblm`.

## 6. Slice: the fiber error handler serves discovery on route-not-found

- [ ] 6.1 Prove how fiber v3 lets the error handler tell a router 404 from a handler returning `fiber.ErrNotFound` (CLAUDE.md rule 8; design D6); verify `go test -run TestFiberNotFound_Distinguishable` passes, and record the signal used in the task notes. If none is reliable, apply the D6 fallback and record why.
- [ ] 6.2 Serve discovery from `ErrorHandler` (fiber-adapter / Scenario: Discovery through fiber, all rows); verify `go test -race -run TestErrorHandler_Discovery` passes using `app.Test`
  - RED: every served and not-served row, including the handler-returned `fiber.ErrNotFound` row
  - GREEN: the discovery branch from design D6, with a minimal recording writer copied to `c`
  - REFACTOR: Extract Function `serveDiscovery`, then /simplify
- [ ] 6.3 Document fiber discovery (godoc, `ExampleErrorHandler_discovery`); verify `go test -run Example` passes
- [ ] 6.4 Verify slice 6 end to end. Check `go tool cover -func` covers every discovery branch in `ErrorHandler` and the recording writer, and run diagnostics, vet, lint, `-race` and `GOWORK=off` in `fiberprblm`.

## 7. Slice: parity across net/http, gin and fiber, and documentation

- [ ] 7.1 Extend the parity tables (gin-adapter and fiber-adapter / Scenario: Same output as net/http, all nine rows); verify `go test -race ./internal/paritytest` passes
  - RED: add the `ErrBlacklisted`, documentation (JSON and HTML) and unknown-type rows, with net/http mounting `Discovery()` at `"GET " + DiscoveryPath() + "/"`
  - GREEN: fix any byte difference in the adapter that diverges. Each difference found is first proven by the failing row (rule 8).
  - REFACTOR: /simplify
- [ ] 7.2 Add a README "Problem types" walkthrough (namespace, definitions, mapper, discovery for net/http, gin and fiber) and a framework-differences row for discovery serving; verify every code block is mirrored by a compiling Example
- [ ] 7.3 Run `go_vulncheck` and confirm no new findings; run all four modules' suites with `-race`, and `GOWORK=off` for the three published modules

## Workflow follow-up

- Run `/opsx:verify add-problem-types`, then archive.
- Implement `add-mapper-plugins` next. Its release slice tags the adapter API this change introduces.
