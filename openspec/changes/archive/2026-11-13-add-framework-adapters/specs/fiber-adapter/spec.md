# Spec Delta

## Purpose

Defines how the library integrates with fiber v3 so that errors returned from fiber handlers, fiber's own routing errors and recovered panics become the same RFC 9457 problem responses the core produces.

## ADDED Requirements

### Requirement: Error handler renders returned errors as problems
The fiber adapter SHALL provide a function usable as the application's error handler. Every error returned from a handler or middleware SHALL be written as a problem resolved by the configured core handler.

#### Scenario: Returned errors
- **Given** a fiber app whose error handler is the adapter, with a mapper `ErrVesselNotFound` to 404
- **When** the handler for `GET /v1/vessels/7` returns `<error>`
- **Then** the response status is `<status>`, the `Content-Type` is `application/problem+json` and the instance is `/v1/vessels/7`

##### Examples:
| error | status |
|-------|--------|
| `ErrVesselNotFound` | 404 |
| `fmt.Errorf("load: %w", ErrVesselNotFound)` | 404 |
| `errors.New("pq: password=hunter2")` | 500 |
| a problem with status 409 | 409 |

#### Scenario: Unmapped error text never leaks
- **Given** a fiber app whose error handler is the adapter with zero configuration
- **When** a handler returns `errors.New("<text>")`
- **Then** the response status is 500 and the body does not contain `<text>`

##### Examples:
| text |
|------|
| `dial tcp 10.0.0.5:5432: connection refused` |
| `user alice@example.com not permitted` |

### Requirement: Fiber errors are status hints without their message
A `*fiber.Error` in the error chain SHALL supply its code as the status hint, and its message SHALL never appear in the response. This SHALL cover fiber's own routing errors.

#### Scenario: Fiber-generated and handler fiber errors
- **Given** a fiber app whose error handler is the adapter, with a route `GET /v1/vessels/:id`
- **When** a client sends `<request>`
- **Then** the response status is `<status>`, the title is `<title>` and the body does not contain `<hidden>`

##### Examples: routing
| request | status | title | hidden |
|---------|--------|-------|--------|
| `GET /v1/unknown` | 404 | `Not Found` | `Cannot GET` |
| `DELETE /v1/vessels/7` | 405 | `Method Not Allowed` | `Method Not Allowed` in detail |

##### Examples: handler-returned
| request | status | title | hidden |
|---------|--------|-------|--------|
| `GET /v1/vessels/7` returning `fiber.NewError(403, "token abc expired")` | 403 | `Forbidden` | `token abc` |
| `GET /v1/vessels/7` returning `fiber.ErrBadRequest` | 400 | `Bad Request` | (none) |
| `GET /v1/vessels/7` returning `fiber.NewError(200, "ok")` | 500 | `Internal Server Error` | `ok` in detail |

#### Scenario: Mapper takes precedence over a fiber error code
- **Given** a mapper `ErrLocked` to 423
- **When** a handler returns `fmt.Errorf("%w: %w", fiber.ErrBadRequest, ErrLocked)`
- **Then** the response status is `<status>`

##### Examples:
| status |
|--------|
| 423 |

### Requirement: Request information comes from fiber
The adapter SHALL describe the request to the core with the method, the path without query string, and the `Accept` header, and SHALL resolve within the request's context, so instance, negotiation, scoped mappers and enrichers behave as in net/http.

#### Scenario: Request facts
- **Given** a fiber app whose error handler is the adapter
- **When** a handler for `<request>` with `Accept: <accept>` returns an unmapped error hinted 404
- **Then** the instance is `<instance>` and the `Content-Type` is `<content_type>`

##### Examples:
| request | accept | instance | content_type |
|---------|--------|----------|--------------|
| `GET /v1/vessels/7?token=abc` | (none) | `/v1/vessels/7` | `application/problem+json` |
| `GET /v1/vessels/7` | `application/xml` | `/v1/vessels/7` | `application/problem+xml` |
| `GET /v1/vessels/7` | `image/png` | `/v1/vessels/7` | `application/problem+json` |

### Requirement: Buffered bodies are replaced, streamed bodies are not
Because fiber sends nothing before the handler returns, the adapter SHALL replace any buffered status, body and content type with the problem. When the handler started a streamed body, the adapter SHALL leave it untouched and only report the error to the hook.

#### Scenario: Handler wrote before returning an error
- **Given** a fiber app whose error handler is the adapter and a hook that records errors
- **When** a handler `<does>` and then returns `ErrVesselNotFound` (mapped to 404)
- **Then** the client receives `<received>`
- **And** the hook received `ErrVesselNotFound`

##### Examples:
| does | received |
|------|----------|
| calls `c.Status(201).JSON(fiber.Map{"id":7})` | status 404 with an `application/problem+json` problem |
| sets header `X-Request-Id: r1` | a 404 problem with header `X-Request-Id: r1` |
| sets a body stream writer that writes `data: 1` | the stream unchanged |

### Requirement: Client disconnects are not answered
When the request context is canceled and the error wraps `context.Canceled`, the adapter SHALL write nothing and SHALL still call the hook.

#### Scenario: Client went away
- **Given** a fiber app whose error handler is the adapter and a request whose context is `<ctx>`
- **When** the handler returns `<error>`
- **Then** the response is `<response>`
- **And** the hook was called 1 time

##### Examples:
| ctx | error | response |
|-----|-------|----------|
| canceled | `fmt.Errorf("query: %w", context.Canceled)` | not written by the library |
| active | `context.Canceled` | a 500 problem |

### Requirement: Faulty consumer code does not break fiber requests
Panics in consumer mappers, enrichers or the hook during error handling SHALL be contained by the core, so the fiber request still completes with a problem response.

#### Scenario: Faulty consumer code under fiber
- **Given** a fiber app using the adapter with `<faulty>` and a mapper `ErrX` to 409 registered after it
- **When** a handler returns `ErrX`
- **Then** the response status is `<status>` and the app keeps serving the next request

##### Examples:
| faulty | status |
|--------|--------|
| a mapper that panics | 409 |
| a mapper that returns nil | 409 |
| an enricher that panics | 409 |
| a hook that panics | 409 |

### Requirement: Scoped mappers for route groups
The fiber adapter SHALL provide a middleware that attaches mappers to the request context for the routes it guards. Nil mappers SHALL be ignored, and a scope with no mappers SHALL change nothing.

#### Scenario: Route group scope
- **Given** a group `/v1/contracts` using a scope mapping `ErrLocked` to 423, and a group `/v1/vessels` without it
- **When** `ErrLocked` is returned for `<path>`
- **Then** the response status is `<status>`

##### Examples:
| path | status |
|------|--------|
| `/v1/contracts/1` | 423 |
| `/v1/vessels/1` | 500 |

#### Scenario: Degenerate scopes
- **Given** a group using a scope created with `<mappers>`
- **When** `ErrLocked` is returned in that group
- **Then** the response status is 500

##### Examples:
| mappers |
|---------|
| no mappers |
| a single nil mapper |

### Requirement: Opt-in recovery returns panics to the error handler
The fiber adapter SHALL offer a recovery middleware that turns a handler panic into an error returned to the error handler. That error SHALL unwrap to the core panic error (value and panic-site stack) and its message SHALL NOT contain the panic value. Recovery SHALL NOT be active unless installed.

#### Scenario: Recovery with the adapter's error handler
- **Given** a fiber app with the adapter's error handler, `<chain>`, and a handler `loadVessel` that panics with `<value>`
- **When** a client sends a request with `Accept: <accept>`
- **Then** the client receives `<received>`
- **And** the hook receives `<hook>`

##### Examples:
| chain | value | accept | received | hook |
|-------|-------|--------|----------|------|
| the recovery middleware | `"boom"` | (none) | a 500 `application/problem+json` problem without `boom` | a panic error with value `"boom"` and a stack containing `loadVessel` |
| the recovery middleware | `io.ErrClosedPipe` | `application/xml` | a 500 `application/problem+xml` problem | an error matching `io.ErrClosedPipe` |
| the recovery middleware | `http.ErrAbortHandler` | (none) | a 500 `application/problem+json` problem | a panic error with value `http.ErrAbortHandler` |
| no recovery middleware | `"boom"` | (none) | fiber's own panic behavior (the library does not recover) | nothing |

#### Scenario: Recovery with fiber's default error handler
- **Given** a fiber app with the recovery middleware but fiber's default error handler
- **When** a handler panics with `<value>`
- **Then** the response status is 500 and the body does not contain `<value>`

##### Examples:
| value |
|-------|
| `"db password=hunter2"` |
| `errors.New("secret")` |

### Requirement: Misuse is rejected at construction
Creating the fiber error handler with a nil handler SHALL panic at construction time with a message naming the handler.

#### Scenario: Nil handler
- **When** the error handler is created with a nil handler
- **Then** it panics with a message naming the problem handler

##### Examples:
| handler |
|---------|
| `nil` |

### Requirement: Concurrent requests are isolated
The adapter SHALL keep per-request state only, so concurrent requests never see each other's status, headers or errors.

#### Scenario: Concurrent fiber requests
- **Given** a fiber app where `/a` returns `ErrVesselNotFound` (404) and `/b` returns `fiber.ErrUnauthorized`
- **When** `<requests>` requests alternate between `/a` and `/b` concurrently through a real listener
- **Then** every `/a` response is a 404 problem and every `/b` response is a 401 problem

##### Examples:
| requests |
|----------|
| 200 |

### Requirement: Parity with the net/http adapter
For the same configured handler and request, the fiber adapter SHALL produce the same status, `Content-Type`, `Vary` and body bytes as the net/http adapter.

#### Scenario: Same output as net/http
- **Given** one handler with an enricher adding `trace_id` = `t1` and a mapper `ErrVesselNotFound` to 404
- **When** the same request `GET /v1/vessels/7` with `Accept: <accept>` produces `<case>` through net/http and through fiber
- **Then** both responses have status `<status>`, `Content-Type: <content_type>`, `Vary: Accept` and identical body bytes

##### Examples:
| case | accept | status | content_type |
|------|--------|--------|--------------|
| a returned `ErrVesselNotFound` | (none) | 404 | `application/problem+json` |
| a returned unmapped error | (none) | 500 | `application/problem+json` |
| a panic under recovery | (none) | 500 | `application/problem+json` |
| a returned `ErrVesselNotFound` | `application/problem+xml` | 404 | `application/problem+xml` |
| `prblm.WithStatus(err, 403)` (net/http) and `fiber.NewError(403, "x")` (fiber) | (none) | 403 | `application/problem+json` |
