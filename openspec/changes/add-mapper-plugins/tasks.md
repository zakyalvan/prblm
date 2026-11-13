# Tasks

## 1. Scaffolding for slice 1

- [x] 1.1 Confirm the core this change builds on works without a tag (`add-problem-details-core` archived; `add-framework-adapters` complete); verify `go build ./...` and `GOWORK=off go test ./...` pass in the root module
- [x] 1.2 Create the `mappers/sqlmap` package directory in the root module with a package doc comment; verify `go build ./...` succeeds and `go list -m all` in the root module still shows no third-party module

## 2. Slice: sqlmap turns database/sql errors into problems (root module)

- [x] 2.1 Map no rows to 404 when registered (sql-error-mapping / Scenario: Registered or not, Scenario: Missing row); verify `go test ./mappers/sqlmap -run TestMapper_NoRows` passes
  - RED: table test over a real `prblm.NewHandler` + `Wrap` via `httptest` with registered/unregistered rows and matched/not-matched rows (plain, wrapped, joined, look-alike text); fails because `sqlmap.Mapper` does not exist
  - GREEN: `Mapper(opts ...Option) prblm.Mapper` returning a `prblm.MapperFunc` with `errors.Is(err, sql.ErrNoRows)` and the generic 404 detail
  - REFACTOR: express cases as data (case list of target + default problem) to avoid repeated branches (Replace Conditional with data/table); /simplify
- [x] 2.2 Map connection failures to 503 (sql-error-mapping / Scenario: Connection failure); verify `go test ./mappers/sqlmap -run TestMapper_ConnFailure` passes
  - RED: the three rows
  - GREEN: add `driver.ErrBadConn` and `sql.ErrConnDone` cases
  - REFACTOR: /simplify
- [x] 2.3 Leave programming errors and degenerate inputs unmatched (sql-error-mapping / Scenario: Unmapped SQL errors, Scenario: Degenerate inputs); verify `go test ./mappers/sqlmap -run 'TestMapper_(Unmapped|Degenerate)'` passes
  - RED: `ErrTxDone` (plain and wrapped), `ErrSkip`, `ErrRemoveArgument`, nil and typed-nil rows; assert no panic and 500 without detail through the handler
  - GREEN: nothing beyond confirming the case list; fix any gap the tests reveal
  - REFACTOR: /simplify
- [x] 2.4 Add per-case overrides (sql-error-mapping / Scenario: Override results, Scenario: Nil override option); verify `go test ./mappers/sqlmap -run TestMapper_Override` passes
  - RED: override returning a typed problem, returning nil (falls to a later 410 mapper), panicking (core containment, falls to 410, hook receives the panic); nil override functions keep defaults
  - GREEN: `WithNotFound`, `WithUnavailable` options; nil options and nil functions ignored
  - REFACTOR: share one override mechanism across cases (Parameterize Function); /simplify
- [x] 2.5 Write the `sqlmap` godoc (opt-in rationale, the deliberate `ErrTxDone` exclusion, driver-specific not-found errors not covered) with `ExampleMapper`; verify `go test ./mappers/sqlmap -run Example` passes
- [x] 2.6 Verify slice 2 end to end (server with `sqlmap` registered answering 404, 503 and fallback 500) and check `go tool cover -func` covers every `sqlmap` match, no-match and override branch; run `go vet`, `golangci-lint`, `go test -race ./...` and `GOWORK=off go test ./...` for the root module; run gopls `go_diagnostics` on changed files

## 3. Slice: gormmap module

- [x] 3.1 Create `mappers/gormmap/go.mod` (requires `gorm.io/gorm` and the core through `replace github.com/kartaladev/prblm => ../..` until the release; `gorm.io/driver/sqlite` for tests), add it to `go.work`, and add a CI job running vet, lint, `go test -race` and `GOWORK=off go test` for it; verify the job passes on an empty package and the root module's dependency check still passes
- [x] 3.2 Map record not found to 404 when registered (gorm-error-mapping / Scenario: Registered or not, Scenario: Missing record); verify `go test ./... -run TestMapper_RecordNotFound` in `mappers/gormmap` passes
  - RED: rows through a real handler, producing `gorm.ErrRecordNotFound` from a real `First` call on SQLite plus wrapped and look-alike rows
  - GREEN: `Mapper` with `errors.Is(err, gorm.ErrRecordNotFound)`
  - REFACTOR: /simplify
- [x] 3.3 Map duplicated key to 409 with translation enabled (gorm-error-mapping / Scenario: Duplicate key with translation enabled); verify `go test -run TestMapper_DuplicatedKey` passes
  - RED: real SQLite unique violation with `TranslateError: true`; body has neither constraint name nor value
  - GREEN: `gorm.ErrDuplicatedKey` case
  - REFACTOR: /simplify
- [x] 3.4 Leave other and degenerate errors unmatched (gorm-error-mapping / Scenario: Degenerate inputs; Scenario: gorm without translation, row "gorm mapper only"); verify `go test -run 'TestMapper_(Degenerate|Untranslated)'` passes
  - RED: nil, `ErrInvalidTransaction`, `ErrInvalidData`, typed-nil rows; untranslated SQLite unique violation reaches the 500 fallback
  - GREEN: confirm the case list
  - REFACTOR: /simplify
- [x] 3.5 Add overrides (gorm-error-mapping / Scenario: Override results); verify `go test -run TestMapper_Override` passes
  - RED: typed problem, nil function, nil result, panic rows
  - GREEN: `WithRecordNotFound`, `WithDuplicatedKey`
  - REFACTOR: reuse the override shape from `sqlmap` without sharing code across modules (keep plugins independent); /simplify
- [x] 3.6 Verify the root module stays dependency-free (gorm-error-mapping / Scenario: Root module stays dependency-free); verify the CI dependency check passes with `gormmap` in the workspace
- [x] 3.7 Write the `gormmap` godoc (`TranslateError: true` requirement, pairing with a driver mapper) with `ExampleMapper`; verify `go test -run Example` passes
- [x] 3.8 Verify slice 3 end to end with a real SQLite-backed handler and check `go tool cover -func` covers every `gormmap` branch; run vet, lint, `go test -race` and `GOWORK=off go test` for `mappers/gormmap`; run gopls `go_diagnostics` on changed files

## 4. Slice: pgxmap module

- [x] 4.1 Create `mappers/pgxmap/go.mod` (requires `github.com/jackc/pgx/v5` and the core through `replace github.com/kartaladev/prblm => ../..` until the release), add it to `go.work` and CI; verify the job passes on an empty package and the root dependency check still passes
- [x] 4.2 Map SQLSTATE codes to problems when registered (pgx-error-mapping / Scenario: Registered or not, Scenario: Mapped codes); verify `go test -run TestMapper_Codes` passes
  - RED: one row per mapped code with exact status and detail, built from real `*pgconn.PgError` values wrapped with `fmt.Errorf`; fails because `pgxmap.Mapper` does not exist
  - GREEN: `errors.AsType[*pgconn.PgError]`, nil guard, read-only default table
  - REFACTOR: /simplify
- [x] 4.3 Leave unlisted codes and degenerate inputs unmatched (pgx-error-mapping / Scenario: Unmapped codes fall through, Scenario: Degenerate inputs); verify `go test -run 'TestMapper_(Unmapped|Degenerate)'` passes
  - RED: unlisted, empty and garbage codes; nil, typed-nil `*pgconn.PgError`, `pgx.ErrNoRows`, look-alike text, joined error rows
  - GREEN: no-match return paths
  - REFACTOR: /simplify
- [x] 4.4 Hide database internals by default (pgx-error-mapping / Scenario: Duplicate email); verify `go test -run TestMapper_NoLeak` passes
  - RED: both rows asserting none of the forbidden strings appear in the JSON and XML bodies
  - GREEN: confirm only table details are used
  - REFACTOR: /simplify
- [x] 4.5 Add `Retry-After` with validation (pgx-error-mapping / Scenario: Retry-After header, Scenario: Invalid Retry-After setting); verify `go test -run TestMapper_RetryAfter` passes
  - RED: default, configured and non-retryable rows checking the response header; 0 and -1 construction panics
  - GREEN: `WithRetryAfter`, header via `prblm.WithHeader`
  - REFACTOR: /simplify
- [x] 4.6 Add the explicit database-detail opt-in (pgx-error-mapping / Scenario: Detail exposure); verify `go test -run TestMapper_DatabaseDetail` passes
  - RED: exposed, default and empty-detail rows
  - GREEN: `WithDatabaseDetail` using only `pe.Detail`
  - REFACTOR: /simplify
- [x] 4.7 Add per-constraint problems (pgx-error-mapping / Scenario: Friendly duplicate email, Scenario: Invalid constraint registration); verify `go test -run TestMapper_Constraint` passes
  - RED: matching, other-constraint, case-mismatch, nil-result and panic rows; empty-name and nil-function construction panics
  - GREEN: `WithConstraint` with exact-match lookup before the table
  - REFACTOR: /simplify
- [x] 4.8 Add per-code problems (pgx-error-mapping / Scenario: Code overrides); verify `go test -run TestMapper_CodeOverride` passes
  - RED: listed override, unlisted code added, nil result falling back to table, nil result for unlisted code
  - GREEN: `WithCode` consulted after constraints and before the table
  - REFACTOR: unify constraint and code lookups behind one ordered lookup (Consolidate Conditional Expression); /simplify
- [x] 4.9 Prove gorm pass-through to pgx (gorm-error-mapping / Scenario: gorm without translation, row "gorm mapper, PostgreSQL mapper"); verify a cross-plugin test in `mappers/pgxmap` (test-only requirement on `gormmap`) with a `*pgconn.PgError` returned through gorm's error path yields 409
- [x] 4.10 Write the `pgxmap` godoc (privacy rationale, `WithDatabaseDetail` warning, constraint and code examples) with `ExampleMapper` and `ExampleWithConstraint`; verify `go test -run Example` passes
- [x] 4.11 Verify slice 4 end to end (handler with `pgxmap` answering 409/422/503 with headers in JSON and XML) and check `go tool cover -func` covers the nil guard, constraint lookup, code override, table lookup, header and detail-opt-in branches; run vet, lint, `go test -race` and `GOWORK=off go test` for `mappers/pgxmap`; run gopls `go_diagnostics` on changed files

## 5. Slice: validatormap module

- [x] 5.1 Create `mappers/validatormap/go.mod` (requires `github.com/go-playground/validator/v10`, `github.com/go-playground/universal-translator` and the core through `replace github.com/kartaladev/prblm => ../..` until the release; `github.com/go-playground/locales` for tests), add it to `go.work` and CI; verify the job passes on an empty package and the root dependency check still passes
- [x] 5.2 Build JSON Pointers and the field fallback (validation-error-mapping / Scenario: Pointer building, Scenario: No tag name function); verify `go test -run 'TestPointer|TestField'` passes and `go test -fuzz FuzzPointer -fuzztime 30s` never panics
  - RED: every namespace row, using namespaces produced by validating real structs (with and without a tag name function) plus the single-variable row
  - GREEN: unexported tokenizer and RFC 6901 escaping with fragment percent-encoding; `Namespace() == StructNamespace()` fallback
  - REFACTOR: separate tokenizing from escaping (Split Phase); /simplify
- [x] 5.3 Turn validation errors into a 422 problem with `errors` entries when registered (validation-error-mapping / Scenario: Registered or not, Scenario: Invalid fields, Scenario: Wrapped and empty validation errors); verify `go test -run TestMapper_Validation` passes
  - RED: rows through a real handler with a real validator and struct; exact `errors` JSON; wrapped, joined and empty rows
  - GREEN: `Mapper` with `errors.AsType[validator.ValidationErrors]`, empty guard, entries with `json` tags, default message `failed validation: <tag>`
  - REFACTOR: /simplify
- [x] 5.4 Never expose rejected values or type names (validation-error-mapping / Scenario: Sensitive input); verify `go test -run TestMapper_NoLeak` passes
  - RED: the forbidden-strings row in JSON and XML
  - GREEN: confirm `fe.Value()`, `fe.Param()` and `fe.Error()` are never used
  - REFACTOR: /simplify
- [x] 5.5 Make status, type and title configurable (validation-error-mapping / Scenario: Configured problem, Scenario: Invalid status); verify `go test -run TestMapper_Options` passes
  - RED: the three configuration rows; 200, 500 and 0 construction panics
  - GREEN: `WithStatus`, `WithType`, `WithTitle`
  - REFACTOR: /simplify
- [x] 5.6 Translate messages per request (validation-error-mapping / Scenario: Translator choices); verify `go test -run TestMapper_Translator` passes
  - RED: Indonesian and English translators with `min` registered (via `locales` + `universal-translator`), nil result, panicking function, no function
  - GREEN: `WithTranslator`, local `recover` around the function, `fe.Translate`
  - REFACTOR: /simplify
- [x] 5.7 Leave invalid usage and degenerate inputs unmatched (validation-error-mapping / Scenario: Degenerate inputs); verify `go test -run TestMapper_Degenerate` passes
  - RED: nil, `InvalidValidationError` from validating nil, typed-nil rows
  - GREEN: confirm guards
  - REFACTOR: /simplify
- [x] 5.8 Render validation problems as XML (validation-error-mapping / Scenario: XML client); verify `go test -run TestMapper_XML` passes with exact `<errors><i>...</i></errors>` content
- [x] 5.9 Write the `validatormap` godoc (registering a tag name function, translator setup, `pointer` vs `field`, 422 rationale) with `ExampleMapper` and `ExampleWithTranslator`; verify `go test -run Example` passes
- [x] 5.10 Verify slice 5 end to end (JSON and XML clients, translated and untranslated) and check `go tool cover -func` covers entry building, the tokenizer and escaping, the field fallback and translator selection including the recover branch; run vet, lint, `go test -race` and `GOWORK=off go test` for `mappers/validatormap`; run gopls `go_diagnostics` on changed files

## 6. Release readiness

- [x] 6.1 Add a "Mapper plugins" section to the README (install per module, registration, defaults table with the `WithDatabaseDetail` warning, combining plugins); verify every snippet is mirrored by a compiling `Example`
- [x] 6.2 Extend `RELEASING.md` with the plugin tags (`mappers/gormmap/vX.Y.Z`, `mappers/pgxmap/vX.Y.Z`, `mappers/validatormap/vX.Y.Z`) the rule that the core is tagged first, and the step that removes each published module's replace before its tag; verify `GOWORK=off go list -m` resolves each module against local tags in a dry run

## 7. Release

- [x] 7.1 Confirm `add-framework-adapters` is archived and every task above is done; run the full verification in every module (`-race`, vet, lint, `GOWORK=off go test ./...`, the root dependency check, cross-platform vet) and verify it passes
- [ ] 7.2 Transfer `zakyalvan/prblm` to `kartaladev` (done by the repository owner) and point `origin` at `git@github.com:kartaladev/prblm.git`; verify `gh repo view kartaladev/prblm` succeeds and `git push` works
- [ ] 7.3 Move the CHANGELOG `[Unreleased]` entries to `[0.1.0]` (core, `MergeInto`, `sqlmap`), tag the core `v0.1.0` and push the tag; verify `GOWORK=off go list -m github.com/kartaladev/prblm@v0.1.0` resolves with an empty module cache
- [ ] 7.4 In `ginprblm`, `fiberprblm`, `mappers/gormmap`, `mappers/pgxmap` and `mappers/validatormap`, remove the core replace, require `v0.1.0` and run `go mod tidy`; verify `GOWORK=off go test -race ./...` passes in each against the real tag, `grep -l replace` finds no published `go.mod`, and CI is green
- [ ] 7.5 Tag `ginprblm/v0.1.0`, `fiberprblm/v0.1.0`, `mappers/gormmap/v0.1.0`, `mappers/pgxmap/v0.1.0` and `mappers/validatormap/v0.1.0` on that commit and push them (no plugin requires another since 8.6); verify `GOWORK=off go list -m <module>@v0.1.0` resolves for each

## 8. Review fixes (PR #6 review and verification)

Done before group 7 continues; 7.1 is re-run in 8.10.

- [x] 8.1 Map gorm's foreign key and check constraint sentinels (gorm-error-mapping / Scenario: Translated constraint violations; Scenario: Degenerate inputs, row `ErrInvalidField`); verify `go test -run 'TestMapper_(ForeignKeyViolated|CheckConstraintViolated|Degenerate)'` passes in `mappers/gormmap`
  - RED: a real SQLite foreign key violation with `TranslateError: true` and `PRAGMA foreign_keys = ON`; a wrapped `ErrCheckConstraintViolated`; both currently 500
  - GREEN: two cases and `WithForeignKeyViolated`, `WithCheckConstraintViolated`, same statuses and details as `pgxmap`
  - REFACTOR: /simplify
- [x] 8.2 Map 57014 to 504 without Retry-After (pgx-error-mapping / Scenario: Mapped codes, canceled statements; Scenario: Retry-After header, row `57014`); verify `go test -run 'TestMapper_(Codes|RetryAfter)'` passes
- [x] 8.3 Reject malformed SQLSTATE codes in `WithCode` (pgx-error-mapping / Scenario: Invalid code registration); verify `go test -run TestMapper_CodeOverrideInvalid` passes
- [x] 8.4 Correct the pgxmap godoc on `pgx.ErrNoRows` (wraps `sql.ErrNoRows` since pgx v5.7.1, so `sqlmap` maps it); verify `go test -run TestMapper_PgxNoRows` in `mappers/pgxmap` (`pgxmap` + `sqlmap` → 404; `sqlmap` is in the core, which `pgxmap` already requires)
- [x] 8.5 Never mix `pointer` and `field` in one problem (validation-error-mapping / Scenario: JSON names equal to Go names); verify `go test -run TestMapper_NamesEqualGoNames` passes
  - RED: `Account{ID json:"ID", Name json:"name"}` gives `field` for `ID` and `pointer` for `name`
  - GREEN: decide once per problem; `location` takes the decision
  - REFACTOR: /simplify
- [x] 8.6 Move the gorm cross-plugin tests to the unpublished `internal/plugintest` module (gorm-error-mapping / Scenario: gorm without translation; Scenario: Translated error with a constraint problem); verify `go test ./...` passes there, `GOWORK=off go mod why -m gorm.io/gorm` in `mappers/pgxmap` reports it unneeded, and CI and RELEASING.md no longer chain the pgxmap tag behind gormmap
  - Characterization, not RED: `TestGorm` resolves the real `postgres.Dialector.Translate` output in both orders. The `gorm mapper, PostgreSQL mapper` row answering 409 instead of the constraint's 422 reproduces the review finding; the fix is the documented order (8.7), not code
  - Move `gorm_test.go`; drop `Example_gorm` (it showed the wrong order; the godoc shows the right one); drop the gorm requirements and the second replace from `mappers/pgxmap/go.mod`
- [x] 8.7 Document the driver-mapper-first order in the gormmap and pgxmap godoc and the README; verify every snippet matches a compiling example
- [x] 8.8 Build pointers for embedded structs with `JSONName` (validation-error-mapping / Scenario: Embedded structs, Scenario: JSON names); verify `go test -run 'TestJSONName|TestMapper_EmbeddedStruct'` passes
  - RED: `TestJSONName` against a stub, and the embedded rows through a real validator with `WithTagNameFuncBlankOmit()`
  - GREEN: `JSONName` following `encoding/json`
  - REFACTOR: use `JSONName` in the examples and README; /simplify
- [x] 8.9 Record translator panics in the problem's cause (validation-error-mapping / Scenario: Translator choices, row "panics"); verify `go test -run TestMapper_Translator` passes
  - RED: the `OnError` hook receives the 422 problem but `errors.As(p, &*prblm.PanicError)` fails
  - GREEN: collect recovered panics as `*prblm.PanicError` and attach them with `prblm.WithCause`
  - REFACTOR: /simplify
- [x] 8.10 Re-run 7.1's full verification in every module, including `internal/plugintest`, and check `go tool cover -func` covers the new branches (gormmap's new cases, `WithCode` validation, the per-problem location choice, `JSONName`, the panic cause)
- [x] 8.11 Security review follow-up: pin the misplaced pointer for a map key containing `]` followed by `.` or `[` with `TestPointer/map_key_with_delimiters`, and document the limit in the godoc, the spec and D6 (it can't be fixed in the mapper: the validator writes keys unescaped)

## Workflow follow-up

- Archive the change after review, once group 7 is done.
