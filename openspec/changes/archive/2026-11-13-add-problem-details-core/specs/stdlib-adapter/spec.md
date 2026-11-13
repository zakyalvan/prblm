# Spec Delta

## Purpose

Defines how the library integrates with net/http: handlers that return errors, and a direct write function for handlers that keep the standard signature.

## ADDED Requirements

### Requirement: Error-returning handlers
The library SHALL turn a function `func(http.ResponseWriter, *http.Request) error` into an `http.Handler`. A non-nil returned error SHALL be written as a problem, and a nil error SHALL leave the response as the function wrote it.

#### Scenario: Handler result
- **Given** an adapted handler that `<does>`
- **When** a client sends `GET /v1/vessels/7`
- **Then** the client receives `<received>`

##### Examples:
| does | received |
|------|----------|
| returns `ErrVesselNotFound` (mapped to 404) | status 404 with an `application/problem+json` problem whose instance is `/v1/vessels/7` |
| writes 200 `{"id":7}` and returns nil | status 200 with body `{"id":7}` |
| writes nothing and returns nil | status 200 with an empty body |
| returns an unmapped error | status 500 problem without detail |
| writes 200 `{"id":7}` and then returns `ErrVesselNotFound` | status 200 with body `{"id":7}` |

### Requirement: Adapted handlers keep writer capabilities
Writes through an adapted handler SHALL keep the underlying writer's optional capabilities reachable through `http.ResponseController` (flush, hijack, deadlines), so streaming handlers keep working. Copying a body with `io.Copy` SHALL keep the underlying writer's `ReadFrom` (the sendfile path), and SHALL count as starting the response.

#### Scenario: Flush through the adapter
- **Given** an adapted handler that writes `data: 1` and flushes through `http.NewResponseController`
- **When** a client streams the response
- **Then** the flush `<result>`

##### Examples:
| result |
|--------|
| succeeds and the client receives `data: 1` before the handler returns |

#### Scenario: Copy through the adapter
- **Given** an adapted handler that `<does>`
- **When** a client sends `GET /v1/vessels/7`
- **Then** `<result>`

##### Examples:
| does | result |
|------|--------|
| copies a body with `io.Copy` | the underlying writer's `ReadFrom` copies it |
| copies `{"id":7}` with `io.Copy` and then returns `ErrVesselNotFound` | the client receives only `{"id":7}` |

### Requirement: Direct write
The library SHALL provide a function that writes a problem for a given response writer, request and error, for handlers that keep the standard signature. Without the adapter, it SHALL assume nothing has been written yet.

#### Scenario: Direct write from a standard handler
- **Given** a standard `http.HandlerFunc` that calls the write function with `<error>`
- **When** a client sends `GET /v1/vessels/7` with `Accept: <accept>`
- **Then** the client receives `<received>`

##### Examples:
| error | accept | received |
|-------|--------|----------|
| `ErrVesselNotFound` (mapped to 404) | (none) | a 404 `application/problem+json` problem |
| `ErrVesselNotFound` (mapped to 404) | `application/xml` | a 404 `application/problem+xml` problem |
| `nil` | (none) | nothing written by the library |

### Requirement: Misuse is rejected early
Creating an adapter from a nil function or with a nil handler SHALL panic at construction time with a clear message, not at request time.

#### Scenario: Nil arguments
- **Given** `<setup>`
- **When** the adapter is constructed
- **Then** construction panics with a message naming `<argument>`

##### Examples:
| setup | argument |
|-------|----------|
| a nil handler function | the handler function |
| a nil `*Handler` receiver | the problem handler |
