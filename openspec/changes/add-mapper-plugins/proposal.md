# Proposal

## Why

Most Go services return the same kinds of errors from their data and validation layers: database/sql sentinels, PostgreSQL errors from pgx, gorm sentinels and go-playground/validator errors. Each one needs a correct problem response. Writing those mappers in every service is repetitive and easy to get wrong in both directions: generic 500s for client mistakes, or row values and constraint names leaked to clients. This change ships opt-in mapper plugins that turn those errors into safe RFC 9457 problems using the core mapper API. It builds on `add-problem-details-core` (archived) and is implemented after `add-framework-adapters`, because it ends with the release of every module: the core, the adapters and the plugins.

## What Changes

- `mappers/sqlmap`, a package of the root module that uses only the standard library:
  - `sql.ErrNoRows` → 404
  - `driver.ErrBadConn` and `sql.ErrConnDone` → 503
  - `sql.ErrTxDone` deliberately not matched
- `mappers/gormmap`, its own module: `gorm.ErrRecordNotFound` → 404, and the sentinels gorm's driver translators return: `gorm.ErrDuplicatedKey` and `gorm.ErrForeignKeyViolated` → 409, `gorm.ErrCheckConstraintViolated` → 422. Driver errors gorm didn't translate are left to other mappers; the documented order registers the driver mapper first.
- `mappers/pgxmap`, its own module: PostgreSQL errors mapped by SQLSTATE.
  - integrity violations → 409 or 422
  - retryable conflicts and capacity errors → 409 or 503, with `Retry-After`
  - canceled statements (`57014`) → 504, without `Retry-After`
  - unlisted codes not matched
  - per-constraint problem overrides
- `mappers/validatormap`, its own module:
  - `validator.ValidationErrors` → 422 with the RFC 9457 `errors: [{detail, pointer}]` extension
  - JSON Pointers built from JSON field names, with `JSONName`, a tag name function that follows `encoding/json` (embedded structs included)
  - optional translation per request
  - configurable status, type and title
- Every plugin exposes one constructor returning a core `prblm.Mapper`, plus options to replace the problem produced for each case.

## Defaults and Customization

- **Opt-in by registration:** a plugin does nothing until a consumer registers its mapper with `prblm.WithMappers`. Importing a plugin package changes no behavior.
- **Zero-configuration output leaks nothing:**
  - Every mapped problem uses a fixed, generic `detail`.
  - Error text, SQL, PostgreSQL message/detail/hint, constraint, table, column and row values never appear.
  - Validation `errors` entries contain only the field pointer and the validation message, never the rejected value.
- **Unmatched errors fall through:** errors a plugin doesn't recognize (for example `sql.ErrTxDone`, an unlisted SQLSTATE, `validator.InvalidValidationError`) are left to later mappers and finally to the core's safe 500 fallback.
- **Opt-in customization:**
  - replace the problem for any mapped case
  - per-constraint problems (pgxmap)
  - the validation status, type and title
  - a translator per request (validatormap)
  - the `Retry-After` value (pgxmap)
- **The one opt-in that loosens a safety default** is `pgxmap.WithDatabaseDetail()`, which puts the PostgreSQL `Detail` text into the problem `detail`. It's named explicitly and documented for internal APIs only.

## Capabilities

### New Capabilities

- `sql-error-mapping`: Opt-in mapping of database/sql and driver errors.
- `gorm-error-mapping`: Opt-in mapping of gorm sentinel errors.
- `pgx-error-mapping`: Opt-in mapping of PostgreSQL errors by SQLSTATE without exposing database internals.
- `validation-error-mapping`: Opt-in mapping of go-playground/validator errors to the RFC 9457 validation-error shape, with optional translation.

### Modified Capabilities

None.

## Scope by Gall's Law

**The simplest version that works:** `sqlmap`, with no new dependency, inside the root module, registered on a core handler and returning a 404 problem for `sql.ErrNoRows` over net/http.

Each later slice adds one plugin to that working system, in order of increasing complexity:
1. `gormmap`: four sentinels
2. `pgxmap`: a code table, overrides and headers
3. `validatormap`: building pointers and translation

Deliberately left out:
- a `lib/pq` mapper
- MySQL and SQLite driver mappers
- validation libraries other than go-playground/validator

## Impact

- New modules:
  - `github.com/kartaladev/prblm/mappers/gormmap` (requires `gorm.io/gorm`)
  - `github.com/kartaladev/prblm/mappers/pgxmap` (requires `github.com/jackc/pgx/v5`)
  - `github.com/kartaladev/prblm/mappers/validatormap` (requires `github.com/go-playground/validator/v10` and `github.com/go-playground/universal-translator`)
- New package in the root module: `github.com/kartaladev/prblm/mappers/sqlmap`. It adds no dependency, and it ships in the core's `v0.1.0`.
- **Release:** nothing is tagged until all planned changes are complete. Until then the plugin modules point at the core in this repository with `replace github.com/kartaladev/prblm => ../..`. The final step of this change transfers the repository to `kartaladev`, tags the core `v0.1.0`, removes the replaces, and tags the adapter and plugin modules.
- `go.work` gains the three new modules. CI gains per-module jobs and a check that the root module still has no third-party requirements.
- **Public API (compatibility decision):** each plugin's `Mapper(opts ...Option) prblm.Mapper` constructor and its options. Nothing in the core API changes.
- **Default responses (compatibility decision):**
  - validation failures default to 422, not 400
  - `sql.ErrNoRows` and `gorm.ErrRecordNotFound` map to 404 only when the plugin is registered
  - PostgreSQL details are hidden unless explicitly exposed
