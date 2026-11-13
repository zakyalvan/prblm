# error-mapping Specification

## Purpose

Defines how a Go error becomes a problem: the consumer-injectable mapper contract, the resolution order, the status hint, request-scoped mappers, the safe fallback, and the hooks around mapping, including faulty consumer code.

## Requirements

### Requirement: Mapper contract
A mapper SHALL receive the request context and the error and return either a problem (a match) or nil (no match). No status code or other sentinel value SHALL signal "no match".

#### Scenario: Match or no match
- **Given** a consumer mapper that maps `ErrVesselNotFound` to status 404
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>`

##### Examples:
| error | status |
|-------|--------|
| `ErrVesselNotFound` | 404 |
| `errors.New("other")` | 500 |

### Requirement: Sentinel and typed mapping helpers
The library SHALL provide helpers that build a mapper from a sentinel error (matched as `errors.Is` does), from an error type (matched as `errors.As` does, generic over the type), and from a sentinel plus a status alone.

#### Scenario: Sentinel helper
- **Given** a mapper built for sentinel `ErrVesselNotFound` returning status 404
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>`

##### Examples:
| error | status |
|-------|--------|
| `ErrVesselNotFound` | 404 |
| `fmt.Errorf("load: %w", ErrVesselNotFound)` | 404 |
| `errors.Join(io.EOF, ErrVesselNotFound)` | 404 |
| `errors.New("vessel not found")` | 500 |

#### Scenario: Typed helper exposes the value
- **Given** a mapper built for type `*RefusedError` that returns status 409 with extension `reasons` from the value
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>` and extension `reasons` is `<reasons>`

##### Examples:
| error | status | reasons |
|-------|--------|---------|
| `&RefusedError{Reasons: ["closed"]}` | 409 | `["closed"]` |
| `fmt.Errorf("x: %w", &RefusedError{Reasons: ["full"]})` | 409 | `["full"]` |
| `errors.New("refused")` | 500 | (none) |

### Requirement: Resolution order and first match wins
The library SHALL resolve an error in this order and stop at the first match: (1) self-describing errors, (2) request-scoped mappers, (3) consumer mappers, (4) built-in mappers, (5) the status hint, (6) the fallback. Within one step, mappers SHALL run in registration order.

#### Scenario: Precedence between steps
- **Given** `<setup>`
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>`

##### Examples:
| setup | error | status |
|-------|-------|--------|
| consumer mapper `*json.SyntaxError` to 400 (built-in maps it to 400 too) and one to 418 registered first | a `*json.SyntaxError` | 418 |
| consumer mapper `*json.UnmarshalTypeError` to 400 | a `*json.UnmarshalTypeError` | 400 |
| scoped mapper `ErrLocked` to 423 and consumer mapper `ErrLocked` to 409 | `ErrLocked` | 423 |
| consumer mapper `ErrLocked` to 409 | `WithStatus(ErrLocked, 400)` | 409 |
| no mappers | `WithStatus(ErrLocked, 400)` | 400 |
| consumer mapper `ErrLocked` to 409 | a problem with status 422 wrapping `ErrLocked` | 422 |

#### Scenario: First registered consumer mapper wins
- **Given** consumer mappers A (`ErrX` to 409) and B (`ErrX` to 410) registered as `<order>`
- **When** the handler resolves `ErrX`
- **Then** the resolved status is `<status>`

##### Examples:
| order | status |
|-------|--------|
| A, B | 409 |
| B, A | 410 |

### Requirement: Self-describing errors
An error chain that contains a problem, or an error that implements the self-describing contract, SHALL resolve to that problem without calling any mapper. The outermost self-describing error in the chain SHALL win.

#### Scenario: Problem or describer in the chain
- **Given** a consumer mapper that counts its calls
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>`
- **And** the mapper was called `<calls>` times

##### Examples:
| error | status | calls |
|-------|--------|-------|
| a problem with status 409 | 409 | 0 |
| `fmt.Errorf("op: %w", problem with status 409)` | 409 | 0 |
| a domain error whose self-description has status 422 | 422 | 0 |
| a problem 400 wrapping a describer with status 422 | 400 | 0 |
| a describer returning nil | 500 | 1 |

### Requirement: Resolved problems are copies
The library SHALL copy a resolved problem before finalizing or enriching it, so problems shared by many requests (for example, package-level values) are never modified.

#### Scenario: Shared problem reused concurrently
- **Given** a package-level problem with status 409 returned by a mapper, and an enricher that sets extension `trace_id` from the request
- **When** `<requests>` concurrent requests resolve it
- **Then** each response carries its own `trace_id`
- **And** the package-level problem still has no extensions and no instance

##### Examples:
| requests |
|----------|
| 1 |
| 100 |

### Requirement: Status hint
Callers SHALL be able to attach a status hint to an error. When no self-describing error or mapper matches, the hint SHALL produce an `about:blank` problem with that status and no detail. A hint outside 400–599 SHALL be ignored.

#### Scenario: Hint used when unmapped
- **Given** no consumer mappers
- **When** the handler resolves `WithStatus(errors.New("secret text"), <hint>)`
- **Then** the resolved status is `<status>`, the title is `<title>` and the response contains no `secret text`

##### Examples: valid hints
| hint | status | title |
|------|--------|-------|
| 404 | 404 | `Not Found` |
| 403 | 403 | `Forbidden` |
| 599 | 599 | (empty) |

##### Examples: ignored hints
| hint | status | title |
|------|--------|-------|
| 200 | 500 | `Internal Server Error` |
| 399 | 500 | `Internal Server Error` |
| 600 | 500 | `Internal Server Error` |
| 0 | 500 | `Internal Server Error` |
| -1 | 500 | `Internal Server Error` |

#### Scenario: Nested and wrapped hints
- **Given** no consumer mappers
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>`

##### Examples:
| error | status |
|-------|--------|
| `WithStatus(WithStatus(err, 400), 404)` | 404 |
| `fmt.Errorf("x: %w", WithStatus(err, 401))` | 401 |
| `WithStatus(nil, 404)` | no error (nil) |

### Requirement: Request-scoped mappers
Consumers SHALL be able to attach mappers to a request context, for example in a route-group middleware. They apply only to that request and run before consumer mappers. Attaching to a context that already has scoped mappers SHALL add to them, the newer ones first.

#### Scenario: Scoped only on its route
- **Given** a scoped mapper `ErrLocked` to 423 attached for routes under `/v1/contracts`
- **When** `ErrLocked` is returned for `<path>`
- **Then** the response status is `<status>`

##### Examples:
| path | status |
|------|--------|
| `/v1/contracts/1` | 423 |
| `/v1/vessels/1` | 500 |

#### Scenario: Nested scopes
- **Given** an outer scope mapping `ErrLocked` to 423 and an inner scope mapping `ErrLocked` to 409
- **When** `ErrLocked` is returned from a route inside both scopes
- **Then** the response status is `<status>`

##### Examples:
| status |
|--------|
| 409 |

### Requirement: Built-in mappers
With zero configuration, the library SHALL map these standard-library errors with a generic detail and never the error text. Consumers SHALL be able to disable all built-ins. A bare `io.EOF` SHALL NOT be mapped: it can come from any reader, including upstream services, so it falls to the safe fallback.

#### Scenario: Built-in mapping
- **Given** a handler with `<builtins>`
- **When** the handler resolves `<error>`
- **Then** the resolved status is `<status>` and the detail is `<detail>`

##### Examples: enabled (default)
| builtins | error | status | detail |
|----------|-------|--------|--------|
| default options | a `*json.SyntaxError` | 400 | `The request body is not valid JSON.` |
| default options | `io.ErrUnexpectedEOF` | 400 | `The request body is incomplete.` |
| default options | `io.EOF` | 500 | (empty) |
| default options | a `*json.UnmarshalTypeError` for field `age` | 422 | `The request body has a value of the wrong type.` |
| default options | a `*http.MaxBytesError` | 413 | `The request body is too large.` |
| default options | `context.DeadlineExceeded` | 504 | (empty) |

##### Examples: disabled
| builtins | error | status | detail |
|----------|-------|--------|--------|
| built-ins disabled | a `*json.SyntaxError` | 500 | (empty) |
| built-ins disabled | `context.DeadlineExceeded` | 500 | (empty) |

### Requirement: Safe fallback
An error that matches no step SHALL produce status 500, type `about:blank`, title `Internal Server Error`, no detail and no extensions except those added by enrichers. The error text SHALL never appear in the response.

#### Scenario: Unmapped error text never leaks
- **Given** a handler with zero configuration
- **When** the handler writes `<error>`
- **Then** the status is 500 and the body contains no part of `<text>`

##### Examples:
| error | text |
|-------|------|
| `errors.New("pq: connection refused to 10.0.0.5")` | `10.0.0.5` |
| `fmt.Errorf("user alice@example.com: %w", io.ErrClosedPipe)` | `alice@example.com` |
| a typed-nil `*MyError` stored in an `error` | `MyError` |

### Requirement: Mapper results are normalized
A mapper result with a status outside 400–599 SHALL be treated as 500, and the mismatch SHALL be reported to the `OnError` hook. A mapper result without a status SHALL be treated as 500.

#### Scenario: Out-of-range status from a mapper
- **Given** a consumer mapper that returns a problem with status `<returned>` for `ErrX`
- **When** the handler resolves `ErrX`
- **Then** the resolved status is `<status>`
- **And** the hook received an error wrapping `ErrInvalidStatus`: `<reported>`

##### Examples:
| returned | status | reported |
|----------|--------|----------|
| 404 | 404 | no |
| 0 | 500 | no |
| 200 | 500 | yes |
| 302 | 500 | yes |
| 600 | 500 | yes |

### Requirement: Faulty consumer code never breaks resolution
A mapper, describer or enricher that panics SHALL NOT crash the request. A panicking mapper or describer SHALL be treated as no match, a panicking enricher SHALL be skipped. Each panic SHALL be reported to the `OnError` hook, and a panicking hook SHALL be ignored.

#### Scenario: Consumer code panics
- **Given** `<faulty>` and a consumer mapper `ErrX` to 409 registered after it
- **When** the handler writes `ErrX`
- **Then** the status is `<status>`
- **And** the hook received `<hook>`

##### Examples:
| faulty | status | hook |
|--------|--------|------|
| a mapper that panics, registered first | 409 | a panic error, then `ErrX` |
| a describer error that panics wrapping `ErrX` | 409 | a panic error, then `ErrX` |
| an enricher that panics | 409 | a panic error, then `ErrX` |
| a hook that panics | 409 | (hook panics, response unaffected) |
| a nil mapper in the option list | 409 | `ErrX` |
| a nil enricher in the option list | 409 | `ErrX` |

### Requirement: Enrichers
Consumers SHALL be able to register enrichers that run, in order, on every resolved problem before it is written. By default, an enricher SHALL set `instance` to the request path, without the query string, when the problem has no instance.

#### Scenario: Default instance
- **Given** a handler with zero configuration
- **When** `<problem>` is written for request `<request>`
- **Then** the instance is `<instance>`

##### Examples:
| problem | request | instance |
|---------|---------|----------|
| an unmapped error | `GET /v1/vessels/7?token=abc` | `/v1/vessels/7` |
| a problem with instance `urn:uuid:1` | `GET /v1/vessels/7` | `urn:uuid:1` |
| an unmapped error | `GET /` | `/` |

#### Scenario: Custom enricher
- **Given** an enricher that sets extension `trace_id` from the request context
- **When** `<error>` is written
- **Then** the body contains `trace_id`

##### Examples:
| error |
|-------|
| an unmapped error (fallback) |
| a mapped `ErrVesselNotFound` |
| a self-describing problem |

### Requirement: OnError hook
Consumers SHALL be able to register a hook that is called once per handled error with the request context, the original error and the resolved problem. The library SHALL NOT log by itself.

#### Scenario: Hook sees the original error
- **Given** a hook that records its calls
- **When** the handler writes `<error>`
- **Then** the hook was called `<calls>` times with error `<error>` and problem status `<status>`

##### Examples:
| error | calls | status |
|-------|-------|--------|
| `errors.New("pq: connection refused")` | 1 | 500 |
| `ErrVesselNotFound` (mapped to 404) | 1 | 404 |

### Requirement: Nil errors are not problems
Resolving or writing a nil error SHALL produce no problem, write nothing and not call the hook.

#### Scenario: Nil error
- **Given** a hook that records its calls
- **When** the handler writes `<error>`
- **Then** nothing is written and the hook was called 0 times

##### Examples:
| error |
|-------|
| `nil` |
| `WithStatus(nil, 404)` |

### Requirement: Canceled requests are answered like any other
A canceled request context SHALL NOT stop the library from producing a problem response: the error SHALL be resolved, reported to the hook and answered like any other. A client that is still connected receives the problem; when the client has gone away, the server drops the write.

#### Scenario: Request context canceled
- **Given** a request whose context is `<ctx>`
- **When** the handler writes `<error>`
- **Then** the response is `<response>`
- **And** the hook was called 1 time

##### Examples:
| ctx | error | response |
|-----|-------|----------|
| canceled | `fmt.Errorf("query: %w", context.Canceled)` | a 500 problem |
| active | `context.Canceled` | a 500 problem |
| canceled | `ErrVesselNotFound` | a 404 problem |

### Requirement: Zero-configuration handler and concurrent use
A handler built with no options SHALL work with the built-ins, the default enricher and the fallback. A handler SHALL be safe for concurrent use and SHALL NOT change after construction.

#### Scenario: Zero options
- **Given** a handler created with `<options>`
- **When** the handler writes a `*json.SyntaxError` from `<requests>` concurrent requests
- **Then** every response is a 400 problem

##### Examples:
| options | requests |
|---------|----------|
| no options | 1 |
| no options | 100 |
| a nil option | 1 |
