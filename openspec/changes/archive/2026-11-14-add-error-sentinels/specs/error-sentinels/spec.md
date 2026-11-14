# Spec Delta

## Purpose

Provides exported sentinel errors for common request outcomes, so an application can return or wrap one and get a correct, leak-free problem response with no configuration.

## ADDED Requirements

### Requirement: Each sentinel maps to a fixed problem
With zero configuration, an error chain containing one of the library's sentinel errors SHALL resolve to a problem with type `about:blank`, the sentinel's status, the status phrase as title and the sentinel's fixed detail.

#### Scenario: Sentinel returned directly
- **Given** a handler with zero configuration
- **When** a handler for `GET /v1/vessels/7` returns `<sentinel>`
- **Then** the response status is `<status>` with `Content-Type: application/problem+json`
- **And** the body has type `about:blank`, title `<title>` and detail `<detail>`

##### Examples: client errors
| sentinel | status | title | detail |
|----------|--------|-------|--------|
| `prblm.ErrInvalidInput` | 400 | `Bad Request` | `The request is invalid.` |
| `prblm.ErrUnauthenticated` | 401 | `Unauthorized` | `Authentication is required.` |
| `prblm.ErrPermissionDenied` | 403 | `Forbidden` | `You are not allowed to perform this action.` |
| `prblm.ErrNotFound` | 404 | `Not Found` | `The requested resource was not found.` |
| `prblm.ErrConflict` | 409 | `Conflict` | `The request conflicts with the current state of the resource.` |
| `prblm.ErrPreconditionFailed` | 412 | `Precondition Failed` | `A precondition of the request was not met.` |
| `prblm.ErrRateLimited` | 429 | `Too Many Requests` | `Too many requests.` |

##### Examples: dependency errors
| sentinel | status | title | detail |
|----------|--------|-------|--------|
| `prblm.ErrBadGateway` | 502 | `Bad Gateway` | `An upstream service returned an invalid response.` |
| `prblm.ErrUnavailable` | 503 | `Service Unavailable` | `The service is temporarily unavailable.` |
| `prblm.ErrUpstreamTimeout` | 504 | `Gateway Timeout` | `An upstream service did not respond in time.` |

### Requirement: Sentinels are found anywhere in the error chain
A sentinel SHALL be matched as `errors.Is` reports it: returned directly, wrapped with `%w`, joined, or reported by a custom `Is` method. An error that only looks like a sentinel SHALL NOT match.

#### Scenario: Wrapped, joined and custom errors
- **Given** a handler with zero configuration
- **When** a handler returns `<error>`
- **Then** the response status is `<status>`

##### Examples: matched
| error | status |
|-------|--------|
| `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` | 404 |
| `fmt.Errorf("load: %w", fmt.Errorf("vessel 7: %w", prblm.ErrNotFound))` | 404 |
| `errors.Join(io.EOF, prblm.ErrNotFound)` | 404 |
| a `*VesselMissingError` whose `Is` method reports `prblm.ErrNotFound` | 404 |
| `fmt.Errorf("op: %w", &VesselMissingError{ID: 7})` | 404 |

##### Examples: not matched
| error | status |
|-------|--------|
| `errors.New("prblm: not found")` | 500 |
| `errors.New("not found")` | 500 |
| `fmt.Errorf("vessel 7: %v", prblm.ErrNotFound)` | 500 |

### Requirement: Wrap text never reaches the client
The response for a sentinel SHALL contain only the sentinel's fixed detail. The wrap text, the sentinel's error string, wrapped causes and the fields of a custom error type SHALL NOT appear in the response in any format.

#### Scenario: Context added by the application stays private
- **Given** a handler with zero configuration
- **When** a handler returns `<error>` to a client sending `Accept: <accept>`
- **Then** the response status is `<status>` and the body contains none of `<forbidden>`

##### Examples:
| error | accept | status | forbidden |
|-------|--------|--------|-----------|
| `fmt.Errorf("vessel 7 of alice@example.com: %w", prblm.ErrNotFound)` | (none) | 404 | `alice@example.com`, `vessel 7`, `prblm:` |
| `fmt.Errorf("vessel 7 of alice@example.com: %w", prblm.ErrNotFound)` | `application/problem+xml` | 404 | `alice@example.com`, `vessel 7`, `prblm:` |
| `fmt.Errorf("user alice@example.com: %w", prblm.ErrPermissionDenied)` | (none) | 403 | `alice@example.com`, `prblm:` |
| `fmt.Errorf("user alice@example.com: %w", prblm.ErrPermissionDenied)` | `application/problem+xml` | 403 | `alice@example.com`, `prblm:` |
| `fmt.Errorf("%w: dial tcp 10.0.0.5:5432", prblm.ErrUnavailable)` | (none) | 503 | `10.0.0.5`, `dial tcp` |
| `&VesselMissingError{ID: 7, Owner: "alice"}` reporting `prblm.ErrNotFound` | (none) | 404 | `alice`, `VesselMissingError` |

### Requirement: The hook receives the original error
The `OnError` hook SHALL receive the error exactly as returned, including the wrap text, so the application can log what the client does not see.

#### Scenario: Logging the full error
- **Given** a handler with a hook that records its calls
- **When** a handler returns `<error>`
- **Then** the hook was called once with an error whose text is `<text>` and a problem with status `<status>`

##### Examples:
| error | text | status |
|-------|------|--------|
| `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` | `vessel 7: prblm: not found` | 404 |
| `prblm.ErrConflict` | `prblm: conflict` | 409 |

### Requirement: Sentinel error strings
Each sentinel's error string SHALL start with `prblm: ` and be unique among the sentinels, so logs show where the error came from.

#### Scenario: Error text
- **Given** the library's sentinels
- **When** `<sentinel>.Error()` is called
- **Then** it returns `<text>`

##### Examples:
| sentinel | text |
|----------|------|
| `prblm.ErrInvalidInput` | `prblm: invalid input` |
| `prblm.ErrUnauthenticated` | `prblm: unauthenticated` |
| `prblm.ErrPermissionDenied` | `prblm: permission denied` |
| `prblm.ErrNotFound` | `prblm: not found` |
| `prblm.ErrConflict` | `prblm: conflict` |
| `prblm.ErrPreconditionFailed` | `prblm: precondition failed` |
| `prblm.ErrRateLimited` | `prblm: rate limited` |
| `prblm.ErrBadGateway` | `prblm: bad gateway` |
| `prblm.ErrUnavailable` | `prblm: unavailable` |
| `prblm.ErrUpstreamTimeout` | `prblm: upstream timeout` |

### Requirement: Consumer code takes precedence over sentinels
A problem or describer in the chain, a request-scoped mapper and a consumer mapper SHALL each take precedence over a sentinel's built-in mapping. A consumer mapper that returns nil or panics SHALL leave the error to the sentinel's mapping.

#### Scenario: Overriding the default problem
- **Given** `<setup>`
- **When** a handler returns `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)`, or the error given in `<setup>`
- **Then** the response status is `<status>` and the type is `<type>`
- **And** the hook received `<hook>`

##### Examples: consumer wins
| setup | status | type | hook |
|-------|--------|------|------|
| consumer mapper `prblm.Is(prblm.ErrNotFound, ...)` returning 404 with type `https://example.com/probs/vessel-not-found` | 404 | `https://example.com/probs/vessel-not-found` | the returned error |
| consumer mapper `prblm.Status(prblm.ErrNotFound, 410)` | 410 | `about:blank` | the returned error |
| scoped mapper `prblm.Status(prblm.ErrNotFound, 410)` on the request | 410 | `about:blank` | the returned error |
| the error is a problem with status 422 wrapping `prblm.ErrNotFound` | 422 | `about:blank` | the returned error |
| the error is a describer with status 409 whose `Is` reports `prblm.ErrNotFound` | 409 | `about:blank` | the returned error |
| consumer mapper `prblm.Status(prblm.ErrNotFound, 410)`, and the error is `fmt.Errorf("%w: %w", prblm.ErrBadGateway, fmt.Errorf("upstream: %w", prblm.ErrNotFound))` | 410 | `about:blank` | the returned error |
| the error is `fmt.Errorf("%w: %w", prblm.ErrBadGateway, <a problem with status 422>)` | 422 | `about:blank` | the returned error |

##### Examples: faulty consumer code falls back to the sentinel
| setup | status | type | hook |
|-------|--------|------|------|
| consumer mapper for `prblm.ErrNotFound` that returns nil | 404 | `about:blank` | the returned error |
| consumer mapper that panics | 404 | `about:blank` | a panic error, then the returned error |
| a describer returning nil whose `Is` reports `prblm.ErrNotFound` | 404 | `about:blank` | the returned error |

### Requirement: The outer of a sentinel and a status hint decides
Between a sentinel and a status hint in the same chain, the one found first in `errors.Is` order SHALL decide the status: a hint that wraps the sentinel wins, and a sentinel that wraps the hint wins. Only the hint that would apply on its own counts: the outermost one, with a status in 400–599.

#### Scenario: Sentinel and hint together
- **Given** a handler with `<builtins>`
- **When** a handler returns `<error>`
- **Then** the response status is `<status>` and the detail is `<detail>`

##### Examples:
| builtins | error | status | detail |
|----------|-------|--------|--------|
| default options | `prblm.WithStatus(prblm.ErrNotFound, 410)` | 410 | (none) |
| default options | `fmt.Errorf("x: %w", prblm.WithStatus(prblm.ErrConflict, 422))` | 422 | (none) |
| default options | `prblm.WithStatus(fmt.Errorf("repo: %w", prblm.ErrNotFound), 410)` | 410 | (none) |
| default options | `fmt.Errorf("%w: %w", prblm.ErrBadGateway, prblm.WithStatus(errors.New("upstream"), 404))` | 502 | `An upstream service returned an invalid response.` |
| default options | `prblm.WithStatus(prblm.ErrNotFound, 200)` | 404 | `The requested resource was not found.` |
| default options | `prblm.WithStatus(prblm.WithStatus(prblm.ErrNotFound, 410), 200)` | 404 | `The requested resource was not found.` |
| built-ins disabled | `prblm.WithStatus(prblm.ErrNotFound, 400)` | 400 | (none) |

### Requirement: Sentinels are disabled with the other built-ins
Disabling the built-in mappers SHALL also disable the sentinel mappings, so a sentinel then resolves like any other error.

#### Scenario: Built-ins disabled
- **Given** a handler with built-ins disabled and `<mappers>`
- **When** a handler returns `<error>`
- **Then** the response status is `<status>` and the detail is `<detail>`

##### Examples:
| mappers | error | status | detail |
|---------|-------|--------|--------|
| no mappers | `prblm.ErrNotFound` | 500 | (none) |
| no mappers | `prblm.ErrUnavailable` | 500 | (none) |
| consumer mapper `prblm.Status(prblm.ErrNotFound, 404)` | `prblm.ErrNotFound` | 404 | (none) |

### Requirement: Order when an error matches several built-ins
When an error chain matches several sentinels, the first one found in `errors.Is` order SHALL win: an error before the errors it wraps, wrapped errors in the order given. Within one error whose `Is` method reports several, the lowest status SHALL win. A sentinel SHALL win over a standard-library built-in in the same chain.

#### Scenario: Several matches in one chain
- **Given** a handler with zero configuration
- **When** a handler returns `<error>`
- **Then** the response status is `<status>` and the detail is `<detail>`

##### Examples:
| error | status | detail |
|-------|--------|--------|
| `fmt.Errorf("%w: %w", prblm.ErrBadGateway, fmt.Errorf("upstream: %w", prblm.ErrNotFound))` | 502 | `An upstream service returned an invalid response.` |
| `fmt.Errorf("vessel 7: %w", fmt.Errorf("%w: %w", prblm.ErrUnavailable, prblm.ErrInvalidInput))` | 503 | `The service is temporarily unavailable.` |
| `errors.Join(prblm.ErrConflict, prblm.ErrNotFound)` | 409 | `The request conflicts with the current state of the resource.` |
| `errors.Join(prblm.ErrUnavailable, prblm.ErrInvalidInput)` | 503 | `The service is temporarily unavailable.` |
| `errors.Join(errors.New("audit"), fmt.Errorf("x: %w", prblm.ErrNotFound), prblm.ErrConflict)` | 404 | `The requested resource was not found.` |
| a custom error whose `Is` method reports both `prblm.ErrConflict` and `prblm.ErrNotFound` | 404 | `The requested resource was not found.` |
| a custom error whose `Is` method reports `prblm.ErrConflict`, wrapping `prblm.ErrNotFound` | 409 | `The request conflicts with the current state of the resource.` |
| `fmt.Errorf("%w: %w", prblm.ErrUpstreamTimeout, context.DeadlineExceeded)` | 504 | `An upstream service did not respond in time.` |
| `fmt.Errorf("%w: %w", prblm.ErrUnavailable, context.DeadlineExceeded)` | 503 | `The service is temporarily unavailable.` |
| `fmt.Errorf("%w: %w", prblm.ErrInvalidInput, a *json.SyntaxError)` | 400 | `The request is invalid.` |
| `context.DeadlineExceeded` | 504 | (none) |

### Requirement: Degenerate errors never break resolution
Resolving nil, typed-nil and faulty custom errors SHALL NOT panic. A custom `Is` method that panics SHALL be contained like any faulty consumer code and reported to the hook.

#### Scenario: Degenerate inputs
- **Given** a handler with a hook that records its calls
- **When** a handler returns `<error>`
- **Then** the response is `<response>`
- **And** the hook received `<hook>`

##### Examples:
| error | response | hook |
|-------|----------|------|
| `nil` | nothing written | (nothing) |
| `prblm.WithStatus(nil, 404)` | nothing written | (nothing) |
| a typed-nil `*MyError` (no `Is` method) stored in an `error` | a 500 problem without detail | the returned error |
| an error whose `Is` method panics | a 500 problem without detail | one or more panic errors, then the returned error |

### Requirement: Sentinels are safe under concurrent use
Sentinel values and their built-in mappings SHALL be shared read-only, so concurrent requests returning the same sentinel get independent responses.

#### Scenario: Concurrent requests
- **Given** a handler with an enricher setting extension `trace_id` from the request
- **When** `<requests>` concurrent requests each return `fmt.Errorf("vessel %d: %w", id, prblm.ErrNotFound)` with their own id
- **Then** every response is a 404 problem with its own `trace_id` and no vessel id in the body

##### Examples:
| requests |
|----------|
| 1 |
| 100 |

### Requirement: Same responses in every adapter
For the same options, the net/http, gin and fiber v3 adapters SHALL answer a sentinel with the same status, `Content-Type`, `Vary` and body bytes. An adapter's own status hint, such as gin's abort code, wraps the error, so it SHALL decide over a sentinel in it.

#### Scenario: Adapter parity
- **Given** one options slice shared by all adapters
- **When** a `GET /v1/vessels/7` request with `Accept: <accept>` is answered through `<framework>` after `<action>`
- **Then** the response has status `<status>`, `Content-Type: <content_type>`, `Vary: Accept` and the same body bytes as net/http

##### Examples:
| framework | action | accept | status | content_type |
|-----------|--------|--------|--------|--------------|
| net/http | returning `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` | (none) | 404 | `application/problem+json` |
| gin | `c.Error(fmt.Errorf("vessel 7: %w", prblm.ErrNotFound))` | (none) | 404 | `application/problem+json` |
| fiber v3 | returning `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` | (none) | 404 | `application/problem+json` |
| fiber v3 | returning `fmt.Errorf("vessel 7: %w", prblm.ErrNotFound)` | `application/problem+xml` | 404 | `application/problem+xml` |
| gin | `c.AbortWithError(410, prblm.ErrNotFound)` | (none) | 410 | `application/problem+json` |
