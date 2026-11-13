# Spec Delta

## Purpose

Provides an opt-in mapper for gorm's sentinel errors, so services using gorm get not-found and duplicate-key problems without writing the mapper themselves.

## ADDED Requirements

### Requirement: Opt-in mapping in its own module
The gorm mapper SHALL live in its own Go module, SHALL have no effect unless registered, and SHALL NOT add gorm to the root module's dependencies.

#### Scenario: Registered or not
- **Given** a problem handler `<registration>`
- **When** a handler returns `gorm.ErrRecordNotFound` for `GET /v1/vessels/7`
- **Then** the response status is `<status>`

##### Examples:
| registration | status |
|--------------|--------|
| with the gorm mapper registered | 404 |
| without the gorm mapper | 500 |

#### Scenario: Root module stays dependency-free
- **Given** a project requiring the root module and the gorm mapper module
- **When** the root module's own requirements are listed
- **Then** they contain `<modules>`

##### Examples:
| modules |
|---------|
| no `gorm.io/gorm` and no other third-party module |

### Requirement: Record not found
When registered, an error chain containing `gorm.ErrRecordNotFound` SHALL produce a 404 problem with the detail `The requested resource was not found.`.

#### Scenario: Missing record
- **Given** a problem handler with the gorm mapper registered
- **When** a handler returns `<error>`
- **Then** the response status is `<status>` and the body contains no `record not found`

##### Examples:
| error | status |
|-------|--------|
| `gorm.ErrRecordNotFound` | 404 |
| `fmt.Errorf("find: %w", gorm.ErrRecordNotFound)` | 404 |
| `errors.New("record not found")` | 500 |

### Requirement: Duplicated key
When registered, an error chain containing `gorm.ErrDuplicatedKey` SHALL produce a 409 problem with the detail `The resource conflicts with an existing one.`, without database values.

#### Scenario: Duplicate key with translation enabled
- **Given** gorm configured with `TranslateError: true` and the gorm mapper registered
- **When** an insert violates unique constraint `users_email_key` with value `alice@example.com`
- **Then** the response status is `<status>`
- **And** the body contains neither `users_email_key` nor `alice@example.com`

##### Examples:
| status |
|--------|
| 409 |

### Requirement: Untranslated driver errors pass through
The gorm mapper SHALL NOT match driver errors gorm did not translate, leaving them to other mappers such as the PostgreSQL mapper.

#### Scenario: gorm without translation
- **Given** gorm configured with `TranslateError: false` and the mappers `<mappers>` registered in that order
- **When** an insert violates a unique constraint (SQLSTATE `23505`)
- **Then** the response status is `<status>`

##### Examples:
| mappers | status |
|---------|--------|
| gorm mapper, PostgreSQL mapper | 409 |
| gorm mapper only | 500 |

### Requirement: Absent and other errors are not matched
The gorm mapper SHALL report no match for nil and for any error other than its two sentinels, and SHALL NOT panic.

#### Scenario: Degenerate inputs
- **Given** the gorm mapper used directly
- **When** it maps `<error>`
- **Then** the result is `<result>`

##### Examples:
| error | result |
|-------|--------|
| `nil` | no match |
| `gorm.ErrInvalidTransaction` | no match |
| `gorm.ErrInvalidData` | no match |
| a typed-nil `*MyError` stored in an `error` | no match |

### Requirement: Mapped problems are customizable
Consumers SHALL be able to replace the problem produced for each gorm sentinel. An override returning nil SHALL mean "no match"; a panicking override SHALL be contained by the core handler; a nil override function SHALL keep the default.

#### Scenario: Override results
- **Given** the gorm mapper registered with a record-not-found override that `<override>`
- **When** a handler returns `gorm.ErrRecordNotFound`
- **Then** the response status is `<status>` and title is `<title>`

##### Examples:
| override | status | title |
|----------|--------|-------|
| returns a 404 problem titled `Resource not found` | 404 | `Resource not found` |
| is a nil function | 404 | `Not Found` |
| returns nil | 500 | `Internal Server Error` |
| panics | 500 | `Internal Server Error` |
