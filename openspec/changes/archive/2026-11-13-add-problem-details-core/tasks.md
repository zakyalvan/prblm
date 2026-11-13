# Tasks

## 1. Scaffolding for slice 1

- [x] 1.1 Create `go.mod` (`github.com/kartaladev/prblm`, `go 1.27`), `doc.go` with the package comment, and `go.work` listing `.`; verify `go build ./...` and `go vet ./...` succeed
- [x] 1.2 Add `.golangci.yml` (golangci-lint v2 with the linters chosen per the golang-lint skill) and a CI workflow running `go vet`, `golangci-lint run`, `go test -race ./...` and `GOWORK=off go test ./...`; verify the pipeline passes on the scaffold
- [x] 1.3 Add a CI check that the root module has no third-party requirements (`go list -m all` prints only the module itself); verify it fails when a dependency is temporarily added

## 2. Slice: sentinel error renders as a JSON problem over net/http

- [x] 2.1 Encode standard members as JSON in fixed order, omitting empty ones (problem-document / Scenario: Members serialized to JSON); verify `go test -run TestProblem_MarshalJSON` passes
  - RED: `TestProblem_MarshalJSON` with the "all members" and "empty members omitted" rows, comparing exact bytes; fails because `Problem` does not exist
  - GREEN: `Problem` struct with `MarshalJSON` writing type, title, status, detail, instance
  - REFACTOR: look for duplicated "write member if non-empty" code (Extract Function); /simplify
- [x] 2.2 Resolve with consumer mappers and the safe fallback (error-mapping / Scenario: Match or no match, Scenario: Unmapped error text never leaks); verify `go test -run 'TestResolve_(Match|Fallback)'` passes
  - RED: table tests with the mapped row, the unrelated row and the three leak rows; fails because `Handler` does not exist
  - GREEN: `Mapper`, `MapperFunc`, `Status`, `NewHandler(WithMappers)`, `Resolve` with consumer loop + fallback 500
  - REFACTOR: name the resolution steps (Extract Function per step) so later steps slot in; /simplify
- [x] 2.3 Write problems from error-returning handlers (stdlib-adapter / Scenario: Handler result, rows: returns mapped, writes 200 and returns nil, writes nothing, returns unmapped); verify `go test -run TestWrap` passes with `httptest`
  - RED: `TestWrap` with those four rows checking status, `Content-Type` and body; fails because `Wrap` does not exist
  - GREEN: `Request`, `Response`, `Respond` (JSON only), `Wrap` copying `Response` to the writer, instance from `r.URL.Path`
  - REFACTOR: keep `Respond` free of net/http types except `http.Header` (Move Function); /simplify
- [x] 2.4 Document slice 1 in the package godoc with a runnable `ExampleHandler_Wrap`; verify `go test -run Example` passes
- [x] 2.5 Verify slice 1 end to end with an `httptest.Server` round trip, then check `go tool cover -func` shows `Resolve`, `Respond`, `MarshalJSON` and `Wrap` with every branch covered; verify `go test -race ./...`, `go vet` and `golangci-lint` are clean

## 3. Slice: complete problem model

- [x] 3.1 Apply the `about:blank` default and omit it from output (problem-document / Scenario: Effective type); verify `go test -run TestProblem_EffectiveType` passes
  - RED: three rows (empty, explicit `about:blank`, custom type)
  - GREEN: effective-type accessor and omission in `MarshalJSON`
  - REFACTOR: single source of truth for the default constant; /simplify
- [x] 3.2 Finalize the title from the status for `about:blank` (problem-document / Scenario: Title finalized); verify `go test -run TestProblem_Finalize` passes
  - RED: "filled from status" and "kept" rows
  - GREEN: `finalize()` using `http.StatusText`, called by `Resolve`
  - REFACTOR: /simplify
- [x] 3.3 Implement `Error`, nil-safety and `Unwrap` with a hidden cause (problem-document / Scenario: Error message and cause, Scenario: Nil problem); verify `go test -run TestProblem_Error` passes
  - RED: message rows, `errors.Is` rows, nil-pointer row; and a check that the JSON never contains the cause text
  - GREEN: `Error()`, `Unwrap()`, `WithCause`
  - REFACTOR: /simplify
- [x] 3.4 Validate extension names (problem-document / Scenario: Extension name check); verify `go test -run TestProblem_Set` passes and `go test -fuzz FuzzExtensionName -fuzztime 30s` finds no accepted name that breaks the rule
  - RED: all accepted and rejected rows, including the non-ASCII and reserved rows
  - GREEN: `Set`, `Ext`, unexported `validName`, reserved set; `WithExt` panicking at construction for invalid names
  - REFACTOR: express the rule once and reuse it for XML later (Extract Function); /simplify
- [x] 3.5 Encode extensions flat, sorted, with JSON rules and failures (problem-document / Scenario: Extensions serialized flat and sorted, Scenario: Extension value encoding); verify `go test -run TestProblem_MarshalJSON_Extensions` passes
  - RED: flat/sorted rows, tagged-struct row, `nil` row, and the chan / NaN / cycle failure rows
  - GREEN: extensions loop in `MarshalJSON` returning the first value error
  - REFACTOR: avoid re-sorting per call if profiling shows it matters (Replace Temp with Query); /simplify
- [x] 3.6 Carry response headers that are never serialized (problem-document / Scenario: Header not in body); verify `go test -run TestProblem_Header` passes
  - RED: `Retry-After` and `WWW-Authenticate` rows
  - GREEN: `Header` field and `WithHeader`
  - REFACTOR: /simplify
- [x] 3.7 Decode JSON leniently (problem-document / Scenario: Decode JSON); verify `go test -run TestProblem_UnmarshalJSON` passes and `go test -fuzz FuzzUnmarshalJSON -fuzztime 30s` never panics
  - RED: tolerated and rejected rows
  - GREEN: `UnmarshalJSON` via `map[string]json.RawMessage`, standard members type-checked, the rest into extensions
  - REFACTOR: /simplify
- [x] 3.8 Document the problem model (godoc for `Problem`, `New`, field options, `Set`) with `ExampleNew`; verify `go test -run Example` passes
- [x] 3.9 Verify slice 3's working system end to end (a mapper returning a problem with extensions and headers through `Wrap`), and check `go tool cover -func` covers `MarshalJSON`, `UnmarshalJSON`, `Set`, `finalize` and `Error` branches; run `-race`, vet and lint

## 4. Slice: complete resolution chain

- [x] 4.1 Add `Is` and `As[T]` helpers (error-mapping / Scenario: Sentinel helper, Scenario: Typed helper exposes the value); verify `go test -run 'TestIs|TestAs'` passes
  - RED: wrapped, joined and unrelated rows; typed rows; a construction panic for `Is(nil, ...)`
  - GREEN: `Is` with `errors.Is`, `As[T]` with `errors.AsType[T]`
  - REFACTOR: express `Status` through `Is` (Substitute Algorithm); /simplify
- [x] 4.2 Resolve self-describing errors first (error-mapping / Scenario: Problem or describer in the chain); verify `go test -run TestResolve_SelfDescribing` passes
  - RED: all five rows including mapper call counts and the nil-describer row
  - GREEN: `Describer` and step 1 of `Resolve`
  - REFACTOR: /simplify
- [x] 4.3 Add the status hint with range checks (error-mapping / Scenario: Hint used when unmapped, Scenario: Nested and wrapped hints); verify `go test -run TestWithStatus` passes
  - RED: valid, ignored, nested and nil rows; secret text never in the body
  - GREEN: `WithStatus`, `statusError`, step 5
  - REFACTOR: /simplify
- [x] 4.4 Add request-scoped mappers (error-mapping / Scenario: Scoped only on its route, Scenario: Nested scopes); verify `go test -run TestScopeMappers` passes, including a `-race` test that nests scopes concurrently from one parent context
  - RED: route rows, nested row, parent-unchanged assertion
  - GREEN: `ScopeMappers` with a fresh slice per call; step 2
  - REFACTOR: /simplify
- [x] 4.5 Add the built-in mappers and `WithoutBuiltins` (error-mapping / Scenario: Built-in mapping); verify `go test -run TestBuiltins` passes
  - RED: enabled and disabled rows with exact details
  - GREEN: unexported built-in mappers; step 4
  - REFACTOR: table-drive the built-ins themselves if they repeat (Replace Conditional with Polymorphism is not needed; prefer data); /simplify
- [x] 4.6 Enforce precedence and first match wins (error-mapping / Scenario: Precedence between steps, Scenario: First registered consumer mapper wins); verify `go test -run TestResolve_Order` passes
  - RED: all precedence rows and both order rows
  - GREEN: adjust the step order where tests show gaps
  - REFACTOR: /simplify
- [x] 4.7 Normalize mapper results and copy before enrichment (error-mapping / Scenario: Out-of-range status from a mapper, Scenario: Shared problem reused concurrently); verify `go test -race -run 'TestResolve_(Normalize|Copy)'` passes
  - RED: status rows with hook reports; 100-goroutine shared-problem row
  - GREEN: `clone()` (copies `ext` and `Header`), normalization with a hook note
  - REFACTOR: /simplify
- [x] 4.8 Add enrichers with the default instance enricher (error-mapping / Scenario: Default instance, Scenario: Custom enricher); verify `go test -run TestEnrichers` passes
  - RED: query-stripped, existing-instance, root-path rows; trace_id rows for fallback, mapped and self-describing problems
  - GREEN: `WithEnrichers`, `RequestFrom`, default enricher first
  - REFACTOR: /simplify
- [x] 4.9 Add the `OnError` hook and nil-error handling (error-mapping / Scenario: Hook sees the original error, Scenario: Nil error); verify `go test -run 'TestOnError|TestNilError'` passes
  - RED: hook-call rows; nil and `WithStatus(nil, 404)` rows writing nothing
  - GREEN: `OnError` option; `Respond` returns `false` for nil
  - REFACTOR: /simplify
- [x] 4.10 Contain faulty consumer code (error-mapping / Scenario: Consumer code panics); verify `go test -run TestResolve_FaultyConsumer` passes
  - RED: panicking mapper, describer, enricher and hook rows; nil mapper and nil enricher rows
  - GREEN: `safeCall` around every consumer call; nil filtering in options
  - REFACTOR: one generic `safeCall` instead of per-kind copies (Parameterize Function); /simplify
- [x] 4.11 Answer canceled requests like any other (error-mapping / Scenario: Request context canceled); verify `go test -run TestRespond_Canceled` passes
  - RED: three context/error rows, all answered
  - GREEN: no canceled exit in `Respond` (decision: a still-connected client must get a problem, not an empty 200)
  - REFACTOR: /simplify
- [x] 4.12 Confirm zero-configuration and concurrent use (error-mapping / Scenario: Zero options); verify `go test -race -run TestHandler_ZeroConfig` passes
- [x] 4.13 Document mapping (resolution order, helpers, describers, hints, scopes, hook, enrichers) in godoc with `ExampleIs`, `ExampleAs`, `ExampleWithStatus`, `ExampleScopeMappers`; verify `go test -run Example` passes
- [x] 4.14 Verify slice 4 end to end through `Wrap` (scoped + consumer + built-in + hint + fallback in one server), and check `go tool cover -func` covers every `Resolve` step's match and no-match branch and `safeCall`'s recover branch; run `-race`, vet and lint

## 5. Slice: hardened response writing

- [x] 5.1 Keep response status equal to problem status (problem-response / Scenario: Status consistency); verify `go test -run TestRespond_Status` passes
  - RED: the three rows
  - GREEN: status taken from the finalized problem only
  - REFACTOR: /simplify
- [x] 5.2 Set `Vary`, `Cache-Control` and problem headers, protecting content headers (problem-response / Scenario: Headers on the response, Scenario: Protected headers); verify `go test -run TestRespond_Headers` passes
  - RED: all header rows, including duplicate `Vary` and protected rows
  - GREEN: header building in `Respond`; adapter merges into existing writer headers
  - REFACTOR: /simplify
- [x] 5.3 Fall back to the constant minimal problem on encoding failure (problem-response / Scenario: Encoding fails, JSON rows only); verify `go test -run TestRespond_EncodeFailure` passes
  - RED: chan and Inf rows; hook receives the encoding error
  - GREEN: buffered encode, `fallbackBody`, recover around encoding
  - REFACTOR: /simplify
- [x] 5.4 Leave already-started responses alone using the tracking writer (problem-response / Scenario: Handler wrote before failing; stdlib-adapter / Scenario: Handler result, last row); verify `go test -run TestWrap_Started` passes
  - RED: the four "wrote" rows and the stdlib row
  - GREEN: `trackingWriter` (`WriteHeader`, `Write`, `Flush`, `Unwrap`), no double wrapping
  - REFACTOR: /simplify
- [x] 5.5 Keep writer capabilities reachable (stdlib-adapter / Scenario: Flush through the adapter); verify `go test -run TestWrap_ResponseController` passes against an `httptest.Server`
- [x] 5.6 Add direct `Write` and early misuse panics (stdlib-adapter / Scenario: Direct write from a standard handler, rows without XML; Scenario: Nil arguments); verify `go test -run 'TestWrite|TestWrap_Nil'` passes
  - RED: direct-write rows (JSON and nil); nil-function and nil-handler panics
  - GREEN: `Write`, construction checks in `Wrap`
  - REFACTOR: /simplify
- [x] 5.7 Document net/http usage (`Wrap`, `Write`, chi/ServeMux example, tracking-writer note) with `ExampleHandler_Write`; verify `go test -run Example` passes
- [x] 5.8 Verify slice 5 end to end (streaming handler, header-setting handler, failing encoder) and check `go tool cover -func` covers header building, `trackingWriter` and the fallback branch; run `-race`, vet and lint

## 6. Slice: XML and negotiation

- [x] 6.1 Select a format from `Accept` (problem-response / Scenario: Negotiated format); verify `go test ./internal/negotiate -run TestSelect` passes
  - RED: every JSON and XML row
  - GREEN: `internal/negotiate.Select` with q-values, specificity, case-insensitivity, JSON-first ties
  - REFACTOR: separate parsing from ranking (Split Phase); /simplify
- [x] 6.2 Bound and tolerate bad `Accept` headers (problem-response / Scenario: Fallback to JSON); verify `go test ./internal/negotiate -run TestSelect_Malformed` passes and `go test -fuzz FuzzSelect -fuzztime 30s` never panics or exceeds the limits
  - RED: all fallback rows including the 8 KiB header
  - GREEN: 4 KiB / 32-range limits, invalid-q dropping
  - REFACTOR: /simplify
- [x] 6.3 Walk extension values into Appendix B XML (problem-document / Scenario: XML encoding, Scenario: Tag options respected in XML, Scenario: Invalid nested key); verify `go test ./internal/xmlenc` passes
  - RED: all XML rows, tag-option rows and invalid-key rows
  - GREEN: JSON round trip with `UseNumber`, sorted map keys, `<i>` items, escaping, XML-name check sharing the extension rule
  - REFACTOR: /simplify
- [x] 6.4 Add `MarshalXML` and unexported JSON and XML formats wired into `Respond`; the public `Encoder` interface and `WithEncoders` are postponed to a later change (user decision) (problem-response / Scenario: Respond without writing, XML row; Scenario: Encoding fails, XML row; stdlib-adapter / Scenario: Direct write from a standard handler, XML row); verify `go test -run 'TestRespond_XML|TestWrite_XML'` passes
  - RED: those rows
  - GREEN: `MarshalXML`, unexported formats, negotiation in `Respond`
  - REFACTOR: /simplify
- [x] 6.5 Benchmark `Respond` for JSON and XML (`BenchmarkRespond_JSON`, `BenchmarkRespond_XML`); verify the JSON path allocates no more than before XML was added (compare with `benchstat`)
- [x] 6.6 Document negotiation and XML (godoc, `ExampleProblem_MarshalXML`); verify `go test -run Example` passes
- [x] 6.7 Verify slice 6 end to end (same server answering JSON and XML clients) and check `go tool cover -func` covers `Select`, the XML walker and both encoders including error branches; run `-race`, vet and lint

## 7. Slice: panic recovery for net/http

- [x] 7.1 Recover panics into safe 500 problems, opt-in only (panic-recovery / Scenario: Middleware installed or not, Scenario: Panic values); verify `go test -run TestRecover` passes
  - RED: installed/not-installed rows; panic value rows including `panic(nil)`
  - GREEN: `PanicError`, `Handler.Recover` using `Respond`
  - REFACTOR: /simplify
- [x] 7.2 Give the hook the value and the panic-site stack (panic-recovery / Scenario: Hook receives the panic); verify `go test -run TestRecover_Hook` passes
  - RED: value rows, stack contains `loadVessel`, unwrapping rows
  - GREEN: `debug.Stack()` in the deferred function, `Unwrap`
  - REFACTOR: /simplify
- [x] 7.3 Render recovered panics like errors (panic-recovery / Scenario: Negotiated 500); verify `go test -run TestRecover_Negotiated` passes
- [x] 7.4 Preserve `http.ErrAbortHandler` and skip writes after output or broken connections (panic-recovery / Scenario: Abort handler, Scenario: Panic after output or on broken connection); verify `go test -run 'TestRecover_(Abort|Started|Broken)'` passes
  - RED: both abort rows; the four "does" rows
  - GREEN: exact-sentinel re-panic; `trackingWriter` check; `EPIPE` / `ECONNRESET` detection
  - REFACTOR: /simplify
- [x] 7.5 Never panic during recovery (panic-recovery / Scenario: Faulty consumer code during recovery); verify `go test -run TestRecover_Faulty` passes
  - RED: enricher, hook and encoder panic rows; server keeps serving the next request
  - GREEN: second-level recover writing `fallbackBody`
  - REFACTOR: /simplify
- [x] 7.6 Document recovery (godoc, `ExampleHandler_Recover` logging the hook with `slog`); verify `go test -run Example` passes
- [x] 7.7 Verify slice 7 end to end with an `httptest.Server` (panic, abort, panic after streaming) and check `go tool cover -func` covers every branch of `Recover`; run `-race`, vet and lint

## 8. Slice: client-side parsing

- [x] 8.1 Decode XML leniently (problem-document / Scenario: Decode XML); verify `go test -run TestProblem_UnmarshalXML` passes and `go test -fuzz FuzzUnmarshalXML -fuzztime 30s` never panics
  - RED: the three rows; a JSON → XML → Problem round trip
  - GREEN: `UnmarshalXML` building extensions from child elements (`<i>` lists to slices)
  - REFACTOR: /simplify
- [x] 8.2 Parse problems from HTTP responses (problem-document / Scenario: Parse response); verify `go test -run TestParse` passes
  - RED: parsed and refused rows, including a failing body reader
  - GREEN: `Parse(*http.Response)` with `mime.ParseMediaType`, body closed in every path
  - REFACTOR: /simplify
- [x] 8.3 Document `Parse` for client and test use (`ExampleParse`); verify `go test -run Example` passes
- [x] 8.4 Verify the whole library end to end (server with every feature, client parsing both formats) and check `go tool cover -func` covers every hot path listed in design.md; run `-race`, vet, lint and `GOWORK=off go test ./...`

## 9. Release readiness

- [x] 9.1 Write the README (install, quick start for net/http, defaults table, customization, RFC 9457 compliance notes); verify every code block is mirrored by a compiling `Example`
- [x] 9.2 Add `CHANGELOG.md` and `RELEASING.md` (tag the root module first; later modules use `<dir>/vX.Y.Z` tags); verify a dry run with `GOWORK=off go list -m github.com/kartaladev/prblm@<local tag>` resolves

## 10. Decisions after review

- [x] 10.1 Stop mapping a bare `io.EOF` (error-mapping / Scenario: Built-in mapping, `io.EOF` row); verify `go test -run TestBuiltins` passes
  - RED: the `io.EOF` row expecting 500 with no detail
  - GREEN: remove the `io.EOF` built-in
  - REFACTOR: /simplify
- [x] 10.2 Export `ErrInvalidStatus` and `ErrEncoding` for hook notes (error-mapping / Scenario: Out-of-range status from a mapper; problem-response / Scenario: Encoding fails); verify `go test -run 'TestResolve_Normalize|TestRespond_EncodeFailure'` passes
  - RED: rows asserting `errors.Is` on the notes
  - GREEN: sentinels, wrapped by `normalize` and `encode`; `OnError` godoc
  - REFACTOR: /simplify
- [x] 10.3 Add `WithType`, `WithTitle` and `WithDetail` (problem-document / Scenario: Field options); verify `go test -run TestNew_Fields` passes and `ExampleNew` uses them
  - RED: the four rows
  - GREEN: the three field options
  - REFACTOR: /simplify
- [x] 10.4 Keep `ReadFrom` reachable through the tracking writer (stdlib-adapter / Scenario: Copy through the adapter); verify `go test -run TestWrap_ReadFrom` passes
  - RED: both rows, with a writer that counts `ReadFrom` calls
  - GREEN: `trackingWriter.ReadFrom`
  - REFACTOR: /simplify
- [x] 10.5 Add the MIT `LICENSE` (Copyright (c) 2026 Kartala Digital Indonesia), the README license section, and README/CHANGELOG entries for 10.1–10.4; verify every README code block is still mirrored by a compiling `Example`
- [x] 10.6 Verify the whole library again: `-race`, vet, lint, `GOWORK=off go test ./...`, the core deps check, cross-platform vet, and `go tool cover -func` on the hot paths

## Workflow follow-up

- Archive the change after review and once `v0.1.0` is tagged.
- Start `add-framework-adapters` and `add-mapper-plugins` against the released core.
