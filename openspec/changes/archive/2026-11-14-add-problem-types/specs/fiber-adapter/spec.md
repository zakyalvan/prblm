# Spec Delta

## ADDED Requirements

### Requirement: Error handler configured with core options
The fiber error handler SHALL take the same options as the core handler constructor and SHALL build its problem handler from them at construction. With no options it SHALL give the core's zero-configuration behavior, and nil options SHALL be ignored. The recovery middleware SHALL stay without arguments.

#### Scenario: Options configure the error handler
- **Given** a fiber app whose error handler is the adapter created with `<options>`
- **When** the handler for `GET /v1/vessels/7` returns `ErrVesselNotFound`
- **Then** the response status is `<status>` and the `Content-Type` is `application/problem+json`

##### Examples:
| options | status |
|---------|--------|
| (none) | 500 |
| `WithMappers(Status(ErrVesselNotFound, 404))` | 404 |
| `nil, WithMappers(Status(ErrVesselNotFound, 404)), nil` | 404 |
| `nil` | 500 |

#### Scenario: Invalid options fail at construction
- **When** the error handler is created with `<options>`
- **Then** it panics with a message naming `<named>`

##### Examples:
| options | named |
|---------|-------|
| `WithNamespace("v1/api/probs")` | `WithNamespace` |
| `WithTypes(OutOfCredit)` without a namespace | `WithTypes` |
| `WithNamespace("v1/api/probs")`, passed to the discovery middleware | `WithNamespace` |

### Requirement: Error handler serves type discovery on route-not-found
When its options turn discovery on, the fiber error handler SHALL answer GET and HEAD requests under the namespace path that reach it as fiber's route-not-found error with the type documentation, exactly as the core discovery handler would. A route registered at such a path SHALL take precedence.

#### Scenario: Discovery through fiber
- **Given** a fiber app whose error handler is the adapter with `<setup>`, where the options set namespace `/v1/api/probs` and register the type `out-of-credit` (403, `Out of credit`)
- **When** a client sends `<request>` with `Accept: application/json`
- **Then** the client receives `<received>`

##### Examples: served
| setup | request | received |
|-------|---------|----------|
| no extra routes | `GET /v1/api/probs/out-of-credit` | status 200, `application/json`, `type` `/v1/api/probs/out-of-credit` |
| no extra routes | `HEAD /v1/api/probs/out-of-credit` | status 200, `application/json`, empty body |
| no extra routes | `GET /v1/api/probs` | status 200, the index with `out-of-credit` |
| no extra routes | `GET /v1/api/probs/unknown` | a 404 `application/problem+json` problem |
| no extra routes | `GET /v1/api/probs/%zz` | a 404 `application/problem+json` problem |
| no extra routes | `GET /v1/api/probs/out-of-credit` with `If-None-Match` set to the `ETag` of a previous response | status 304, the same `ETag`, `Cache-Control: public, max-age=3600`, empty body |

##### Examples: not served
| setup | request | received |
|-------|---------|----------|
| a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `GET /v1/api/probs/out-of-credit` | status 200, body `custom` |
| a route `GET /v1/api/probs/out-of-credit` returning `fiber.ErrNotFound` | `GET /v1/api/probs/out-of-credit` | a 404 `application/problem+json` problem, not the documentation |
| a route `POST /v1/api/probs/out-of-credit` | `GET /v1/api/probs/out-of-credit` | a 405 `application/problem+json` problem, not the documentation |
| a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `HEAD /v1/api/probs/out-of-credit` | status 200, the route's own headers, not the documentation (fiber registers HEAD with GET) |
| no extra routes | `POST /v1/api/probs/out-of-credit` | a 404 `application/problem+json` problem, not the documentation |
| no extra routes | `GET /v1/other/out-of-credit` | a 404 `application/problem+json` problem |
| `WithoutDiscovery()` | `GET /v1/api/probs/out-of-credit` | a 404 `application/problem+json` problem |

### Requirement: Discovery middleware reports shadowed documentation
The fiber adapter SHALL offer an opt-in discovery middleware, installed with `app.Use` and taking the error handler's options. With discovery on, it SHALL report every request an endpoint route answers to the handler's shadowed-documentation check, with the route's path pattern. It SHALL change no response and SHALL return the error of the handlers after it unchanged.

#### Scenario: Shadowed documentation through fiber
- **Given** a fiber app whose error handler is the adapter, with namespace `/v1/api/probs`, the type `out-of-credit` registered, a hook that records calls and `<setup>`
- **When** a client sends `<requests>`
- **Then** the client receives `<received>`, and the hook receives `<notes>` notes wrapping `ErrShadowedDocumentation`

##### Examples:
| setup | requests | received | notes |
|-------|----------|----------|-------|
| the discovery middleware via `app.Use` and a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `GET /v1/api/probs/out-of-credit` twice | status 200, body `custom` | 1, naming `/v1/api/probs/out-of-credit` |
| the discovery middleware via `app.Use` and a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `HEAD /v1/api/probs/out-of-credit` | status 200, empty body | 1 |
| the discovery middleware via `app.Use` and a route `GET /v1/vessels/:id` returning 200 `custom` | `GET /v1/vessels/7` | status 200, body `custom` | 0 |
| a route `GET /v1/api/probs/out-of-credit` returning 200 `custom`, no discovery middleware | `GET /v1/api/probs/out-of-credit` | status 200, body `custom` | 0 |
| the discovery middleware via `app.Use` with `WithoutDiscovery()`, and a route `GET /v1/api/probs/out-of-credit` returning 200 `custom` | `GET /v1/api/probs/out-of-credit` | status 200, body `custom` | 0 |
| the discovery middleware via `app.Use`, no routes | `GET /v1/api/probs/out-of-credit` | status 200, `application/json`, `type` `/v1/api/probs/out-of-credit` | 0 |

## MODIFIED Requirements

### Requirement: Parity with the net/http adapter
For the same options, the fiber adapter SHALL produce the same status, `Content-Type`, `Vary` and body bytes as the net/http adapter, including for resolved problem types and type documentation.

#### Scenario: Same output as net/http
- **Given** one options slice with an enricher adding `trace_id` = `t1`, a mapper `ErrVesselNotFound` to 404, a mapper `ErrBlacklisted` to the type `contracts/blacklisted-supplier` (409), namespace `/v1/api/probs` and that type registered
- **When** the same request `GET <path>` with `Accept: <accept>` produces `<case>` through net/http (with discovery mounted) and through fiber
- **Then** both responses have status `<status>`, `Content-Type: <content_type>`, `Vary: Accept` and identical body bytes

##### Examples:
| case | path | accept | status | content_type |
|------|------|--------|--------|--------------|
| a returned `ErrVesselNotFound` | `/v1/vessels/7` | (none) | 404 | `application/problem+json` |
| a returned unmapped error | `/v1/vessels/7` | (none) | 500 | `application/problem+json` |
| a panic under recovery | `/v1/vessels/7` | (none) | 500 | `application/problem+json` |
| a returned `ErrVesselNotFound` | `/v1/vessels/7` | `application/problem+xml` | 404 | `application/problem+xml` |
| `prblm.WithStatus(err, 403)` (net/http) and `fiber.NewError(403, "x")` (fiber) | `/v1/vessels/7` | (none) | 403 | `application/problem+json` |
| a returned `ErrBlacklisted` | `/v1/contracts/7` | (none) | 409 | `application/problem+json` |
| the type's documentation | `/v1/api/probs/contracts/blacklisted-supplier` | `application/json` | 200 | `application/json` |
| the type's documentation | `/v1/api/probs/contracts/blacklisted-supplier` | `text/html` | 200 | `text/html; charset=utf-8` |
| an unknown type under the namespace | `/v1/api/probs/contracts/unknown` | (none) | 404 | `application/problem+json` |

## REMOVED Requirements

### Requirement: Misuse is rejected at construction
**Reason**: The fiber error handler no longer takes a problem handler, so there is no nil handler to reject. It takes core options and builds the handler itself; nil options are ignored, and invalid options panic at construction (see "Error handler configured with core options").
**Migration**: Replace `fiberprblm.ErrorHandler(h)`, where `h := prblm.NewHandler(opts...)`, with `fiberprblm.ErrorHandler(opts...)`.
