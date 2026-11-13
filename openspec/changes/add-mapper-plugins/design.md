# Design

## Context

This change builds on the core from `add-problem-details-core`, which lives in this repository and is tagged only in this change's release step, so it must not redefine anything the core owns. It uses:
- `prblm.Problem`, `prblm.New` and field options such as `WithHeader` (core D2)
- the `Mapper` interface (nil means no match), `MapperFunc`, `Is` and `As[T]` (core D3)
- the resolution order, where consumer mappers run before built-ins, and the core's containment of panicking mappers (core D4)

Motivation is in proposal.md (Why), and behavior is in `specs/`.

Constraints:
- the root module stays free of third-party dependencies
- each plugin that needs a dependency is its own module in `mappers/<name>map`
- Go 1.27

## Goals / Non-Goals

**Goals:**
- One uniform plugin shape, so consumers learn it once: `<name>map.Mapper(opts ...Option) prblm.Mapper`.
- Generic, leak-free details by default; every customization is an option.
- Each plugin is usable on its own and combinable with the others. Plugins for different error families can be registered in any order; a driver mapper goes before the gorm mapper (D4).

**Non-Goals:**
- Changing the core API or resolution order.
- Mappers for `lib/pq`, MySQL, SQLite or other validation libraries.
- Logging database errors. Consumers do that in the core `OnError` hook, which still receives the original error.

## Slices

Each slice ends with a releasable, working plugin registered on a real core handler and exercised over net/http. Later slices only add a plugin, and the last slice releases everything.

1. **`sqlmap` in the root module.**
   - At its end: `prblm.NewHandler(prblm.WithMappers(sqlmap.Mapper()))` turns `sql.ErrNoRows` into a 404 problem and `driver.ErrBadConn` into a 503 problem through `Wrap`. No new dependency, no new module.
   - Scenarios: all sql-error-mapping scenarios.
   - Hot paths: the `sqlmap` match and no-match paths and its override path.
2. **`gormmap` module.**
   - At its end: a separate module with its own `go.mod`. `gorm.ErrRecordNotFound` gives 404 and `gorm.ErrDuplicatedKey` gives 409. Verified against SQLite through `gorm.io/driver/sqlite` in tests only.
   - Scenarios: all gorm-error-mapping scenarios except "gorm without translation" with the PostgreSQL mapper row and "Translated error with a constraint problem". Those need slice 3, which adds them.
   - Hot paths: the `gormmap` match, no-match and override paths.
3. **`pgxmap` module.**
   - At its end: SQLSTATE table, generic details, `Retry-After`, per-constraint overrides, and the `WithDatabaseDetail` opt-in. The deferred gorm rows are added here as cross-plugin tests in the unpublished `internal/plugintest` module (D7), so `pgxmap` never requires gorm.
   - Scenarios: all pgx-error-mapping scenarios, plus gorm-error-mapping / gorm without translation and / Translated error with a constraint problem.
   - Hot paths: the code lookup, the constraint lookup, building the header, the detail opt-in branch.
4. **`validatormap` module.**
   - At its end: `ValidationErrors` becomes a 422 problem with `errors` entries, pointers or the `field` fallback, configurable status, type and title, and translation per request. It renders in JSON and XML.
   - Scenarios: all validation-error-mapping scenarios.
   - Hot paths: entry building, pointer building, translator selection.
5. **Release.**
   - At its end: the repository belongs to `kartaladev`, the core is tagged `v0.1.0` (including `sqlmap`), the adapter and plugin modules require that tag with no replace, pass `GOWORK=off` against it, and are tagged `v0.1.0`.
   - Requires `add-framework-adapters` to be complete, since its modules are tagged here too.

## Decisions

### D1. Uniform plugin shape on top of the core API

```go
package sqlmap // and gormmap, pgxmap, validatormap
type Option func(*config)
func Mapper(opts ...Option) prblm.Mapper   // returns a prblm.MapperFunc
```

- Each plugin's mapper is a `prblm.MapperFunc` built from `errors.Is` or `errors.AsType`, returning `*prblm.Problem` or nil, with nothing new in the core.
- Options are applied once at construction. The returned mapper is immutable and safe for concurrent use.
- A nil `Option` is ignored.
- *Alternative:* returning a `[]prblm.Mapper` per plugin (one per case). Rejected: a single mapper keeps registration order simple, and lets a plugin check its own cases in a fixed priority (for example, constraint before SQLSTATE).
- **Failure modes:**
  - **A nil option in `opts`:** skipped.
  - **The consumer registers a plugin twice:** harmless, because the first match wins.
  - **The consumer registers plugins in an order where one shadows another:**
    - `sqlmap` and `pgxmap`: no shadowing. `sqlmap` matches only database/sql sentinels, and `*pgconn.PgError` doesn't wrap them. `pgx.ErrNoRows` does wrap `sql.ErrNoRows` (pgx v5.7.1 and later), so `sqlmap` maps it to 404, and `pgxmap` never matches it.
    - `gormmap` and `pgxmap`: gorm's PostgreSQL driver v1.6.2 and later returns `fmt.Errorf("%w: %w", gorm.ErrDuplicatedKey, pgErr)`, so both match a translated error. Registered first, `gormmap` would hide `pgxmap`'s constraint and code problems. The documented order is the driver mapper first (D4).

### D2. Override functions

Every mapped case has an override option taking `func(err error) *prblm.Problem`. In sqlmap these are `WithNotFound` and `WithUnavailable`, and gormmap and pgxmap follow the same pattern.
- A nil function keeps the default.
- A nil result means "no match", consistent with the core mapper contract, so later mappers get the error.
- A panic is not recovered by the plugin. It propagates to the core, which treats the mapper as no match and reports the panic to the hook (core D4). One containment mechanism, not two.
- *Alternative:* overrides that return a full replacement problem only, with nil meaning "use the default". Rejected, because consumers could no longer opt a case out.
- **Failure modes:**
  - **An override returns a problem with a non-error status:** the core normalizes it to 500 and reports it (core error-mapping).
  - **An override mutates a shared problem:** the core clones resolved problems (core D4).

### D3. sqlmap in the root module

- Lives at `mappers/sqlmap` as a package of the root module, so it adds no module or tag. It imports only `database/sql` and `database/sql/driver`.
- The match order is `sql.ErrNoRows`, then `driver.ErrBadConn` / `sql.ErrConnDone`.
- `sql.ErrTxDone`, `driver.ErrSkip` and `driver.ErrRemoveArgument` are deliberately unmatched: they signal programming errors or are internal to drivers.
- *Alternative:* a separate module, like the others. Rejected, because there's no dependency to isolate.
- **Failure modes:**
  - **A driver that returns its own not-found error instead of `sql.ErrNoRows`:** not matched, and documented as such.
  - **A typed-nil custom error:** `errors.Is` doesn't dereference it, so no match.

### D4. gormmap

- `errors.Is` against `gorm.ErrRecordNotFound` (404), and against the sentinels gorm's driver error translators return: `gorm.ErrDuplicatedKey` (409), `gorm.ErrForeignKeyViolated` (409) and `gorm.ErrCheckConstraintViolated` (422). The last three use the same statuses and details as `pgxmap` gives `23505`, `23503` and `23514`, so a consumer gets the same problem whichever plugin answers. Overrides: `WithRecordNotFound`, `WithDuplicatedKey`, `WithForeignKeyViolated`, `WithCheckConstraintViolated`.
- What the translators return (gorm drivers v1.6): postgres translates 23505, 23503, 23514 and 42703 (`ErrInvalidField`); mysql 1062, 1451 and 1452; sqlite 1555, 2067 and 787; sqlserver 2627, 2601 and 547.
- `gorm.ErrInvalidField` (undefined column) is not mapped: it's a server fault, left to the 500 fallback.
- The godoc states that the translated sentinels require `gorm.Config{TranslateError: true}`. Without it, register `pgxmap` (or another driver mapper) to handle the raw driver error.
- **Registration order:** driver mapper first, `prblm.WithMappers(pgxmap.Mapper(), gormmap.Mapper())`. That order is right in every case: an untranslated error and a translated error from postgres driver v1.6.2+ both still hold the `*pgconn.PgError`, so `pgxmap` answers with its constraint and code problems; a bare translated sentinel (older drivers, other databases) falls to `gormmap`. `gormmap` can't step aside by itself, since it can't import pgx and guessing from the chain would be fragile.
- Tests use `gorm.io/driver/sqlite` (test-only dependency of the `gormmap` module) to produce real gorm errors.
- **Failure modes:**
  - **TranslateError is off:** documented, and the pass-through is tested in slice 3.
  - **gormmap registered before pgxmap:** documented as the wrong order; a cross-plugin test pins both orders (gorm-error-mapping / Translated error with a constraint problem).
  - **gorm wraps the sentinel with extra context:** `errors.Is` still matches.

### D5. pgxmap

```go
func Mapper(opts ...Option) prblm.Mapper
func WithDatabaseDetail() Option                                        // loosens a safety default
func WithConstraint(name string, fn func(*pgconn.PgError) *prblm.Problem) Option
func WithRetryAfter(d time.Duration) Option                             // > 0, whole seconds
func WithCode(code string, fn func(*pgconn.PgError) *prblm.Problem) Option // per-SQLSTATE override; code is 5 of [0-9A-Z]
```

- It imports only `github.com/jackc/pgx/v5/pgconn`, but the module still requires `github.com/jackc/pgx/v5`.
- Lookup:
  1. `pe, ok := errors.AsType[*pgconn.PgError](err)`; if `!ok || pe == nil`, no match
  2. a constraint function for `pe.ConstraintName` (case-sensitive exact match); a non-nil result wins
  3. a per-code override
  4. the default table
  5. otherwise, no match
- The default table is a package-level `map[string]entry{status, detail, retry bool}`, read-only after init.
- `Retry-After` is set through `prblm.WithHeader` on the default problems of retryable codes (`40001`, `40P01`, `53300`), in whole seconds (`strconv.FormatInt(int64(d/time.Second), 10)`). Construction panics when `d < time.Second`. Problems from `WithConstraint` and `WithCode` functions are used as built; they add the header themselves when they need it.
- `57014` (`query_canceled`: `statement_timeout` or a cancel request) maps to 504 without `Retry-After`. pgx cancels a request's context by setting a socket deadline rather than sending a cancel request, so the context error the core maps to 504 is what a canceled request surfaces as, and a `57014` that reaches the mapper is mostly a statement timeout. Retrying it at once would most likely time out again, and the client can't tell the two causes apart.
- `WithCode` panics unless the code is five characters of digits and upper-case ASCII letters, the form PostgreSQL reports. A lower-case or short code would never match and the override would silently do nothing.
- `WithDatabaseDetail` replaces the generic detail with `pe.Detail` only when that's non-empty. The message, hint, schema, table, column, data type and where fields are never used.
- *Alternative:* mapping by SQLSTATE class (`23xxx`) instead of exact codes. Rejected, because classes mix client and server faults (e.g. `23P01` exclusion violations need their own decision), and an explicit table is easier to review.
- **Failure modes:**
  - **A typed-nil `*pgconn.PgError`:** guarded by `pe == nil`.
  - **An empty or unknown code:** no match.
  - **A constraint function panics:** core containment, so no match and a 500 (spec row).
  - **An empty constraint name, a malformed SQLSTATE code, a nil function, or a `Retry-After` under one second:** construction panics.
  - **The consumer exposes detail on a public API:** documented risk of that explicit opt-in.

### D6. validatormap

```go
func Mapper(opts ...Option) prblm.Mapper
func WithStatus(code int) Option       // 400..499, else construction panic
func WithType(uri string) Option
func WithTitle(title string) Option
func WithTranslator(fn func(ctx context.Context) ut.Translator) Option
func JSONName(fld reflect.StructField) string // tag name function following encoding/json
```

The mapper:
- `ves, ok := errors.AsType[validator.ValidationErrors](err)`; if `!ok || len(ves) == 0`, no match. `InvalidValidationError` is a different type, so it never matches.
- The problem gets the status (default 422), type and title (empty by default, so the core fills in the reason phrase), detail `The request is not valid.`, and the `errors` extension.
- Entries are `[]entry` with `Detail string \`json:"detail"\``, `Pointer string \`json:"pointer,omitempty"\`` and `Field string \`json:"field,omitempty"\``. They're plain structs, so the core encodes JSON and XML from the same tags.

Choosing between `pointer` and `field`, once per problem:
- The validator has no getter for its tag name function, so the mapper infers it. When any entry's `Namespace()` differs from its `StructNamespace()`, a tag name function is registered and every entry gets a `pointer`, including entries whose JSON names equal their Go names. When all are equal, every entry gets `field`. A problem never mixes the two, so a client reading `pointer` never silently drops an entry.
- The one case the inference can't see: a problem whose failed fields all have JSON names equal to their Go names. It gets `field` entries, which still name the field correctly.

Building the pointer from `fe.Namespace()`:
- Strip the first segment (the root struct) when there is one; see the anonymous-struct failure mode below. An empty remainder gives the pointer `#`.
- Tokenize on `.` and `[...]`. Each token is escaped per RFC 6901 (`~` → `~0`, `/` → `~1`), then percent-encoded for the URI fragment form (`url.PathEscape` semantics on non-unreserved characters).

The message:
- Call the translator function, if any, guarded by `defer recover()`. A panic or a nil result uses the default message.
- With a translator, use `fe.Translate(t)`, otherwise `"failed validation: " + fe.Tag()`.
- Never use `fe.Value()`, `fe.Param()` or `fe.Error()`. Each can carry the rejected value, struct names or parameters.

On the translator panic: it's recovered locally rather than by the core. A translator panic is a problem with an optional enhancement, not with the mapper, so losing the whole 422 to a 500 would be worse for clients. This is the one place a plugin contains a panic itself. The plugin has no access to the hook, so it records each recovered panic as a `*prblm.PanicError` (value and stack) in the problem's cause with `prblm.WithCause`, joined with `errors.Join` when there are several. The cause never reaches the client, and the `OnError` hook receives the problem and finds the panic with `errors.As`. *Alternative:* a core API to report a note from a mapper. Postponed: it widens the core API for one caller.

Embedded structs: the validator puts an embedded struct's name in the namespace (`Order.Base.name`), while `encoding/json` flattens its fields (`#/name`). `FieldError` doesn't expose the root type, so the mapper can't tell an embedded segment from a nested one. The fix is at the source: validator v10.30.3+ drops a segment when the tag name function returns "" and the validator was created with `validator.WithTagNameFuncBlankOmit()`. `JSONName` follows `encoding/json`'s naming: the json tag name when there is one, "" for `json:"-"` and for an untagged embedded struct (or pointer to struct), and the Go name otherwise. The godoc, README and examples register both.

- *Alternative:* RFC 7807-era `invalid-params` with `name`/`reason`. Rejected in favor of the RFC 9457 §3 example shape.
- **Failure modes:**
  - **An empty `ValidationErrors`:** no match.
  - **A non-ASCII map key:** percent-encoded.
  - **A map key containing `]` followed by `.` or `[`:** the validator writes keys unescaped, and `FieldError` exposes only the namespace strings (checked against v10.30.6, the latest), so the key `x].admin[0` gives the same namespace as the path `meta`, `x`, `admin`, `0`, `name`. It can't be fixed in the mapper: no parse can tell the two apart, and a digit key like `0].x[1` even looks like a slice index. The pointer follows the deeper path. Only the client that sent the key gets the misplaced pointer, inside its own input, so nothing leaks. Pinned by `TestPointer/map_key_with_delimiters` and documented.
  - **A namespace with no root segment:** `#`.
  - **A translator missing the tag:** `fe.Translate` falls back to `fe.Error()`, which names the Go struct (found in implementation, `TestMapper_TranslatorGaps`). The mapper detects that fallback and uses the default message instead.
  - **A translation that panics:** recovered per entry, like the translator function, and the default message is used.
  - **An anonymous root struct:** the validator reports no root segment for it (found in implementation, `TestPointer/anonymous_struct`). A root is stripped only when `Namespace()` and `StructNamespace()` start with the same segment; with equal namespaces, a single segment is a field. A nested field of an anonymous struct whose first JSON name equals its Go name stays ambiguous; documented.
  - **A huge number of failed fields:** entries are bounded by what the validator returns. No cap is added; documented.
  - **A tag name function that doesn't omit embedded structs, or a validator without `WithTagNameFuncBlankOmit`:** the embedded struct's name stays in the pointer. Documented next to `JSONName`.

### D7. Module and CI wiring

- Cross-plugin tests (gorm through `pgxmap`) live in `internal/plugintest`, an unpublished module in `go.work` and CI, with replaces for the core and every plugin. A published plugin's `go.mod` requires only what its own package and tests import, so `pgxmap` users never download gorm, and the plugins can be tagged in any order.
- `mappers/gormmap`, `mappers/pgxmap` and `mappers/validatormap` each have their own `go.mod` requiring `github.com/kartaladev/prblm` through `replace github.com/kartaladev/prblm => ../..` until the release, and are added to `go.work`. The release step removes the replace and requires `v0.1.0`.
- CI runs each module with `GOWORK=off`, keeps the root module's "no third-party requirements" check, and runs `go mod tidy -diff` per module.
- Tags: `mappers/gormmap/vX.Y.Z`, etc.
- **Failure modes:**
  - **A plugin compiles only inside the workspace:** caught by the `GOWORK=off` job.
  - **A replace left in a published module:** consumers ignore replace directives and would see an unresolvable placeholder version. The release step runs `GOWORK=off` against the real tag and checks that no published `go.mod` has a replace.
  - **A test-only dependency** (sqlite driver) leaks into the plugin's non-test API: `go mod why` check in CI. It stays a module requirement but isn't imported by non-test code.

## Defaults

| Plugin | Behavior | Default | Why | Opt-in |
|---|---|---|---|---|
| all | registration | not active | importing must not change behavior | `prblm.WithMappers(<name>map.Mapper())` |
| all | detail | fixed generic sentence | never leak internals | per-case override |
| all | unmatched errors | fall through to core fallback | keep the safe 500 | — |
| sqlmap | `sql.ErrNoRows` | 404 | most common meaning in handlers | `WithNotFound` |
| pgxmap | PostgreSQL detail | hidden | contains row values and schema names | `WithDatabaseDetail()` **loosens a safety default** |
| pgxmap | `Retry-After` | 1 second on the default problems of retryable codes (`40001`, `40P01`, `53300`) | lets clients retry politely | `WithRetryAfter` |
| pgxmap | `57014` | 504, no `Retry-After` | a statement timeout would most likely time out again | `WithCode("57014", ...)` |
| gormmap, pgxmap | registration order | driver mapper first | keeps the driver mapper's constraint and code problems | — |
| validatormap | status | 422 | RFC 9457 example and semantics of a well-formed but invalid body | `WithStatus` (400–499) |
| validatormap | message | `failed validation: <tag>` | no values, no struct names | `WithTranslator` |
| validatormap | location | `pointer`, or `field` when JSON names are unknown, never both in one problem | never emit a wrong pointer | register `JSONName` on a validator created with `WithTagNameFuncBlankOmit()` |

## Hot Paths

- `sqlmap`: the `errors.Is` checks in order, the no-match return, the override branch (nil function, nil result)
- `gormmap`: the four sentinel checks, the no-match return, the override branch
- `pgxmap`: `errors.AsType` and the nil guard, the `WithCode` code validation, the constraint lookup, the per-code override, the table lookup, header building, the detail opt-in branch, the no-match return
- `validatormap`: `errors.AsType` and the empty guard, the per-problem location choice, entry building, pointer tokenizing and escaping, the `field` fallback, `JSONName`, translator selection including the recover branch and the recorded panic cause

## Risks / Trade-offs

- [pgx v5 or gorm major releases change error types] → Each plugin is its own module with its own major-version path. Its tests construct real error values, so incompatibilities show up as failures.
- [`WithDatabaseDetail` misused on public APIs] → The option name and godoc state the risk. The default is off, and it's listed as the only safety-loosening option.
- [Validator namespace format changes] → Pointer building is covered by table tests on real validator output (not hand-written namespaces only).
- [A translator panic is contained in the plugin, not reported by the core] → It is recorded as a `*prblm.PanicError` in the problem's cause, which the `OnError` hook receives. Documented.
- [gorm's driver translators change what they wrap] → The cross-plugin test calls the real postgres dialector's `Translate`, so a change shows up as a failure.
- [SQLSTATE table choices (e.g. `23503` → 409) don't fit every domain] → `WithCode` and `WithConstraint` let consumers change any code.

## Migration Plan

There's no migration. Release order, once every planned change is complete:
1. Transfer the repository from `zakyalvan/prblm` to `kartaladev/prblm`, so the module path resolves.
2. Tag the core `v0.1.0`; it includes `sqlmap`.
3. In `ginprblm`, `fiberprblm` and `mappers/*`, remove the replace, require `v0.1.0`, run `go mod tidy` and `GOWORK=off go test` against the real tag, and commit. `internal/paritytest` and `internal/plugintest` are unpublished and keep their replaces.
4. Tag `ginprblm/v0.1.0`, `fiberprblm/v0.1.0`, `mappers/gormmap/v0.1.0`, `mappers/pgxmap/v0.1.0` and `mappers/validatormap/v0.1.0` on that commit. No plugin requires another, so no tag waits for another.

A module can only require a core tag that already exists, which is why the core is always tagged first. Each plugin can be rolled back by unregistering its mapper. Behavior returns to the core fallback.
