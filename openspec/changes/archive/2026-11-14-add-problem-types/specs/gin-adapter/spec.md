# Spec Delta

## ADDED Requirements

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

## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Misuse is rejected at construction
**Reason**: The gin middleware and recovery middleware no longer take a problem handler, so there is no nil handler to reject. They take core options and build the handler themselves; nil options are ignored, and invalid options panic at construction (see "Adapter configured with core options").
**Migration**: Replace `ginprblm.Middleware(h)` and `ginprblm.Recovery(h)`, where `h := prblm.NewHandler(opts...)`, with `ginprblm.Middleware(opts...)` and `ginprblm.Recovery(opts...)`.
