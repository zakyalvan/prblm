# gin-adapter Specification

## Purpose

Defines how the library integrates with gin so that errors recorded or aborted in gin handlers become the same RFC 9457 problem responses the core produces, without disturbing successful, streamed or hijacked responses.

## Requirements

### Requirement: Errors recorded in the context become problems
The gin middleware SHALL run after the downstream handlers and, when the context holds errors and no body has been written, SHALL write a problem resolved from the last recorded error.

#### Scenario: Recorded errors
- **Given** a gin engine using the middleware with a handler mapping `ErrVesselNotFound` to 404 and `ErrLocked` to 423
- **When** a handler for `GET /v1/vessels/7` records `<recorded>` and returns
- **Then** the response status is `<status>`, the `Content-Type` is `application/problem+json` and the instance is `/v1/vessels/7`
- **And** the hook received `<hook_error>`

##### Examples:
| recorded | status | hook_error |
|----------|--------|------------|
| `c.Error(ErrVesselNotFound)` | 404 | `ErrVesselNotFound` |
| `c.Error(errors.New("pq: password=hunter2"))` | 500 | `pq: password=hunter2` |
| `c.Error(ErrVesselNotFound)` then `c.Error(ErrLocked)` | 423 | `ErrLocked` |
| `c.Error(ErrLocked)` from a middleware, then `c.Error(ErrVesselNotFound)` from the handler | 404 | `ErrVesselNotFound` |

#### Scenario: Unmapped error text never leaks
- **Given** a gin engine using the middleware with zero configuration
- **When** a handler records `c.Error(errors.New("<text>"))`
- **Then** the response status is 500 and the body does not contain `<text>`

##### Examples:
| text |
|------|
| `dial tcp 10.0.0.5:5432: connection refused` |
| `user alice@example.com not permitted` |

### Requirement: Abort status is a status hint with full header control
When a handler aborts with a status and an error, the middleware SHALL use that status as the error's status hint. The abort SHALL NOT commit the response early, so the problem's status and `Content-Type` are always written, and a mapper still takes precedence over the hint.

#### Scenario: AbortWithError
- **Given** a gin engine using the middleware with `<mapping>`
- **When** a handler calls `c.AbortWithError(<abort_status>, <error>)`
- **Then** the response status is `<status>`, the `Content-Type` is `application/problem+json` and the detail is `<detail>`

##### Examples: hint used
| mapping | abort_status | error | status | detail |
|---------|--------------|-------|--------|--------|
| no mappers | 403 | `errors.New("nope")` | 403 | (empty) |
| no mappers | 400 | `errors.New("secret")` | 400 | (empty) |
| no mappers | 404 | `ErrVesselNotFound` | 404 | (empty) |

##### Examples: mapper wins
| mapping | abort_status | error | status | detail |
|---------|--------------|-------|--------|--------|
| `ErrLocked` to 423 with detail `Vessel is locked.` | 400 | `ErrLocked` | 423 | `Vessel is locked.` |
| a self-describing problem 409 | 400 | that problem | 409 | (empty) |

##### Examples: invalid hints
| mapping | abort_status | error | status | detail |
|---------|--------------|-------|--------|--------|
| no mappers | 200 | `errors.New("x")` | 500 | (empty) |
| no mappers | 302 | `errors.New("x")` | 500 | (empty) |

### Requirement: Status-only aborts become problems
When a handler aborts with a status of 400 or above without recording an error and without writing a body, the middleware SHALL write an `about:blank` problem for that status. Statuses below 400 SHALL be committed unchanged.

#### Scenario: AbortWithStatus and plain statuses
- **Given** a gin engine using the middleware
- **When** a handler calls `<call>`
- **Then** the client receives `<received>`

##### Examples: problems
| call | received |
|------|----------|
| `c.AbortWithStatus(401)` | status 401, `application/problem+json`, title `Unauthorized` |
| `c.AbortWithStatus(429)` | status 429, `application/problem+json`, title `Too Many Requests` |
| `c.Status(500)` and no body | status 500, `application/problem+json`, title `Internal Server Error` |

##### Examples: passed through
| call | received |
|------|----------|
| `c.Status(204)` | status 204, empty body, no `Content-Type` set by the library |
| `c.Status(304)` | status 304, empty body |
| `c.AbortWithStatus(200)` | status 200, empty body |
| `c.JSON(200, gin.H{"id":7})` | status 200, body `{"id":7}` |

### Requirement: Written bodies are never replaced
When a body has already been written, the middleware SHALL commit the response unchanged, even when errors are recorded, and SHALL still report the last error to the hook.

#### Scenario: Body written before or after an error
- **Given** a gin engine using the middleware and a hook that records errors
- **When** a handler `<does>`
- **Then** the client receives `<received>`
- **And** the hook received `<hook_error>`

##### Examples:
| does | received | hook_error |
|------|----------|------------|
| calls `c.AbortWithStatusJSON(400, gin.H{"code":"x"})` | status 400, body `{"code":"x"}` | (none) |
| calls `c.JSON(200, gin.H{"ok":true})` then `c.Error(ErrLocked)` | status 200, body `{"ok":true}` | `ErrLocked` |
| calls `c.Error(ErrLocked)` then `c.String(409, "locked")` | status 409, body `locked` | `ErrLocked` |

### Requirement: Streams and hijacked connections pass through
Flushing, hijacking or starting a body SHALL commit the response immediately, so streaming and websocket handlers behave as without the middleware. After a hijack, the middleware SHALL write nothing and SHALL only report recorded errors to the hook.

#### Scenario: Streaming and hijacking
- **Given** a gin engine using the middleware and a hook that records errors
- **When** a handler `<does>`
- **Then** `<outcome>`

##### Examples:
| does | outcome |
|------|---------|
| writes `data: 1\n\n` with `c.SSEvent` and flushes, then sleeps 50 ms | the client receives `data: 1` before the handler returns |
| calls `c.Writer.Flush()` with status 200 and then records `c.Error(ErrLocked)` | the client receives status 200 and the hook receives `ErrLocked` |
| hijacks the connection (websocket upgrade) and then records `c.Error(ErrLocked)` | nothing is written to the hijacked connection by the library and the hook receives `ErrLocked` |
| calls `c.Writer.WriteHeaderNow()` with status 200 and writes nothing | status 200 with an empty body |

### Requirement: Client disconnects are not answered
When the request context is canceled and the last error wraps `context.Canceled`, the middleware SHALL write nothing and SHALL still call the hook.

#### Scenario: Client went away
- **Given** a gin engine using the middleware and a request whose context is `<ctx>`
- **When** the handler records `<error>`
- **Then** the response is `<response>`
- **And** the hook was called 1 time

##### Examples:
| ctx | error | response |
|-----|-------|----------|
| canceled | `fmt.Errorf("query: %w", context.Canceled)` | not written by the library |
| active | `context.Canceled` | a 500 problem |

### Requirement: Faulty consumer code does not break gin requests
Panics in consumer mappers, enrichers or the hook during error handling SHALL be contained by the core, so the gin request still completes with a problem response.

#### Scenario: Faulty consumer code under gin
- **Given** a gin engine using the middleware with `<faulty>` and a mapper `ErrX` to 409 registered after it
- **When** a handler records `c.Error(ErrX)`
- **Then** the response status is `<status>` and the server keeps serving the next request

##### Examples:
| faulty | status |
|--------|--------|
| a mapper that panics | 409 |
| a mapper that returns nil | 409 |
| an enricher that panics | 409 |
| a hook that panics | 409 |

### Requirement: Scoped mappers for route groups
The gin adapter SHALL provide a middleware that attaches mappers to the request context for the routes it guards. Nil mappers SHALL be ignored, and a scope with no mappers SHALL change nothing.

#### Scenario: Route group scope
- **Given** a group `/v1/contracts` using a scope mapping `ErrLocked` to 423, and a group `/v1/vessels` without it
- **When** `ErrLocked` is recorded for `<path>`
- **Then** the response status is `<status>`

##### Examples:
| path | status |
|------|--------|
| `/v1/contracts/1` | 423 |
| `/v1/vessels/1` | 500 |

#### Scenario: Degenerate scopes
- **Given** a group using a scope created with `<mappers>`
- **When** `ErrLocked` is recorded in that group
- **Then** the response status is 500

##### Examples:
| mappers |
|---------|
| no mappers |
| a single nil mapper |

### Requirement: Opt-in recovery independent of order
The gin adapter SHALL offer a recovery middleware that turns a handler panic into the core's safe 500 problem. It SHALL work whether it is registered before or after the error middleware, or without it, and SHALL NOT be active unless installed.

#### Scenario: Recovery placement
- **Given** a gin engine with `<chain>` and a handler that panics with `"boom"`
- **When** a client sends a request with `Accept: <accept>`
- **Then** the client receives `<received>`

##### Examples:
| chain | accept | received |
|-------|--------|----------|
| recovery, then middleware | (none) | a 500 `application/problem+json` problem without `boom` |
| middleware, then recovery | (none) | a 500 `application/problem+json` problem without `boom` |
| recovery only | (none) | a 500 `application/problem+json` problem without `boom` |
| recovery, then middleware | `application/xml` | a 500 `application/problem+xml` problem |
| middleware only | (none) | the panic propagates to gin's own handling |

#### Scenario: Panic details go to the hook
- **Given** a gin engine with recovery and a handler function `loadVessel` that panics with `io.ErrClosedPipe`
- **When** a client sends a request
- **Then** the hook receives an error that unwraps to a panic error with value `io.ErrClosedPipe` and a stack containing `loadVessel`

##### Examples:
| value |
|-------|
| `io.ErrClosedPipe` |

#### Scenario: Recovery edge cases
- **Given** a gin engine with recovery and a handler that `<does>`
- **When** a client sends a request
- **Then** `<outcome>`

##### Examples:
| does | outcome |
|------|---------|
| panics with `http.ErrAbortHandler` | the recovery re-panics with `http.ErrAbortHandler` and writes nothing |
| writes `{"partial":` and flushes, then panics | the client receives only `{"partial":` and the hook receives the panic |
| panics with an error wrapping `syscall.EPIPE` | nothing is written and the hook receives the panic |
| panics while an enricher also panics | the client receives a 500 problem and the server keeps serving |

### Requirement: Concurrent requests are isolated
The middleware SHALL keep per-request state only, so concurrent requests never see each other's status, headers or errors.

#### Scenario: Concurrent gin requests
- **Given** a gin engine using the middleware where `/a` records `ErrVesselNotFound` (404) and `/b` aborts with 401
- **When** `<requests>` requests alternate between `/a` and `/b` concurrently
- **Then** every `/a` response is a 404 problem and every `/b` response is a 401 problem

##### Examples:
| requests |
|----------|
| 200 |

### Requirement: Parity with the net/http adapter
For the same options, the gin adapter SHALL produce the same status, `Content-Type`, `Vary` and body bytes as the net/http adapter, including for resolved problem types and type documentation.

#### Scenario: Same output as net/http
- **Given** one options slice with an enricher adding `trace_id` = `t1`, a mapper `ErrVesselNotFound` to 404, a mapper `ErrBlacklisted` to the type `contracts/blacklisted-supplier` (409), namespace `/v1/api/probs` and that type registered
- **When** the same request `GET <path>` with `Accept: <accept>` produces `<case>` through net/http (with discovery mounted) and through gin
- **Then** both responses have status `<status>`, `Content-Type: <content_type>`, `Vary: Accept` and identical body bytes

##### Examples:
| case | path | accept | status | content_type |
|------|------|--------|--------|--------------|
| a returned or recorded `ErrVesselNotFound` | `/v1/vessels/7` | (none) | 404 | `application/problem+json` |
| a returned or recorded unmapped error | `/v1/vessels/7` | (none) | 500 | `application/problem+json` |
| a panic under recovery | `/v1/vessels/7` | (none) | 500 | `application/problem+json` |
| a returned or recorded `ErrVesselNotFound` | `/v1/vessels/7` | `application/problem+xml` | 404 | `application/problem+xml` |
| `prblm.WithStatus(err, 403)` (net/http) and `c.AbortWithError(403, err)` (gin) | `/v1/vessels/7` | (none) | 403 | `application/problem+json` |
| a returned or recorded `ErrBlacklisted` | `/v1/contracts/7` | (none) | 409 | `application/problem+json` |
| the type's documentation | `/v1/api/probs/contracts/blacklisted-supplier` | `application/json` | 200 | `application/json` |
| the type's documentation | `/v1/api/probs/contracts/blacklisted-supplier` | `text/html` | 200 | `text/html; charset=utf-8` |
| an unknown type under the namespace | `/v1/api/probs/contracts/unknown` | (none) | 404 | `application/problem+json` |

### Requirement: Adapter configured with core options
The gin middleware and the recovery middleware SHALL each take the same options as the core handler constructor, and SHALL build their problem handler from them at construction. With no options they SHALL give the core's zero-configuration behavior, and nil options SHALL be ignored.

#### Scenario: Options configure the middleware
- **Given** a gin engine using the middleware created with `<options>`
- **When** a handler for `GET /v1/vessels/7` records `c.Error(ErrVesselNotFound)`
- **Then** the response status is `<status>` and the `Content-Type` is `application/problem+json`

##### Examples:
| options | status |
|---------|--------|
| (none) | 500 |
| `WithMappers(Status(ErrVesselNotFound, 404))` | 404 |
| `nil, WithMappers(Status(ErrVesselNotFound, 404)), nil` | 404 |
| `nil` | 500 |

#### Scenario: Shared options across middleware and recovery
- **Given** one options slice with a mapper `ErrVesselNotFound` to 404 and a hook that records calls, passed to both the recovery middleware and the error middleware
- **When** `<request>`
- **Then** the response status is `<status>` and the hook was called `<calls>` time

##### Examples:
| request | status | calls |
|---------|--------|-------|
| a handler records `c.Error(ErrVesselNotFound)` | 404 | 1 |
| a handler panics with `"boom"` | 500 | 1 |

#### Scenario: Invalid options fail at construction
- **When** `<constructor>` is created with `<options>`
- **Then** it panics with a message naming `<named>`

##### Examples:
| constructor | options | named |
|-------------|---------|-------|
| the error middleware | `WithNamespace("v1/api/probs")` | `WithNamespace` |
| the recovery middleware | `WithNamespace("v1/api/probs")` | `WithNamespace` |
| the error middleware | `WithTypes(OutOfCredit)` without a namespace | `WithTypes` |

### Requirement: Middleware serves type discovery for unclaimed paths
When its options turn discovery on, the gin middleware installed on the engine SHALL answer GET and HEAD requests under the namespace path that no route claims with the type documentation, exactly as the core discovery handler would. A route registered at such a path SHALL take precedence, including a route for another method when `engine.HandleMethodNotAllowed` is on, which keeps gin's 405.

#### Scenario: Discovery through gin
- **Given** a gin engine with `<setup>`, where the options set namespace `/v1/api/probs` and register the type `out-of-credit` (403, `Out of credit`)
- **When** a client sends `<request>` with `Accept: application/json`
- **Then** the client receives `<received>`

##### Examples: served
| setup | request | received |
|-------|---------|----------|
| the middleware via `engine.Use` | `GET /v1/api/probs/out-of-credit` | status 200, `application/json`, `type` `/v1/api/probs/out-of-credit` |
| the middleware via `engine.Use` | `HEAD /v1/api/probs/out-of-credit` | status 200, `application/json`, empty body |
| the middleware via `engine.Use` | `GET /v1/api/probs` | status 200, the index with `out-of-credit` |
| the middleware via `engine.Use` | `GET /v1/api/probs/unknown` | a 404 `application/problem+json` problem |
| the middleware via `engine.Use` | `GET /v1/api/probs/out-of-credit` with `If-None-Match` set to the `ETag` of a previous response | status 304, the same `ETag`, `Cache-Control: public, max-age=3600`, empty body |
| the middleware via `engine.Use` and `engine.NoRoute` returning 404 `custom` | `GET /v1/api/probs/out-of-credit` | status 200, `application/json`, `type` `/v1/api/probs/out-of-credit` |
| the middleware via `engine.Use` and `engine.HandleMethodNotAllowed = true` | `GET /v1/api/probs/out-of-credit` | status 200, `application/json`, `type` `/v1/api/probs/out-of-credit` |
| the middleware via `engine.Use` and a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `HEAD /v1/api/probs/out-of-credit` | status 200, `application/json`, empty body |

##### Examples: not served
| setup | request | received |
|-------|---------|----------|
| the middleware via `engine.Use` and a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `GET /v1/api/probs/out-of-credit` | status 200, body `custom` |
| the middleware via `engine.Use` | `POST /v1/api/probs/out-of-credit` | status 404, not the documentation |
| the middleware via `engine.Use` | `GET /v1/other/out-of-credit` | status 404, not the documentation |
| the middleware via `engine.Use` and `WithoutDiscovery()` | `GET /v1/api/probs/out-of-credit` | status 404, not the documentation |
| the middleware only on the group `/v1/vessels` | `GET /v1/api/probs/out-of-credit` | status 404, not the documentation |
| the middleware via `engine.Use` and `engine.NoRoute` returning 404 `custom` | `GET /v1/other/out-of-credit` | status 404, body `custom` |
| the middleware via `engine.Use`, `engine.HandleMethodNotAllowed = true` and a route `POST /v1/api/probs/out-of-credit` | `GET /v1/api/probs/out-of-credit` | status 405, not the documentation |
| the middleware via `engine.Use`, `engine.HandleMethodNotAllowed = true` and a route `POST /v1/api/probs` | `GET /v1/api/probs` | status 405, not the index |

### Requirement: Middleware reports shadowed documentation
When its options turn discovery on, the gin middleware SHALL report every request a route answers to the handler's shadowed-documentation check, with the route's pattern (`c.FullPath()`), so the hook receives the note of type-discovery "Shadowed documentation is noted". The response SHALL be unchanged.

#### Scenario: Shadowed documentation through gin
- **Given** a gin engine using the middleware via `engine.Use` with namespace `/v1/api/probs`, the type `out-of-credit` registered, a hook that records calls and `<setup>`
- **When** a client sends `<requests>`
- **Then** each response is the route's own, and the hook receives `<notes>` notes wrapping `ErrShadowedDocumentation`

##### Examples:
| setup | requests | notes |
|-------|----------|-------|
| a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `GET /v1/api/probs/out-of-credit` twice | 1, naming `/v1/api/probs/out-of-credit` |
| a route `GET /v1/api/probs/:name` returning 200 `custom` | `GET /v1/api/probs/out-of-credit` | 1, naming `/v1/api/probs/:name` |
| a route `GET /v1/vessels/:id` returning 200 `custom` | `GET /v1/vessels/7` | 0 |
| a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` and `WithoutDiscovery()` | `GET /v1/api/probs/out-of-credit` | 0 |
