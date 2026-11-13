# Spec Delta

## Purpose

Provides an opt-in mapper for PostgreSQL errors reported by pgx, translating SQLSTATE codes into problems without exposing database internals such as row values or constraint names.

## ADDED Requirements

### Requirement: Opt-in mapping in its own module
The PostgreSQL mapper SHALL live in its own Go module, SHALL have no effect unless registered, and SHALL NOT add pgx to the root module's dependencies.

#### Scenario: Registered or not
- **Given** a problem handler `<registration>`
- **When** a handler returns a PostgreSQL error with code `23505`
- **Then** the response status is `<status>`

##### Examples:
| registration | status |
|--------------|--------|
| with the PostgreSQL mapper registered | 409 |
| without the PostgreSQL mapper | 500 |

### Requirement: SQLSTATE mapping
When registered, a PostgreSQL error in the chain SHALL be mapped by its SQLSTATE code to the status and generic detail in the Examples. Any other code SHALL NOT be matched.

#### Scenario: Mapped codes
- **Given** a problem handler with the PostgreSQL mapper registered
- **When** a handler returns an error wrapping a PostgreSQL error with code `<code>`
- **Then** the response status is `<status>` and the detail is `<detail>`

##### Examples: integrity violations
| code | status | detail |
|------|--------|--------|
| `23505` | 409 | `The resource conflicts with an existing one.` |
| `23503` | 409 | `The resource references, or is referenced by, another resource.` |
| `23502` | 422 | `A required value is missing.` |
| `23514` | 422 | `A value does not satisfy a constraint.` |

##### Examples: retryable and capacity
| code | status | detail |
|------|--------|--------|
| `40001` | 409 | `The request conflicted with a concurrent change; retry it.` |
| `40P01` | 409 | `The request conflicted with a concurrent change; retry it.` |
| `57014` | 503 | `The service is temporarily unavailable.` |
| `53300` | 503 | `The service is temporarily unavailable.` |

#### Scenario: Unmapped codes fall through
- **Given** a problem handler with the PostgreSQL mapper registered and no other mappers
- **When** a handler returns a PostgreSQL error with code `<code>`
- **Then** the response is a 500 problem without detail

##### Examples:
| code |
|------|
| `42P01` |
| `42601` |
| `08006` |
| (empty) |
| `ZZZZZ` |

### Requirement: Retryable errors advertise Retry-After
Problems for codes `40001`, `40P01`, `57014` and `53300` SHALL carry a `Retry-After` header, 1 second by default. Consumers SHALL be able to set another positive number of seconds; zero or negative values SHALL be rejected at construction.

#### Scenario: Retry-After header
- **Given** the PostgreSQL mapper registered with Retry-After `<setting>`
- **When** a handler returns a PostgreSQL error with code `<code>`
- **Then** the response header `Retry-After` is `<header>`

##### Examples:
| setting | code | header |
|---------|------|--------|
| default | `40P01` | `1` |
| default | `53300` | `1` |
| 5 seconds | `40001` | `5` |
| default | `23505` | (none) |

#### Scenario: Invalid Retry-After setting
- **When** the PostgreSQL mapper is constructed with Retry-After `<setting>`
- **Then** construction panics with a message naming the Retry-After option

##### Examples:
| setting |
|---------|
| 0 seconds |
| -1 seconds |

### Requirement: Database internals hidden by default
By default, mapped problems SHALL contain only the generic detail. They SHALL NOT include the PostgreSQL message, detail, hint, constraint, schema, table, column, data type, where or the values involved.

#### Scenario: Duplicate email
- **Given** a problem handler with the PostgreSQL mapper registered
- **When** a handler returns a PostgreSQL error with code `23505`, message `<message>`, detail `<detail>` and constraint `<constraint>`
- **Then** the response body contains none of `<forbidden>`

##### Examples:
| message | detail | constraint | forbidden |
|---------|--------|------------|-----------|
| `duplicate key value violates unique constraint "users_email_key"` | `Key (email)=(alice@example.com) already exists.` | `users_email_key` | `alice@example.com`, `users_email_key`, `duplicate key` |
| `insert or update on table "orders" violates foreign key constraint "orders_user_fk"` | `Key (user_id)=(42) is not present in table "users".` | `orders_user_fk` | `orders`, `users`, `42` |

### Requirement: Explicit opt-in exposes database detail
Consumers SHALL be able to opt in to putting the PostgreSQL detail text into the problem detail, for internal APIs. Without the opt-in the detail stays generic.

#### Scenario: Detail exposure
- **Given** the PostgreSQL mapper registered `<option>`
- **When** a handler returns a code `23505` error with detail `Key (email)=(alice@example.com) already exists.`
- **Then** the response detail is `<detail>`

##### Examples:
| option | detail |
|--------|--------|
| with database detail exposed | `Key (email)=(alice@example.com) already exists.` |
| without options | `The resource conflicts with an existing one.` |
| with database detail exposed, but the PostgreSQL detail is empty | `The resource conflicts with an existing one.` |

### Requirement: Per-constraint problems
Consumers SHALL be able to register a problem function for a named constraint. It SHALL take precedence over the SQLSTATE mapping for errors on that constraint. A function returning nil SHALL fall back to the SQLSTATE mapping. Registering an empty constraint name or a nil function SHALL panic at construction.

#### Scenario: Friendly duplicate email
- **Given** the PostgreSQL mapper registered with constraint `users_email_key` mapped to a function that `<function>`
- **When** a handler returns a code `23505` error on constraint `<constraint>`
- **Then** the response status is `<status>` and title is `<title>`

##### Examples:
| function | constraint | status | title |
|----------|------------|--------|-------|
| returns a 409 problem titled `Email already registered` | `users_email_key` | 409 | `Email already registered` |
| returns a 409 problem titled `Email already registered` | `users_phone_key` | 409 | `Conflict` |
| returns a 409 problem titled `Email already registered` | `USERS_EMAIL_KEY` | 409 | `Conflict` |
| returns nil | `users_email_key` | 409 | `Conflict` |
| panics | `users_email_key` | 500 | `Internal Server Error` |

#### Scenario: Invalid constraint registration
- **When** the PostgreSQL mapper is constructed with constraint `<name>` and function `<function>`
- **Then** construction panics with a message naming `<argument>`

##### Examples:
| name | function | argument |
|------|----------|----------|
| (empty) | a valid function | the constraint name |
| `users_email_key` | nil | the problem function |

### Requirement: Per-code problems
Consumers SHALL be able to register a problem function for a SQLSTATE code, including codes the default table does not list. It SHALL replace the default for that code; a nil result SHALL fall back to the default table, or to no match for unlisted codes.

#### Scenario: Code overrides
- **Given** the PostgreSQL mapper registered with code `<code>` mapped to a function that `<function>`
- **When** a handler returns a PostgreSQL error with code `<code>`
- **Then** the response status is `<status>`

##### Examples:
| code | function | status |
|------|----------|--------|
| `23503` | returns a 422 problem | 422 |
| `23P01` | returns a 409 problem | 409 |
| `23503` | returns nil | 409 |
| `42P01` | returns nil | 500 |

### Requirement: Absent and non-PostgreSQL errors are not matched
The PostgreSQL mapper SHALL report no match for nil, for a typed-nil PostgreSQL error, and for errors without a PostgreSQL error in the chain, and SHALL NOT panic.

#### Scenario: Degenerate inputs
- **Given** the PostgreSQL mapper used directly
- **When** it maps `<error>`
- **Then** the result is `<result>`

##### Examples:
| error | result |
|-------|--------|
| `nil` | no match |
| a typed-nil `*pgconn.PgError` stored in an `error` | no match |
| `pgx.ErrNoRows` | no match |
| `errors.New("ERROR: duplicate key (SQLSTATE 23505)")` | no match |
| `errors.Join(io.EOF, <a 23505 PostgreSQL error>)` | status 409 |
