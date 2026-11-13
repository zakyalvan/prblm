# problem-types Specification

## Purpose

Defines how an application gives its problem types one namespace, names them by short sub-types in business code, and declares each type once with its status, title and description.

## Requirements

### Requirement: Relative sub-types resolve against the namespace
When a handler has a namespace, finalizing a problem SHALL turn a type that is a relative sub-type into the namespace, a `/`, and the sub-type. Absolute URIs, absolute paths and `about:blank` SHALL be kept unchanged.

#### Scenario: Type resolved
- **Given** a handler with namespace `<namespace>` and a mapper returning a 409 problem with type `<given>`
- **When** a client requests `GET /v1/contracts/7` through net/http and the mapper matches
- **Then** the problem's `type` member is `<rendered>`

##### Examples: resolved
| namespace | given | rendered |
|-----------|-------|----------|
| `/v1/api/probs` | `contracts/blacklisted-supplier` | `/v1/api/probs/contracts/blacklisted-supplier` |
| `https://kartala.id/probs` | `contracts/blacklisted-supplier` | `https://kartala.id/probs/contracts/blacklisted-supplier` |
| `/v1/api/probs` | `out-of-credit` | `/v1/api/probs/out-of-credit` |
| `/v1/api/probs/` | `out-of-credit` | `/v1/api/probs/out-of-credit` |
| `https://kartala.id/probs` | `a/b/c-2` | `https://kartala.id/probs/a/b/c-2` |

##### Examples: kept
| namespace | given | rendered |
|-----------|-------|----------|
| `/v1/api/probs` | `https://example.com/probs/x` | `https://example.com/probs/x` |
| `/v1/api/probs` | `/other/probs/x` | `/other/probs/x` |
| `/v1/api/probs` | `about:blank` | (none) |
| `/v1/api/probs` | (empty) | (none) |
| `/v1/api/probs` | `urn:kartala:probs:x` | `urn:kartala:probs:x` |

### Requirement: Without a namespace, types are passed through
A handler without a namespace SHALL write every type exactly as the problem carries it, including relative references, so existing output is unchanged. For a relative type (not empty, without a scheme, not starting with `/`) the hook SHALL receive a note wrapping `ErrInvalidType`, because a client resolves it against the request URL.

#### Scenario: No namespace
- **Given** a handler with no namespace, a hook that records calls, and a mapper returning a 409 problem with type `<given>`
- **When** the mapper matches a request
- **Then** the problem's `type` member is `<rendered>`
- **And** the hook `<note>` a note wrapping `ErrInvalidType`

##### Examples:
| given | rendered | note |
|-------|----------|------|
| `contracts/blacklisted-supplier` | `contracts/blacklisted-supplier` | receives |
| `Contracts/X` | `Contracts/X` | receives |
| `https://example.com/probs/x` | `https://example.com/probs/x` | does not receive |
| `/other/probs/x` | `/other/probs/x` | does not receive |
| `about:blank` | (none) | does not receive |
| (empty) | (none) | does not receive |

#### Scenario: Problem from a definition without a namespace
- **Given** a handler with no namespace and a hook that records calls
- **When** a mapper returns `DefineType("out-of-credit", 403, "Out of credit", "").New()` for `GET /v1/orders/42`
- **Then** the problem's `type` member is `out-of-credit`
- **And** the hook receives a note wrapping `ErrInvalidType` before the error being answered

##### Examples:
| hook |
|------|
| records calls |

#### Scenario: Type notes cost nothing without a hook
- **Given** a handler with `<config>` and no hook
- **When** a problem with type `<given>` is resolved
- **Then** resolving it allocates no more than resolving the same problem with type `about:blank`

##### Examples:
| config | given |
|--------|-------|
| no namespace | `contracts/x` |
| namespace `/v1/api/probs` | `Bad Type` |
| namespace `/v1/api/probs` and type `contracts/blacklisted-supplier` registered | `contracts/unknown` |

### Requirement: Types never come from the request host
Type resolution SHALL use only the configured namespace. The request's `Host` header, `X-Forwarded-*` headers and URL SHALL NOT affect the `type` member.

#### Scenario: Hostile host headers
- **Given** a handler with namespace `<namespace>` and a mapper returning type `contracts/blacklisted-supplier`
- **When** a request arrives with `<headers>`
- **Then** the problem's `type` member is `<rendered>`

##### Examples:
| namespace | headers | rendered |
|-----------|---------|----------|
| `/v1/api/probs` | `Host: evil.example` | `/v1/api/probs/contracts/blacklisted-supplier` |
| `/v1/api/probs` | `X-Forwarded-Host: evil.example`, `X-Forwarded-Proto: http` | `/v1/api/probs/contracts/blacklisted-supplier` |
| `https://kartala.id/probs` | `Host: evil.example` | `https://kartala.id/probs/contracts/blacklisted-supplier` |

### Requirement: Invalid namespaces are rejected at construction
Creating a handler with a namespace that is neither an absolute `http`/`https` URI with a host nor an absolute path SHALL panic with a message naming `WithNamespace`. The namespace SHALL have a non-root path, and no query, fragment, user info, or `.` or `..` segment. When several `WithNamespace` options are given, the last SHALL replace the earlier ones, and only it SHALL be validated.

#### Scenario: Namespace validated
- **When** a handler is created with `WithNamespace(<namespace>)`
- **Then** construction `<outcome>`

##### Examples: accepted
| namespace | outcome |
|-----------|---------|
| `"/v1/api/probs"` | succeeds |
| `"/v1/api/probs/"` | succeeds |
| `"https://kartala.id/probs"` | succeeds |
| `"http://localhost:8080/probs"` | succeeds |

##### Examples: rejected
| namespace | outcome |
|-----------|---------|
| `""` | panics naming `WithNamespace` |
| `"v1/api/probs"` | panics naming `WithNamespace` |
| `"/"` | panics naming `WithNamespace` |
| `"//evil.example/probs"` | panics naming `WithNamespace` |
| `"https://kartala.id"` | panics naming `WithNamespace` |
| `"https://kartala.id/"` | panics naming `WithNamespace` |
| `"https:///probs"` | panics naming `WithNamespace` |
| `"ftp://kartala.id/probs"` | panics naming `WithNamespace` |
| `"https://user@kartala.id/probs"` | panics naming `WithNamespace` |
| `"https://kartala.id/probs?v=1"` | panics naming `WithNamespace` |
| `"/v1/api/probs#x"` | panics naming `WithNamespace` |
| `"/v1/../probs"` | panics naming `WithNamespace` |
| `"/v1/api probs"` | panics naming `WithNamespace` |

#### Scenario: Namespace option order
- **When** a handler is created with `<options>`
- **Then** construction `<outcome>` and the namespace in use is `<namespace>`

##### Examples:
| options | outcome | namespace |
|---------|---------|-----------|
| `WithNamespace("bad"), WithNamespace("/v1/api/probs")` | succeeds | `/v1/api/probs` |
| `WithNamespace("/a/probs"), WithNamespace("/v1/api/probs")` | succeeds | `/v1/api/probs` |
| `WithNamespace("/v1/api/probs"), WithNamespace("bad")` | panics naming `WithNamespace` | (none) |

### Requirement: Invalid relative types fall back to about:blank
With a namespace, a relative type that is not a valid sub-type SHALL be replaced by `about:blank` (no `type` member). The title SHALL become the status phrase, as RFC 9457 section 4.2.1 asks for `about:blank`, so a title written for the lost type is not sent. The status and detail SHALL be kept, and the hook SHALL receive a note wrapping `ErrInvalidType`. A valid sub-type is one or more `/`-separated segments of lowercase letters and digits, with single `-`, `_` or `.` between them.

#### Scenario: Invalid relative type
- **Given** a handler with namespace `/v1/api/probs` and `<source>` setting type `<given>` on a 409 problem titled `Locked` with detail `Contract 7 is locked.`
- **When** the problem is answered
- **Then** the response has status 409, title `Conflict`, detail `Contract 7 is locked.` and no `type` member
- **And** the hook receives a note wrapping `ErrInvalidType` before the error being answered

##### Examples: malformed sub-types
| source | given |
|--------|-------|
| a mapper | `Contracts/X` |
| a mapper | `contracts//x` |
| a mapper | `contracts/` |
| a mapper | `../admin` |
| a mapper | `./x` |
| a mapper | `contracts/x?y=1` |
| a mapper | `contracts/x#y` |
| a mapper | `x y` |
| a mapper | `x--y` |
| a mapper | `-x` |

##### Examples: other problem sources
| source | given |
|--------|-------|
| a self-describing error | `Bad Type` |
| a scoped mapper | `Bad Type` |
| an enricher | `Bad Type` |

#### Scenario: Faulty hook on a type note
- **Given** a handler with namespace `/v1/api/probs`, a mapper returning type `Bad Type` with status 409 and a hook that `<hook>`
- **When** the problem is answered
- **Then** the client receives a 409 problem without a `type` member

##### Examples:
| hook |
|------|
| panics on every call |
| panics only on the note |

### Requirement: Resolution applies to every problem source
Type resolution SHALL apply to problems from self-describing errors, scoped mappers, handler mappers and enrichers. It SHALL work on the request's own copy, so a shared package-level problem keeps its relative type.

#### Scenario: Problem sources
- **Given** a handler with namespace `/v1/api/probs`
- **When** `<source>` produces a 409 problem with type `contracts/locked`
- **Then** the problem's `type` member is `/v1/api/probs/contracts/locked`

##### Examples:
| source |
|--------|
| a self-describing error |
| a scoped mapper |
| a handler mapper |
| an enricher that sets the type on a mapped problem |

#### Scenario: Shared problem stays relative
- **Given** a package-level problem with type `contracts/locked` returned by a mapper, and a handler with namespace `/v1/api/probs`
- **When** `<requests>` concurrent requests are answered
- **Then** every response has type `/v1/api/probs/contracts/locked`
- **And** the package-level problem's type is still `contracts/locked`

##### Examples:
| requests |
|----------|
| 1 |
| 200 |

### Requirement: Type definitions build problems
A type definition SHALL carry a sub-type, a status, a title and a description. Building a problem from it SHALL give a problem with that sub-type as its type and that status and title, plus any extra fields, applied after them.

#### Scenario: Problem from a definition
- **Given** the definition `DefineType("contracts/blacklisted-supplier", 409, "Supplier is blacklisted", "The supplier is on the procurement blacklist.")` and a handler with namespace `/v1/api/probs`
- **When** a mapper returns the definition's problem built with `<fields>`
- **Then** the response is status 409 with body `<json>`

##### Examples:
| fields | json |
|--------|------|
| (none) | `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts/7"}` |
| `WithDetail("Supplier 42 is blacklisted.")` | `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"detail":"Supplier 42 is blacklisted.","instance":"/v1/contracts/7"}` |
| `WithExt("supplier_id", 42)` | `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts/7","supplier_id":42}` |
| `nil` | `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts/7"}` |

#### Scenario: Nil definition
- **Given** `<definition>` and a handler with namespace `/v1/api/probs`
- **When** a mapper returns the problem built from it with `<fields>`
- **Then** the response is the safe 500 problem with body `<json>`

##### Examples:
| definition | fields | json |
|------------|--------|------|
| a nil `*Type` | (none) | `{"title":"Internal Server Error","status":500,"instance":"/v1/contracts/7"}` |
| a nil `*Type` | `WithDetail("secret")` | `{"title":"Internal Server Error","status":500,"instance":"/v1/contracts/7"}` |
| a zero-value `&Type{}` | (none) | `{"title":"Internal Server Error","status":500,"instance":"/v1/contracts/7"}` |
| a zero-value `&Type{}` | `WithDetail("secret")` | `{"title":"Internal Server Error","status":500,"instance":"/v1/contracts/7"}` |

#### Scenario: Nil definition accessors
- **Given** a nil `*Type`
- **When** `<accessor>` is called
- **Then** it returns `<value>` without panicking

##### Examples:
| accessor | value |
|----------|-------|
| `Subtype()` | `""` |
| `Status()` | `0` |
| `Title()` | `""` |
| `Description()` | `""` |

#### Scenario: Each problem is a fresh copy
- **Given** the definition above
- **When** two problems are built from it and the first gets `WithDetail("a")`
- **Then** the second problem's detail is `<detail>`

##### Examples:
| detail |
|--------|
| (empty) |

### Requirement: Invalid type definitions are rejected
Defining a type with an invalid sub-type, a status outside 400–599 or an empty title SHALL panic with a message naming `DefineType` and the offending value. The description MAY be empty.

#### Scenario: Definition validated
- **When** a type is defined with sub-type `<subtype>`, status `<status>` and title `<title>`
- **Then** definition `<outcome>`

##### Examples: accepted
| subtype | status | title | outcome |
|---------|--------|-------|---------|
| `contracts/blacklisted-supplier` | 409 | `Supplier is blacklisted` | succeeds |
| `out-of-credit` | 403 | `Out of credit` | succeeds |
| `v2/limits/rate.exceeded` | 429 | `Rate exceeded` | succeeds |
| `x` | 400 | `X` | succeeds |
| `x` | 599 | `X` | succeeds |

##### Examples: rejected
| subtype | status | title | outcome |
|---------|--------|-------|---------|
| (empty) | 409 | `X` | panics naming `DefineType` and the sub-type |
| `Contracts/X` | 409 | `X` | panics naming `DefineType` and the sub-type |
| `/contracts/x` | 409 | `X` | panics naming `DefineType` and the sub-type |
| `https://kartala.id/x` | 409 | `X` | panics naming `DefineType` and the sub-type |
| `contracts//x` | 409 | `X` | panics naming `DefineType` and the sub-type |
| `contracts/../x` | 409 | `X` | panics naming `DefineType` and the sub-type |
| `x` | 399 | `X` | panics naming `DefineType` and the status |
| `x` | 600 | `X` | panics naming `DefineType` and the status |
| `x` | 0 | `X` | panics naming `DefineType` and the status |
| `x` | 409 | (empty) | panics naming `DefineType` and the title |
| `x` | 409 | `"   "` | panics naming `DefineType` and the title |

### Requirement: Definitions are registered with the handler
`WithTypes` SHALL register type definitions with a handler and SHALL skip nil definitions. Registering two definitions with the same sub-type, registering any definition without a namespace, or registering a definition that `DefineType` would reject (such as a zero-value `Type`), SHALL panic at construction with a message naming `WithTypes`.

#### Scenario: Registration validated
- **When** a handler is created with `<options>`
- **Then** construction `<outcome>`

##### Examples: accepted
| options | outcome |
|---------|---------|
| `WithNamespace("/v1/api/probs"), WithTypes(A, B)` | succeeds |
| `WithTypes(A), WithNamespace("/v1/api/probs")` | succeeds |
| `WithNamespace("/v1/api/probs"), WithTypes(A), WithTypes(B)` | succeeds |
| `WithNamespace("/v1/api/probs"), WithTypes(A, nil)` | succeeds |
| `WithNamespace("/v1/api/probs"), WithTypes()` | succeeds |
| `WithTypes()` | succeeds |

##### Examples: rejected
| options | outcome |
|---------|---------|
| `WithTypes(A)` | panics naming `WithTypes` and the missing namespace |
| `WithNamespace("/v1/api/probs"), WithTypes(A, A)` | panics naming `WithTypes` and the sub-type |
| `WithNamespace("/v1/api/probs"), WithTypes(A), WithTypes(A2)` where `A2` reuses A's sub-type | panics naming `WithTypes` and the sub-type |
| `WithNamespace("/v1/api/probs"), WithTypes(&Type{})` | panics naming `WithTypes` and the invalid definition |
| `WithNamespace("/v1/api/probs"), WithTypes(A, &Type{})` | panics naming `WithTypes` and the invalid definition |

### Requirement: Undocumented types are noted
When discovery is on, a resolved relative sub-type with no registered definition SHALL leave the response unchanged and SHALL give the hook a note wrapping `ErrUndocumentedType`, so the broken documentation link is visible. Without discovery there SHALL be no such note.

#### Scenario: Undocumented sub-type
- **Given** a handler with namespace `/v1/api/probs`, `<config>`, and a mapper returning type `<given>` with status 409
- **When** the problem is answered
- **Then** the problem's `type` member is `<rendered>`
- **And** the hook `<note>` a note wrapping `ErrUndocumentedType`

##### Examples:
| config | given | rendered | note |
|--------|-------|----------|------|
| type `contracts/blacklisted-supplier` registered | `contracts/unknown` | `/v1/api/probs/contracts/unknown` | receives |
| type `contracts/blacklisted-supplier` registered | `contracts/blacklisted-supplier` | `/v1/api/probs/contracts/blacklisted-supplier` | does not receive |
| type `contracts/blacklisted-supplier` registered and `WithoutDiscovery()` | `contracts/unknown` | `/v1/api/probs/contracts/unknown` | does not receive |
| no types registered | `contracts/unknown` | `/v1/api/probs/contracts/unknown` | does not receive |
| type `contracts/blacklisted-supplier` registered | `https://example.com/probs/x` | `https://example.com/probs/x` | does not receive |
