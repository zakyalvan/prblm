# Spec Delta

## Purpose

Lets a client dereference a problem's type URL and read that type's documentation, as RFC 9457 recommends, served from the type definitions the application registered.

## ADDED Requirements

### Requirement: Type documentation at the type URL
When discovery is on, a GET request for the path of a registered type's URL SHALL return 200 with that type's documentation: sub-type, resolved type, status, title and description. It SHALL be HTML by default and JSON when the client prefers `application/json`.

#### Scenario: Documentation served
- **Given** a handler with namespace `/v1/api/probs` and the type `contracts/blacklisted-supplier` (409, `Supplier is blacklisted`, description `The supplier is on the procurement blacklist.`) registered, served by its discovery handler over net/http
- **When** a client sends `GET /v1/api/probs/contracts/blacklisted-supplier` with `Accept: <accept>`
- **Then** the response status is 200 and the `Content-Type` is `<content_type>`
- **And** the body `<body>`

##### Examples: HTML
| accept | content_type | body |
|--------|--------------|------|
| (none) | `text/html; charset=utf-8` | contains `Supplier is blacklisted`, `409` and `The supplier is on the procurement blacklist.` |
| `text/html` | `text/html; charset=utf-8` | contains `Supplier is blacklisted` |
| `*/*` | `text/html; charset=utf-8` | contains `Supplier is blacklisted` |
| `application/json;q=0.5, text/html` | `text/html; charset=utf-8` | contains `Supplier is blacklisted` |
| `application/json, text/html` | `text/html; charset=utf-8` | contains `Supplier is blacklisted` |
| `text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8` (a browser) | `text/html; charset=utf-8` | is a complete HTML page (`<!DOCTYPE html>`, `<title>Supplier is blacklisted</title>`) containing `409` and the description |

##### Examples: JSON
| accept | content_type | body |
|--------|--------------|------|
| `application/json` | `application/json` | is `{"type":"/v1/api/probs/contracts/blacklisted-supplier","subtype":"contracts/blacklisted-supplier","status":409,"title":"Supplier is blacklisted","description":"The supplier is on the procurement blacklist."}` |
| `text/html;q=0.5, application/json` | `application/json` | is the JSON document above |

##### Examples: malformed Accept
| accept | content_type | body |
|--------|--------------|------|
| `;;;` | `text/html; charset=utf-8` | contains `Supplier is blacklisted` |
| `image/png` | `text/html; charset=utf-8` | contains `Supplier is blacklisted` |

#### Scenario: HEAD request
- **Given** the handler and registered type above, served by its discovery handler over net/http
- **When** a client sends `HEAD <path>`
- **Then** the response status is `<status>`, the `Content-Type` is `<content_type>` and the body is empty

##### Examples:
| path | status | content_type |
|------|--------|--------------|
| `/v1/api/probs/contracts/blacklisted-supplier` | 200 | `text/html; charset=utf-8` |
| `/v1/api/probs/contracts/unknown` | 404 | `application/problem+json` |

### Requirement: Index of registered types
When discovery is on, a GET request for the namespace path SHALL return 200 with every registered type, sorted by sub-type. Each entry SHALL link to its documentation. The index SHALL be HTML by default and JSON when the client prefers `application/json`.

#### Scenario: Index served
- **Given** a handler with namespace `/v1/api/probs` and the types `<registered>` registered, served by its discovery handler over net/http
- **When** a client sends `GET <path>` with `Accept: application/json`
- **Then** the response status is 200 and the body is `<json>`

##### Examples:
| registered | path | json |
|------------|------|------|
| `out-of-credit` (403, `Out of credit`), `contracts/blacklisted-supplier` (409, `Supplier is blacklisted`) | `/v1/api/probs` | `{"types":[{"type":"/v1/api/probs/contracts/blacklisted-supplier","subtype":"contracts/blacklisted-supplier","status":409,"title":"Supplier is blacklisted"},{"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit"}]}` |
| `out-of-credit` (403, `Out of credit`) | `/v1/api/probs/` | `{"types":[{"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit"}]}` |

#### Scenario: HTML index links
- **Given** a handler with namespace `<namespace>` and the type `out-of-credit` registered, served by its discovery handler over net/http
- **When** a client sends `GET <path>` with `Accept: text/html`
- **Then** the HTML contains a link with `href="<href>"`

##### Examples:
| namespace | path | href |
|-----------|------|------|
| `/v1/api/probs` | `/v1/api/probs` | `/v1/api/probs/out-of-credit` |
| `https://kartala.id/probs` | `/probs` | `https://kartala.id/probs/out-of-credit` |

### Requirement: Unknown paths under the namespace are not found
A GET or HEAD request under the namespace path that names no registered type SHALL return a 404 `about:blank` problem, negotiated like any other problem. It SHALL NOT call the hook, because no application error occurred.

#### Scenario: Unknown type
- **Given** a handler with namespace `/v1/api/probs`, the type `contracts/blacklisted-supplier` registered and a hook that records calls, served by its discovery handler over net/http
- **When** a client sends `GET <path>`
- **Then** the response is a 404 problem with title `Not Found` and `Content-Type: application/problem+json`
- **And** the hook was called 0 times

##### Examples:
| path |
|------|
| `/v1/api/probs/contracts/unknown` |
| `/v1/api/probs/contracts/blacklisted-supplier/` |
| `/v1/api/probs/contracts/blacklisted-supplier/extra` |
| `/v1/api/probs/Contracts/Blacklisted-Supplier` |
| `/v1/api/probs/contracts` |
| `/v1/api/probs/contracts/%2e%2e/out-of-credit` |
| `/v1/api/probs/contracts/blacklisted-supplier%00` |

### Requirement: Methods other than GET and HEAD are refused
The discovery handler SHALL answer any method other than GET or HEAD with a 405 problem and `Allow: GET, HEAD`.

#### Scenario: Wrong method
- **Given** the discovery handler for namespace `/v1/api/probs` with a registered type
- **When** it receives `<method> /v1/api/probs/contracts/blacklisted-supplier`
- **Then** the response is a 405 problem with header `Allow: GET, HEAD`

##### Examples:
| method |
|--------|
| `POST` |
| `PUT` |
| `DELETE` |
| `OPTIONS` |

### Requirement: Documentation content is inert
Documentation responses SHALL escape all definition text in HTML and JSON. They SHALL carry `X-Content-Type-Options: nosniff` and `Vary: Accept`, and HTML SHALL also carry `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'`.

#### Scenario: Hostile definition text
- **Given** a registered type whose title is `<title>` and whose description is `<description>`
- **When** a client requests its documentation as `<format>`
- **Then** the body contains `<contains>` and does not contain `<absent>`

##### Examples:
| title | description | format | contains | absent |
|-------|-------------|--------|----------|--------|
| `Locked` | `<script>alert(1)</script>` | HTML | `&lt;script&gt;alert(1)&lt;/script&gt;` | `<script>` |
| `<img src=x onerror=alert(1)>` | `x` | HTML | `&lt;img src=x onerror=alert(1)&gt;` | `<img` |
| `Locked` | `"quoted" & <tagged>` | JSON | `"\"quoted\" \u0026 \u003ctagged\u003e"`, which decodes to the description | `<tagged>` |
| `Locked` | `line one\nline two` | HTML | `line one` | `<script>` |

#### Scenario: Security headers
- **Given** a registered type
- **When** a client requests its documentation as `<format>`
- **Then** the response has headers `<headers>`

##### Examples:
| format | headers |
|--------|---------|
| HTML | `X-Content-Type-Options: nosniff`, `Vary: Accept`, `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'` |
| JSON | `X-Content-Type-Options: nosniff`, `Vary: Accept` |
| index HTML | `X-Content-Type-Options: nosniff`, `Vary: Accept`, `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'` |

### Requirement: Documentation is cacheable by browsers
Successful documentation and index responses SHALL carry `Cache-Control: public, max-age=3600` and a strong `ETag` unique to the representation. A GET or HEAD request whose `If-None-Match` matches SHALL get 304 with that `ETag` and no body. Problem responses (404, 405) SHALL NOT carry these caching headers.

#### Scenario: Caching headers
- **Given** a handler with namespace `/v1/api/probs` and the type `out-of-credit` registered, served by its discovery handler over net/http
- **When** a client sends `GET <path>` with `Accept: <accept>`
- **Then** the response status is `<status>` and it `<caching>`

##### Examples: cacheable
| path | accept | status | caching |
|------|--------|--------|---------|
| `/v1/api/probs/out-of-credit` | `text/html` | 200 | has `Cache-Control: public, max-age=3600` and a quoted `ETag` |
| `/v1/api/probs/out-of-credit` | `application/json` | 200 | has `Cache-Control: public, max-age=3600` and a quoted `ETag` different from the HTML one |
| `/v1/api/probs` | `text/html` | 200 | has `Cache-Control: public, max-age=3600` and a quoted `ETag` |

##### Examples: not cacheable
| path | accept | status | caching |
|------|--------|--------|---------|
| `/v1/api/probs/unknown` | `text/html` | 404 | has the problem default `Cache-Control: no-store`, not `public, max-age=3600`, and no `ETag` |

#### Scenario: Conditional requests
- **Given** the handler above and the `ETag` `E` of the HTML documentation of `out-of-credit`
- **When** a client sends `<method> /v1/api/probs/out-of-credit` with `Accept: <accept>` and `If-None-Match: <if_none_match>`
- **Then** the response status is `<status>`, its `ETag` is `<etag>` and the body is `<body>`

##### Examples: revalidated
| method | accept | if_none_match | status | etag | body |
|--------|--------|---------------|--------|------|------|
| `GET` | `text/html` | `E` | 304 | `E` | (empty) |
| `HEAD` | `text/html` | `E` | 304 | `E` | (empty) |
| `GET` | `text/html` | `"other", E` | 304 | `E` | (empty) |
| `GET` | `text/html` | `W/E` (weak form of `E`) | 304 | `E` | (empty) |
| `GET` | `text/html` | `*` | 304 | `E` | (empty) |

##### Examples: full response
| method | accept | if_none_match | status | etag | body |
|--------|--------|---------------|--------|------|------|
| `GET` | `text/html` | `"other"` | 200 | `E` | the HTML page |
| `GET` | `application/json` | `E` | 200 | the JSON `ETag` | the JSON document |
| `GET` | `text/html` | `;;;garbage` | 200 | `E` | the HTML page |
| `GET` | `text/html` | (empty) | 200 | `E` | the HTML page |

#### Scenario: ETag changes with the definition
- **Given** two handlers that register the type `out-of-credit` with descriptions `<first>` and `<second>`
- **When** both serve its HTML documentation
- **Then** their `ETag` values are `<relation>`

##### Examples:
| first | second | relation |
|-------|--------|----------|
| `Balance too low.` | `Balance too low.` | equal |
| `Balance too low.` | `Balance is below the price.` | different |

### Requirement: Discovery is on only with registered types
Discovery SHALL be on exactly when at least one type is registered and `WithoutDiscovery` is not set. With discovery off, the discovery path SHALL be empty and the discovery handler SHALL answer every request with a 404 problem.

#### Scenario: Discovery state
- **Given** a handler created with `<options>`
- **When** its discovery path is read and its discovery handler receives `GET /v1/api/probs/out-of-credit`
- **Then** the discovery path is `<path>` and the response status is `<status>`

##### Examples: on
| options | path | status |
|---------|------|--------|
| `WithNamespace("/v1/api/probs"), WithTypes(OutOfCredit)` | `/v1/api/probs` | 200 |
| `WithNamespace("/v1/api/probs/"), WithTypes(OutOfCredit)` | `/v1/api/probs` | 200 |

##### Examples: off
| options | path | status |
|---------|------|--------|
| (none) | (empty) | 404 |
| `WithNamespace("/v1/api/probs")` | (empty) | 404 |
| `WithNamespace("/v1/api/probs"), WithTypes()` | (empty) | 404 |
| `WithNamespace("/v1/api/probs"), WithTypes(OutOfCredit), WithoutDiscovery()` | (empty) | 404 |
| `WithoutDiscovery(), WithNamespace("/v1/api/probs"), WithTypes(OutOfCredit)` | (empty) | 404 |

### Requirement: Absolute namespaces serve their path
With an absolute-URI namespace, the discovery path SHALL be that URI's path, and the documentation's `type` SHALL be the absolute URI. Routing that host to the application is left to the operator.

#### Scenario: Absolute namespace
- **Given** a handler with namespace `https://kartala.id/probs` and the type `out-of-credit` (403, `Out of credit`) registered, served by its discovery handler over net/http
- **When** a client sends `GET <path>` with `Accept: application/json`
- **Then** the response status is `<status>` and the `type` member is `<type>`

##### Examples:
| path | status | type |
|------|--------|------|
| `/probs/out-of-credit` | 200 | `https://kartala.id/probs/out-of-credit` |
| `/v1/api/probs/out-of-credit` | 404 | (none) |

### Requirement: Mounting discovery in net/http
The handler SHALL expose its discovery path and an `http.Handler` serving discovery. Together they let a net/http application mount discovery at the namespace path, next to its other routes.

#### Scenario: net/http mount
- **Given** an `http.ServeMux` with `GET /v1/contracts/{id}` and the discovery handler mounted under `<pattern>`
- **When** a client sends `GET <path>`
- **Then** the response status is `<status>`

##### Examples:
| pattern | path | status |
|---------|------|--------|
| `GET /v1/api/probs/` | `/v1/api/probs/out-of-credit` | 200 |
| `GET /v1/api/probs/` | `/v1/api/probs/` | 200 |
| `GET /v1/api/probs/` | `/v1/contracts/7` | 200 (from the contract handler) |

### Requirement: Concurrent discovery requests
Discovery SHALL be safe for concurrent use, and documentation SHALL be built from definitions that cannot change after the handler is created.

#### Scenario: Concurrent documentation requests
- **Given** a handler with two registered types, served by its discovery handler over net/http
- **When** `<requests>` requests alternate between the two documentation paths and the index concurrently, with mixed `Accept` headers
- **Then** every response is 200 with the documentation its path and `Accept` call for

##### Examples:
| requests |
|----------|
| 300 |
