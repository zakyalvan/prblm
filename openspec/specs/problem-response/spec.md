# problem-response Specification

## Purpose

Defines how a resolved problem becomes an HTTP response in a framework-neutral way: status consistency, format negotiation, headers, and what happens when the response cannot be written as planned.

## Requirements

### Requirement: Response status equals problem status
The HTTP response status SHALL always equal the problem's `status` member in the body.

#### Scenario: Status consistency
- **Given** a mapper that returns a problem with status `<returned>`
- **When** the problem is written
- **Then** the HTTP status is `<status>` and the body `status` is `<status>`

##### Examples:
| returned | status |
|----------|--------|
| 409 | 409 |
| 404 | 404 |
| (none) | 500 |

### Requirement: Accept negotiation between JSON and XML
The library SHALL choose the format from the request `Accept` header, honoring q-values and specificity. JSON types and wildcards select `application/problem+json`. `application/problem+xml`, `application/xml` and `text/xml` select `application/problem+xml`. Equal preference SHALL select JSON.

#### Scenario: Negotiated format
- **Given** a request with `Accept: <accept>`
- **When** a 404 problem is written
- **Then** the `Content-Type` is `<content_type>`

##### Examples: JSON
| accept | content_type |
|--------|--------------|
| (none) | `application/problem+json` |
| `*/*` | `application/problem+json` |
| `application/json` | `application/problem+json` |
| `application/problem+json` | `application/problem+json` |
| `application/*` | `application/problem+json` |
| `application/xml, application/json` | `application/problem+json` |
| `application/xml;q=0.5, */*` | `application/problem+json` |

##### Examples: XML
| accept | content_type |
|--------|--------------|
| `application/problem+xml` | `application/problem+xml` |
| `application/xml` | `application/problem+xml` |
| `text/xml` | `application/problem+xml` |
| `application/json;q=0.5, application/xml` | `application/problem+xml` |
| `application/json;q=0, application/xml` | `application/problem+xml` |
| `APPLICATION/XML` | `application/problem+xml` |

### Requirement: Unacceptable or malformed Accept falls back to JSON
When no supported format is acceptable, or the `Accept` header is malformed or too large, the library SHALL respond with `application/problem+json` and the problem's status, never 406.

#### Scenario: Fallback to JSON
- **Given** a request with `Accept: <accept>`
- **When** a 404 problem is written
- **Then** the status is 404 and the `Content-Type` is `application/problem+json`

##### Examples:
| accept |
|--------|
| `image/png` |
| `application/json;q=0, application/xml;q=0` |
| `;;;,,,` |
| `application/xml;q=abc` |
| `application/xml;q=2` |
| an 8 KiB header of repeated `text/html,` |

### Requirement: Response headers
Every problem response SHALL set `Content-Type` to the negotiated media type and add `Accept` to `Vary` (keeping existing values). It SHALL set `Cache-Control: no-store` unless a `Cache-Control` header is already present or the problem carries one.

#### Scenario: Headers on the response
- **Given** existing response headers `<existing>` and a problem with headers `<problem_headers>`
- **When** the problem is written
- **Then** the response headers include `<expected>`

##### Examples:
| existing | problem_headers | expected |
|----------|-----------------|----------|
| (none) | (none) | `Vary: Accept`, `Cache-Control: no-store` |
| `Vary: Origin` | (none) | `Vary: Origin`, `Vary: Accept` |
| `Vary: Accept` | (none) | exactly one `Accept` in `Vary` |
| `Cache-Control: private` | (none) | `Cache-Control: private` |
| (none) | `Cache-Control: max-age=60` | `Cache-Control: max-age=60` |
| (none) | `Retry-After: 5` | `Retry-After: 5`, `Cache-Control: no-store` |

### Requirement: Problem headers cannot override content headers
A problem's own headers SHALL NOT replace `Content-Type` or `Content-Length`, which the library controls.

#### Scenario: Protected headers
- **Given** a problem with header `<header>`
- **When** the problem is written as JSON
- **Then** the response `<name>` is `<value>`

##### Examples:
| header | name | value |
|--------|------|-------|
| `Content-Type: text/html` | `Content-Type` | `application/problem+json` |
| `Content-Length: 1` | `Content-Length` | the real body length |

### Requirement: Framework-neutral response
The library SHALL turn a request description (method, path, `Accept`) and an error into a complete response (status, headers, body) without writing it, so any framework adapter can deliver it. It SHALL report "nothing to write" for a nil error only.

#### Scenario: Respond without writing
- **Given** a request `<request>` with `Accept: <accept>`
- **When** a response is produced for `<error>`
- **Then** the result is `<result>`

##### Examples:
| request | accept | error | result |
|---------|--------|-------|--------|
| `GET /v1/vessels/7` | (none) | `ErrVesselNotFound` (mapped to 404) | status 404, `application/problem+json`, body with instance `/v1/vessels/7` |
| `GET /v1/vessels/7` | `application/xml` | `ErrVesselNotFound` (mapped to 404) | status 404, `application/problem+xml` |
| `GET /v1/vessels/7` | (none) | `nil` | nothing to write |
| `GET /v1/vessels/7` (context canceled) | (none) | `context.Canceled` | status 500, `application/problem+json` |

### Requirement: Encoding failures fall back to a minimal problem
When encoding the problem fails, the library SHALL respond with a constant, pre-encoded `application/problem+json` 500 problem and report the encoding error to the `OnError` hook, wrapped with the exported `ErrEncoding`. No partial body SHALL be sent.

#### Scenario: Encoding fails
- **Given** a request with `Accept: <accept>` and a mapper that adds extension `<extension>`
- **When** the problem is written
- **Then** the status is 500, the `Content-Type` is `application/problem+json` and the body is `{"title":"Internal Server Error","status":500}`
- **And** the hook received an error wrapping `ErrEncoding`

##### Examples:
| accept | extension |
|--------|-----------|
| `application/json` | `bad` holding a `chan int` |
| `application/xml` | `meta` holding a map with key `file.name` |
| `application/json` | `ratio` holding `math.Inf(1)` |

### Requirement: Responses already started are left alone
When the response has already started (status or body sent) before the problem is written, the library SHALL NOT write anything more and SHALL still report the error to the `OnError` hook.

#### Scenario: Handler wrote before failing
- **Given** a handler that `<wrote>` and then returns `ErrVesselNotFound`
- **When** the error is handled
- **Then** the client receives `<received>`
- **And** the hook received `ErrVesselNotFound`

##### Examples:
| wrote | received |
|-------|----------|
| wrote status 200 and body `{"partial":true}` | status 200 and body `{"partial":true}` only |
| wrote status 202 and no body | status 202 and an empty body |
| flushed headers with status 200 | status 200 and an empty body |
| set a header but wrote nothing | a 404 problem |
