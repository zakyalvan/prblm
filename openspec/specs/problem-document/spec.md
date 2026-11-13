# problem-document Specification

## Purpose

Defines the RFC 9457 problem details document the library produces and consumes: its standard members, extension members, and JSON and XML representations.

## Requirements

### Requirement: Standard problem members
A problem SHALL carry the RFC 9457 members `type`, `status`, `title`, `detail` and `instance`, serialized under exactly those names, with `status` as a number. Members with empty values SHALL be omitted.

#### Scenario: Members serialized to JSON
- **Given** a problem with type `<type>`, status `<status>`, title `<title>`, detail `<detail>` and instance `<instance>`
- **When** the problem is encoded as JSON
- **Then** the document is `<json>`

##### Examples: all members
| type | status | title | detail | instance | json |
|------|--------|-------|--------|----------|------|
| `https://example.com/probs/out-of-credit` | 403 | `You do not have enough credit.` | `Your current balance is 30, but that costs 50.` | `/account/12345/msgs/abc` | `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc"}` |

##### Examples: empty members omitted
| type | status | title | detail | instance | json |
|------|--------|-------|--------|----------|------|
| (empty) | 500 | `Internal Server Error` | (empty) | (empty) | `{"title":"Internal Server Error","status":500}` |
| (empty) | 404 | `Not Found` | (empty) | `/v1/vessels/7` | `{"title":"Not Found","status":404,"instance":"/v1/vessels/7"}` |

### Requirement: Problems can be built in one expression
`New` SHALL accept field options that set the `type`, `title` and `detail` members, so a problem with any members and extensions can be declared as a single expression, for example a package-level variable.

#### Scenario: Field options
- **Given** a problem built with `New(403, <fields>)`
- **When** the problem is encoded as JSON
- **Then** the document is `<json>`

##### Examples:
| fields | json |
|--------|------|
| `WithType("https://example.com/probs/out-of-credit")` | `{"type":"https://example.com/probs/out-of-credit","status":403}` |
| `WithTitle("You do not have enough credit.")` | `{"title":"You do not have enough credit.","status":403}` |
| `WithDetail("Your balance is 30.")` | `{"status":403,"detail":"Your balance is 30."}` |
| `WithTitle("Out of credit"), WithExt("balance", 30)` | `{"title":"Out of credit","status":403,"balance":30}` |

### Requirement: Default type is about:blank
A problem without a type SHALL have the effective type `about:blank`. The `type` member SHALL be omitted from serialized output when the effective type is `about:blank`.

#### Scenario: Effective type
- **Given** a problem created with type `<given>`
- **When** its effective type is read and it is encoded as JSON
- **Then** the effective type is `<effective>`
- **And** the JSON `<has_type>` a `type` member

##### Examples:
| given | effective | has_type |
|-------|-----------|----------|
| (empty) | `about:blank` | has no |
| `about:blank` | `about:blank` | has no |
| `https://example.com/probs/x` | `https://example.com/probs/x` | has |

### Requirement: Title defaults to the HTTP reason phrase
When a problem's effective type is `about:blank` and its title is empty, finalizing the problem SHALL set the title to the standard HTTP reason phrase of its status. An explicit title SHALL be kept.

#### Scenario: Title finalized
- **Given** a problem with type `<type>`, status `<status>` and title `<title>`
- **When** the problem is finalized for writing
- **Then** its title is `<final>`

##### Examples: filled from status
| type | status | title | final |
|------|--------|-------|-------|
| (empty) | 404 | (empty) | `Not Found` |
| (empty) | 422 | (empty) | `Unprocessable Entity` |
| (empty) | 503 | (empty) | `Service Unavailable` |

##### Examples: kept
| type | status | title | final |
|------|--------|-------|-------|
| (empty) | 404 | `Vessel not found` | `Vessel not found` |
| `https://example.com/probs/x` | 409 | (empty) | (empty) |

### Requirement: Problem is a Go error
A problem SHALL implement the Go `error` interface, with a non-empty message. It SHALL optionally carry a cause, reachable through standard unwrapping and never serialized. Calling its error method on a nil problem SHALL NOT panic.

#### Scenario: Error message and cause
- **Given** a problem with status `<status>`, title `<title>`, detail `<detail>` and cause `<cause>`
- **When** the error message is read, the chain is checked against `<cause>`, and the problem is encoded as JSON
- **Then** the message is `<message>`
- **And** the chain match is `<matches>`
- **And** the JSON contains no text of `<cause>`

##### Examples:
| status | title | detail | cause | message | matches |
|--------|-------|--------|-------|---------|---------|
| 404 | `Not Found` | `vessel 7 not found` | `sql.ErrNoRows` | `Not Found: vessel 7 not found` | true |
| 500 | (empty) | (empty) | (none) | `Internal Server Error` | false |
| 418 | `Teapot` | (empty) | `io.EOF` | `Teapot` | true |

#### Scenario: Nil problem
- **Given** a nil problem pointer
- **When** its error message is read
- **Then** the call does not panic and returns `<message>`

##### Examples:
| message |
|---------|
| `<nil problem>` |

### Requirement: Extension members at top level
Extension members SHALL be serialized as top-level members next to the standard members, sorted by name after the standard members, never under a wrapper member.

#### Scenario: Extensions serialized flat and sorted
- **Given** a problem with status 403 and extensions `<extensions>`
- **When** the problem is finalized and encoded as JSON
- **Then** the document is `<json>`

##### Examples:
| extensions | json |
|------------|------|
| `balance=30` | `{"title":"Forbidden","status":403,"balance":30}` |
| `trace_id="abc"`, `balance=30` | `{"title":"Forbidden","status":403,"balance":30,"trace_id":"abc"}` |
| `accounts=["/a/1","/a/2"]` | `{"title":"Forbidden","status":403,"accounts":["/a/1","/a/2"]}` |

### Requirement: Extension names are validated
An extension name SHALL be at least three characters, start with an ASCII letter, contain only ASCII letters, digits and underscore, and not equal a standard member name. Setting an invalid name SHALL return an error and leave the problem unchanged.

#### Scenario: Extension name check
- **Given** a problem with no extensions
- **When** extension `<name>` is set
- **Then** the result is `<result>`
- **And** the problem has `<count>` extensions

##### Examples: accepted
| name | result | count |
|------|--------|-------|
| `trace_id` | ok | 1 |
| `abc` | ok | 1 |
| `Balance2` | ok | 1 |

##### Examples: rejected
| name | result | count |
|------|--------|-------|
| `ab` | invalid extension name error | 0 |
| `1abc` | invalid extension name error | 0 |
| `_abc` | invalid extension name error | 0 |
| `field.name` | invalid extension name error | 0 |
| `trace-id` | invalid extension name error | 0 |
| `nãme` | invalid extension name error | 0 |
| (empty) | invalid extension name error | 0 |
| `status` | reserved name error | 0 |
| `type` | reserved name error | 0 |

### Requirement: Extension values follow JSON rules
Extension values SHALL be encoded with standard Go JSON rules, including struct `json` tags. A value that cannot be encoded SHALL make encoding fail with an error instead of producing a partial document.

#### Scenario: Extension value encoding
- **Given** a problem with status 422 and extension `errors` holding `<value>`
- **When** the problem is encoded as JSON
- **Then** the result is `<result>`

##### Examples: encodable
| value | result |
|-------|--------|
| `[{Detail:"must be positive",Pointer:"#/age"}]` with tags `json:"detail"`, `json:"pointer"` | `"errors":[{"detail":"must be positive","pointer":"#/age"}]` in the document |
| `nil` | `"errors":null` in the document |

##### Examples: not encodable
| value | result |
|-------|--------|
| a `chan int` | encoding error |
| `math.NaN()` | encoding error |
| a self-referencing pointer cycle | encoding error |

### Requirement: Problems carry response headers
A problem SHALL be able to carry HTTP response headers that are never serialized into the document.

#### Scenario: Header not in body
- **Given** a problem with status `<status>` and header `<header>`
- **When** the problem is encoded as JSON
- **Then** the document has no member named after `<header>`

##### Examples:
| status | header |
|--------|--------|
| 503 | `Retry-After: 5` |
| 401 | `WWW-Authenticate: Bearer` |

### Requirement: XML encoding per RFC 9457 Appendix B
A problem SHALL be encodable as XML: root element `problem` in namespace `urn:ietf:rfc:7807`, one child element per member, nested elements for objects (sorted by key), `i` elements for array items, empty elements for null.

#### Scenario: XML encoding
- **Given** a problem with status 403, title `Forbidden` and extension `<extension>`
- **When** the problem is encoded as XML
- **Then** the root is `<problem xmlns="urn:ietf:rfc:7807">` containing `<status>403</status>` and `<title>Forbidden</title>`
- **And** it contains `<xml>`

##### Examples:
| extension | xml |
|-----------|-----|
| `balance=30` | `<balance>30</balance>` |
| `accounts=["/a/1","/a/2"]` | `<accounts><i>/a/1</i><i>/a/2</i></accounts>` |
| `errors=[{detail:"must be positive",pointer:"#/age"}]` | `<errors><i><detail>must be positive</detail><pointer>#/age</pointer></i></errors>` |
| `meta={b:1,a:2}` | `<meta><a>2</a><b>1</b></meta>` |
| `note=null` | `<note></note>` |
| `text="a<b&c"` | `<text>a&lt;b&amp;c</text>` |

### Requirement: XML and JSON share one data model
An extension value SHALL produce the same logical structure in XML as in JSON: names from `json` tags, the same omitted fields, the same nesting.

#### Scenario: Tag options respected in XML
- **Given** an extension `item` holding a struct whose field `<field>` has tag `<tag>` and value `<value>`
- **When** the problem is encoded as XML
- **Then** the XML `<outcome>`

##### Examples:
| field | tag | value | outcome |
|-------|-----|-------|---------|
| `Code` | `json:"code"` | `"x1"` | contains `<item><code>x1</code></item>` |
| `Hint` | `json:"hint,omitempty"` | `""` | contains no `hint` element |
| `Secret` | `json:"-"` | `"s3cr3t"` | contains no `s3cr3t` |

### Requirement: Nested object keys must be valid XML names
When an extension value contains an object key that is not a valid XML element name, XML encoding SHALL fail with an error, never produce malformed XML.

#### Scenario: Invalid nested key
- **Given** an extension `meta` holding a map with key `<key>`
- **When** the problem is encoded as XML
- **Then** the result is `<result>`

##### Examples:
| key | result |
|-----|--------|
| `valid_key` | encoded |
| `file.name` | XML name error |
| `1st` | XML name error |
| `a b` | XML name error |
| (empty) | XML name error |

### Requirement: Lenient decoding
Decoding a problem from JSON or XML SHALL keep unknown members as extensions and ignore standard members whose value has the wrong type, without failing. Input that is not a JSON object or a `problem` XML element SHALL fail with an error.

#### Scenario: Decode JSON
- **Given** the JSON input `<input>`
- **When** it is decoded as a problem
- **Then** the result is `<result>`

##### Examples: tolerated
| input | result |
|-------|--------|
| `{"status":404,"title":"Not Found"}` | status 404, title `Not Found` |
| `{"status":"404","title":"Not Found","balance":30}` | status 0, title `Not Found`, extension `balance`=30 |
| `{"title":42,"detail":"d"}` | title empty, detail `d` |
| `{}` | empty problem, effective type `about:blank` |

##### Examples: rejected
| input | result |
|-------|--------|
| `[1,2]` | decode error |
| `"text"` | decode error |
| `{"status":` | decode error |
| (empty) | decode error |

#### Scenario: Decode XML
- **Given** the XML input `<input>`
- **When** it is decoded as a problem
- **Then** the result is `<result>`

##### Examples:
| input | result |
|-------|--------|
| `<problem xmlns="urn:ietf:rfc:7807"><status>404</status><title>Not Found</title></problem>` | status 404, title `Not Found` |
| `<problem xmlns="urn:ietf:rfc:7807"><status>abc</status><balance>30</balance></problem>` | status 0, extension `balance` present |
| `<other/>` | decode error |

### Requirement: Parse a problem from an HTTP response
The library SHALL decode a problem from an HTTP response, choosing JSON or XML by the response `Content-Type`, and SHALL return an error for any other media type or an unreadable body.

#### Scenario: Parse response
- **Given** an HTTP response with `Content-Type: <content_type>` and body `<body>`
- **When** it is parsed
- **Then** the result is `<result>`

##### Examples: parsed
| content_type | body | result |
|--------------|------|--------|
| `application/problem+json` | `{"status":404,"title":"Not Found"}` | status 404 |
| `application/problem+json; charset=utf-8` | `{"status":409}` | status 409 |
| `application/problem+xml` | `<problem xmlns="urn:ietf:rfc:7807"><status>404</status></problem>` | status 404 |

##### Examples: refused
| content_type | body | result |
|--------------|------|--------|
| `text/html` | `<html></html>` | not-a-problem error |
| (none) | `{"status":404}` | not-a-problem error |
| `application/problem+json` | a body whose read fails | read error |
| `application/problem+json` | `{"status":` | decode error |
