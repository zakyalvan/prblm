# Spec Delta

## Purpose

Defines opt-in net/http middleware that turns handler panics into safe RFC 9457 500 responses, while giving the panic value and stack only to the consumer's hook.

## ADDED Requirements

### Requirement: Recovery is opt-in
Panics SHALL be recovered only when the recovery middleware is installed. The error-handling adapter alone SHALL NOT recover panics.

#### Scenario: Middleware installed or not
- **Given** a handler that panics, `<installed>`
- **When** a client sends a request
- **Then** `<outcome>`

##### Examples:
| installed | outcome |
|-----------|---------|
| wrapped by the recovery middleware | the client receives a 500 problem |
| wrapped only by the error-returning adapter | the panic propagates to net/http's own handling |

### Requirement: Panic becomes a safe 500 problem
A recovered panic SHALL produce a 500 problem with type `about:blank`, title `Internal Server Error` and no detail. The panic value and stack SHALL never appear in the response.

#### Scenario: Panic values
- **Given** a handler wrapped by the recovery middleware that panics with `<value>`
- **When** a client sends a request
- **Then** the status is 500 and the body contains no part of `<text>` and no stack frame

##### Examples:
| value | text |
|-------|------|
| `"index out of range"` | `index out of range` |
| `errors.New("db password=hunter2")` | `hunter2` |
| `42` | `42` |
| `nil` (via `panic(nil)`, a `*runtime.PanicNilError`) | `nil` |

### Requirement: Panic details go to the hook
The hook SHALL receive the panic as an error carrying the panic value and the stack captured at the panic site. When the value is an error, it SHALL be reachable through standard unwrapping.

#### Scenario: Hook receives the panic
- **Given** a handler function `loadVessel` wrapped by the recovery middleware that panics with `<value>`
- **When** a client sends a request
- **Then** the hook receives an error from which value `<value>` can be extracted
- **And** its stack contains `loadVessel`
- **And** unwrapping to `<target>` gives `<matches>`

##### Examples:
| value | target | matches |
|-------|--------|---------|
| `io.ErrClosedPipe` | `io.ErrClosedPipe` | true |
| `"boom"` | `io.ErrClosedPipe` | false |

### Requirement: Same rendering as errors
Recovered panics SHALL go through the same enrichers, negotiation and headers as other errors.

#### Scenario: Negotiated 500
- **Given** an enricher that adds `trace_id` and a request with `Accept: <accept>`
- **When** the handler panics under the recovery middleware
- **Then** the 500 problem has `Content-Type: <content_type>` and contains `trace_id`

##### Examples:
| accept | content_type |
|--------|--------------|
| (none) | `application/problem+json` |
| `application/problem+xml` | `application/problem+xml` |

### Requirement: net/http abort signal is preserved
A panic with `http.ErrAbortHandler` SHALL be re-raised unchanged so net/http aborts the response as designed, and SHALL NOT be reported as an error.

#### Scenario: Abort handler
- **Given** a handler wrapped by the recovery middleware that panics with `<value>`
- **When** a client sends a request
- **Then** the middleware `<behavior>`

##### Examples:
| value | behavior |
|-------|----------|
| `http.ErrAbortHandler` | re-panics with `http.ErrAbortHandler`, writes nothing and does not call the hook |
| `fmt.Errorf("x: %w", http.ErrAbortHandler)` | writes a 500 problem (only the exact sentinel aborts) |

### Requirement: No write after a started response or a lost connection
When the response has already started, or the panic shows the client connection is broken, recovery SHALL NOT write a problem and SHALL still report the panic to the hook.

#### Scenario: Panic after output or on broken connection
- **Given** a handler wrapped by the recovery middleware that `<does>`
- **When** a client sends a request
- **Then** the client receives `<received>`
- **And** the hook receives the panic

##### Examples:
| does | received |
|------|----------|
| writes `{"partial":` and then panics | only `{"partial":` |
| panics with an error wrapping `syscall.EPIPE` | nothing more |
| panics with an error wrapping `syscall.ECONNRESET` | nothing more |
| panics before writing anything | a 500 problem |

### Requirement: Recovery never panics
If producing the problem panics during recovery (for example, an enricher or the hook panics), the middleware SHALL write the constant minimal JSON 500 problem when nothing has been written yet, and SHALL NOT let a second panic escape.

#### Scenario: Faulty consumer code during recovery
- **Given** a handler that panics and `<faulty>`
- **When** a client sends a request under the recovery middleware
- **Then** the client receives a 500 `application/problem+json` response and the server keeps serving

##### Examples:
| faulty |
|--------|
| an enricher that panics |
| a hook that panics |
| an encoder that panics |
