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
- Each plugin is usable on its own and combinable with the others, in any order the consumer chooses.

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
   - Scenarios: all gorm-error-mapping scenarios except "gorm without translation" with the PostgreSQL mapper row. That row needs slice 3, which adds it.
   - Hot paths: the `gormmap` match, no-match and override paths.
3. **`pgxmap` module.**
   - At its end: SQLSTATE table, generic details, `Retry-After`, per-constraint overrides, and the `WithDatabaseDetail` opt-in. The deferred gorm pass-through row is added here as a cross-plugin test.
   - Scenarios: all pgx-error-mapping scenarios, plus gorm-error-mapping / gorm without translation.
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
  - **The consumer registers plugins in an order where one shadows another** (e.g. `sqlmap` before `pgxmap` for a pgx error): no shadowing is possible. `sqlmap` matches only database/sql sentinels, and pgx errors aren't wrapped in them.

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

- `errors.Is` against `gorm.ErrRecordNotFound` and `gorm.ErrDuplicatedKey`. No other gorm sentinel is mapped.
- The godoc states that `ErrDuplicatedKey` requires `gorm.Config{TranslateError: true}`. Without it, register `pgxmap` (or another driver mapper) to handle the raw driver error.
- Tests use `gorm.io/driver/sqlite` (test-only dependency of the `gormmap` module) to produce real gorm errors.
- **Failure modes:**
  - **TranslateError is off:** documented, and the pass-through is tested in slice 3.
  - **gorm wraps the sentinel with extra context:** `errors.Is` still matches.

### D5. pgxmap

```go
func Mapper(opts ...Option) prblm.Mapper
func WithDatabaseDetail() Option                                        // loosens a safety default
func WithConstraint(name string, fn func(*pgconn.PgError) *prblm.Problem) Option
func WithRetryAfter(d time.Duration) Option                             // > 0, whole seconds
func WithCode(code string, fn func(*pgconn.PgError) *prblm.Problem) Option // per-SQLSTATE override
```

- It imports only `github.com/jackc/pgx/v5/pgconn`, but the module still requires `github.com/jackc/pgx/v5`.
- Lookup:
  1. `pe, ok := errors.AsType[*pgconn.PgError](err)`; if `!ok || pe == nil`, no match
  2. a constraint function for `pe.ConstraintName` (case-sensitive exact match); a non-nil result wins
  3. a per-code override
  4. the default table
  5. otherwise, no match
- The default table is a package-level `map[string]entry{status, detail, retry bool}`, read-only after init.
- `Retry-After` is set through `prblm.WithHeader` on the produced problem, in whole seconds (`strconv.Itoa(int(d / time.Second))`). Construction panics when `d < time.Second`.
- `WithDatabaseDetail` replaces the generic detail with `pe.Detail` only when that's non-empty. The message, hint, schema, table, column, data type and where fields are never used.
- *Alternative:* mapping by SQLSTATE class (`23xxx`) instead of exact codes. Rejected, because classes mix client and server faults (e.g. `23P01` exclusion violations need their own decision), and an explicit table is easier to review.
- **Failure modes:**
  - **A typed-nil `*pgconn.PgError`:** guarded by `pe == nil`.
  - **An empty or unknown code:** no match.
  - **A constraint function panics:** core containment, so no match and a 500 (spec row).
  - **An empty constraint name, nil function, or non-positive `Retry-After`:** construction panics.
  - **The consumer exposes detail on a public API:** documented risk of that explicit opt-in.

### D6. validatormap

```go
func Mapper(opts ...Option) prblm.Mapper
func WithStatus(code int) Option       // 400..499, else construction panic
func WithType(uri string) Option
func WithTitle(title string) Option
func WithTranslator(fn func(ctx context.Context) ut.Translator) Option
```

The mapper:
- `ves, ok := errors.AsType[validator.ValidationErrors](err)`; if `!ok || len(ves) == 0`, no match. `InvalidValidationError` is a different type, so it never matches.
- The problem gets the status (default 422), type and title (empty by default, so the core fills in the reason phrase), detail `The request is not valid.`, and the `errors` extension.
- Entries are `[]entry` with `Detail string \`json:"detail"\``, `Pointer string \`json:"pointer,omitempty"\`` and `Field string \`json:"field,omitempty"\``. They're plain structs, so the core encodes JSON and XML from the same tags.

Building the pointer from `fe.Namespace()`:
- If `fe.Namespace() == fe.StructNamespace()`, no tag name function is registered (or the names coincide), so fall back to `field`. The coincidence case, e.g. tags equal to struct names, is acceptable: the field name is still correct.
- Strip the first segment (the root struct) when there is one; see the anonymous-struct failure mode below. An empty remainder gives the pointer `#`.
- Tokenize on `.` and `[...]`. Each token is escaped per RFC 6901 (`~` → `~0`, `/` → `~1`), then percent-encoded for the URI fragment form (`url.PathEscape` semantics on non-unreserved characters).

The message:
- Call the translator function, if any, guarded by `defer recover()`. A panic or a nil result uses the default message.
- With a translator, use `fe.Translate(t)`, otherwise `"failed validation: " + fe.Tag()`.
- Never use `fe.Value()`, `fe.Param()` or `fe.Error()`. Each can carry the rejected value, struct names or parameters.

On the translator panic: it's recovered locally rather than by the core. A translator panic is a problem with an optional enhancement, not with the mapper, so losing the whole 422 to a 500 would be worse for clients. This is the one place a plugin contains a panic itself, and it reports nothing, since the plugin has no hook access. It's documented.

- *Alternative:* RFC 7807-era `invalid-params` with `name`/`reason`. Rejected in favor of the RFC 9457 §3 example shape.
- **Failure modes:**
  - **An empty `ValidationErrors`:** no match.
  - **A non-ASCII map key:** percent-encoded.
  - **A namespace with no root segment:** `#`.
  - **A translator missing the tag:** `fe.Translate` falls back to `fe.Error()`, which names the Go struct (found in implementation, `TestMapper_TranslatorGaps`). The mapper detects that fallback and uses the default message instead.
  - **A translation that panics:** recovered per entry, like the translator function, and the default message is used.
  - **An anonymous root struct:** the validator reports no root segment for it (found in implementation, `TestPointer/anonymous_struct`). A root is stripped only when `Namespace()` and `StructNamespace()` start with the same segment; with equal namespaces, a single segment is a field. A nested field of an anonymous struct whose first JSON name equals its Go name stays ambiguous; documented.
  - **A huge number of failed fields:** entries are bounded by what the validator returns. No cap is added; documented.

### D7. Module and CI wiring

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
| pgxmap | `Retry-After` | 1 second on retryable codes | lets clients retry politely | `WithRetryAfter` |
| validatormap | status | 422 | RFC 9457 example and semantics of a well-formed but invalid body | `WithStatus` (400–499) |
| validatormap | message | `failed validation: <tag>` | no values, no struct names | `WithTranslator` |
| validatormap | location | `pointer`, or `field` when JSON names are unknown | never emit a wrong pointer | register a tag name function on the validator |

## Hot Paths

- `sqlmap`: the `errors.Is` checks in order, the no-match return, the override branch (nil function, nil result)
- `gormmap`: both sentinel checks, the no-match return, the override branch
- `pgxmap`: `errors.AsType` and the nil guard, the constraint lookup, the per-code override, the table lookup, header building, the detail opt-in branch, the no-match return
- `validatormap`: `errors.AsType` and the empty guard, entry building, pointer tokenizing and escaping, the `field` fallback, translator selection including the recover branch

## Risks / Trade-offs

- [pgx v5 or gorm major releases change error types] → Each plugin is its own module with its own major-version path. Its tests construct real error values, so incompatibilities show up as failures.
- [`WithDatabaseDetail` misused on public APIs] → The option name and godoc state the risk. The default is off, and it's listed as the only safety-loosening option.
- [Validator namespace format changes] → Pointer building is covered by table tests on real validator output (not hand-written namespaces only).
- [A translator panic is contained in the plugin, not reported to the hook] → Documented. Consumers can wrap their translator function to log the panic.
- [SQLSTATE table choices (e.g. `23503` → 409) don't fit every domain] → `WithCode` and `WithConstraint` let consumers change any code.

## Migration Plan

There's no migration. Release order, once every planned change is complete:
1. Transfer the repository from `zakyalvan/prblm` to `kartaladev/prblm`, so the module path resolves.
2. Tag the core `v0.1.0`; it includes `sqlmap`.
3. In `ginprblm`, `fiberprblm` and `mappers/*`, remove the replace, require `v0.1.0`, run `go mod tidy` and `GOWORK=off go test` against the real tag, and commit. `internal/paritytest` is unpublished and keeps its replaces.
4. Tag `ginprblm/v0.1.0`, `fiberprblm/v0.1.0`, `mappers/gormmap/v0.1.0`, `mappers/pgxmap/v0.1.0` and `mappers/validatormap/v0.1.0` on that commit.

A module can only require a core tag that already exists, which is why the core is always tagged first. Each plugin can be rolled back by unregistering its mapper. Behavior returns to the core fallback.
