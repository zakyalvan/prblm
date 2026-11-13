# Spec Delta

## Purpose

Provides an opt-in mapper for database/sql and database driver errors, so consumers get correct problem responses for common SQL outcomes without writing the mapper themselves.

## ADDED Requirements

### Requirement: Opt-in mapping with no extra dependency
The SQL mapper SHALL have no effect unless a consumer registers it with the problem handler, and SHALL depend only on the Go standard library.

#### Scenario: Registered or not
- **Given** a problem handler `<registration>`
- **When** a handler returns `sql.ErrNoRows` for `GET /v1/vessels/7`
- **Then** the response status is `<status>` with `Content-Type: application/problem+json`

##### Examples:
| registration | status |
|--------------|--------|
| with the SQL mapper registered | 404 |
| without the SQL mapper | 500 |

### Requirement: No rows maps to not found
When registered, an error chain containing `sql.ErrNoRows` SHALL produce a 404 problem with type `about:blank` and the detail `The requested resource was not found.`, never mentioning SQL.

#### Scenario: Missing row
- **Given** a problem handler with the SQL mapper registered
- **When** a handler returns `<error>`
- **Then** the response status is `<status>` and the body detail is `<detail>`
- **And** the body contains no `sql:`

##### Examples: matched
| error | status | detail |
|-------|--------|--------|
| `sql.ErrNoRows` | 404 | `The requested resource was not found.` |
| `fmt.Errorf("find vessel 7: %w", sql.ErrNoRows)` | 404 | `The requested resource was not found.` |
| `errors.Join(io.EOF, sql.ErrNoRows)` | 404 | `The requested resource was not found.` |

##### Examples: not matched
| error | status | detail |
|-------|--------|--------|
| `errors.New("sql: no rows in result set")` | 500 | (none) |

### Requirement: Connection failures map to service unavailable
When registered, an error chain containing `driver.ErrBadConn` or `sql.ErrConnDone` SHALL produce a 503 problem with the detail `The service is temporarily unavailable.`.

#### Scenario: Connection failure
- **Given** a problem handler with the SQL mapper registered
- **When** a handler returns `<error>`
- **Then** the response status is `<status>`

##### Examples:
| error | status |
|-------|--------|
| `driver.ErrBadConn` | 503 |
| `fmt.Errorf("ping: %w", driver.ErrBadConn)` | 503 |
| `sql.ErrConnDone` | 503 |

### Requirement: Programming errors are not mapped
An error chain containing `sql.ErrTxDone`, or any database/sql error not listed above, SHALL NOT be matched, so it reaches later mappers and finally the core's safe 500 fallback.

#### Scenario: Unmapped SQL errors
- **Given** a problem handler with the SQL mapper registered and no other mappers
- **When** a handler returns `<error>`
- **Then** the response is a 500 problem without detail

##### Examples:
| error |
|-------|
| `sql.ErrTxDone` |
| `fmt.Errorf("commit: %w", sql.ErrTxDone)` |
| `driver.ErrSkip` |
| `driver.ErrRemoveArgument` |

### Requirement: Absent and typed-nil errors are not matched
The SQL mapper SHALL report no match for a nil error and SHALL NOT panic on any error value.

#### Scenario: Degenerate inputs
- **Given** the SQL mapper used directly
- **When** it maps `<error>`
- **Then** the result is `<result>` and no panic occurs

##### Examples:
| error | result |
|-------|--------|
| `nil` | no match |
| a typed-nil `*MyError` stored in an `error` | no match |

### Requirement: Mapped problems are customizable
Consumers SHALL be able to replace the problem produced for the no-rows case and for the connection-failure case. An override returning nil SHALL mean "no match". An override that panics SHALL be contained by the core handler like any faulty mapper.

#### Scenario: Override results
- **Given** a problem handler with the SQL mapper registered with a no-rows override that `<override>`, followed by a consumer mapper mapping `sql.ErrNoRows` to 410
- **When** a handler returns `sql.ErrNoRows`
- **Then** the response status is `<status>` and type is `<type>`

##### Examples:
| override | status | type |
|----------|--------|------|
| returns a 404 problem with type `https://example.com/probs/not-found` | 404 | `https://example.com/probs/not-found` |
| returns nil | 410 | `about:blank` |
| panics | 410 | `about:blank` |

#### Scenario: Nil override option
- **Given** the SQL mapper constructed with a nil override function for `<case>`
- **When** a handler returns `<error>`
- **Then** the default problem for that case is used, with status `<status>`

##### Examples:
| case | error | status |
|------|-------|--------|
| no rows | `sql.ErrNoRows` | 404 |
| connection failure | `driver.ErrBadConn` | 503 |
