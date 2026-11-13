# Spec Delta

## Purpose

Provides an opt-in mapper that turns go-playground/validator errors into the RFC 9457 validation-error shape, with JSON Pointers to the offending fields and optional per-request translation of messages.

## ADDED Requirements

### Requirement: Opt-in mapping in its own module
The validation mapper SHALL live in its own Go module, SHALL have no effect unless registered, and SHALL NOT add validator packages to the root module's dependencies.

#### Scenario: Registered or not
- **Given** a problem handler `<registration>`
- **When** a handler returns the `validator.ValidationErrors` from validating an invalid request body
- **Then** the response status is `<status>`

##### Examples:
| registration | status |
|--------------|--------|
| with the validation mapper registered | 422 |
| without the validation mapper | 500 |

### Requirement: Validation errors become a validation problem
When registered, non-empty `validator.ValidationErrors` in the chain SHALL produce a problem with status 422, detail `The request is not valid.` and an `errors` extension holding one `{detail, pointer}` object per failed field, in the validator's order.

#### Scenario: Invalid fields
- **Given** a validator that reports JSON tag names and the validation mapper registered
- **When** validating `<body>` against a struct with `age` (`min=1`) and `profile.color` (`oneof=red green blue`) fails
- **Then** the response status is 422 with `Content-Type: application/problem+json`
- **And** the `errors` member is `<errors>`

##### Examples:
| body | errors |
|------|--------|
| `{"age":0,"profile":{"color":"red"}}` | `[{"detail":"failed validation: min","pointer":"#/age"}]` |
| `{"age":0,"profile":{"color":"pink"}}` | `[{"detail":"failed validation: min","pointer":"#/age"},{"detail":"failed validation: oneof","pointer":"#/profile/color"}]` |

#### Scenario: Wrapped and empty validation errors
- **Given** the validation mapper registered and no other mappers
- **When** a handler returns `<error>`
- **Then** the response status is `<status>`

##### Examples:
| error | status |
|-------|--------|
| `fmt.Errorf("decode: %w", validationErrors)` | 422 |
| `errors.Join(io.EOF, validationErrors)` | 422 |
| an empty `validator.ValidationErrors{}` | 500 |

### Requirement: Rejected values never appear
Validation problems SHALL NOT include the rejected value, the Go struct or type names, or validator parameters beyond the tag name, unless a consumer-supplied translation includes them.

#### Scenario: Sensitive input
- **Given** the validation mapper registered without a translator
- **When** validating `{"password":"hunter2"}` fails on field `password` (`min=12`) of struct `SignupRequest`
- **Then** the response body contains none of `<forbidden>`

##### Examples:
| forbidden |
|-----------|
| `hunter2`, `SignupRequest`, `12` |

### Requirement: Pointers use JSON field names
Each `pointer` SHALL be an RFC 6901 JSON Pointer in URI fragment form, built from the field namespace without the root struct name, with slice indexes and map keys as segments and `~` and `/` escaped.

#### Scenario: Pointer building
- **Given** a validator that reports JSON tag names
- **When** the field at namespace `<namespace>` fails validation
- **Then** the entry's pointer is `<pointer>`

##### Examples:
| namespace | pointer |
|-----------|---------|
| `Signup.age` | `#/age` |
| `Signup.profile.color` | `#/profile/color` |
| `Order.items[0].name` | `#/items/0/name` |
| `Order.items[12].tags[3]` | `#/items/12/tags/3` |
| `Config.meta[region]` | `#/meta/region` |
| `Config.meta[a/b]` | `#/meta/a~1b` |
| `Config.meta[x~y]` | `#/meta/x~0y` |
| `Config.meta[café]` | `#/meta/caf%C3%A9` |
| (empty, from validating a single variable) | `#` |

### Requirement: Field fallback without JSON names
When the validator reports Go struct field names rather than JSON names, each entry SHALL carry a `field` member holding the namespace without the root struct name, instead of a `pointer`.

#### Scenario: No tag name function
- **Given** a validator without a tag name function
- **When** field `<namespace>` fails validation
- **Then** the entry is `<entry>`

##### Examples:
| namespace | entry |
|-----------|-------|
| `Signup.VesselName` | `{"detail":"failed validation: required","field":"VesselName"}` |
| `Order.Items[0].Name` | `{"detail":"failed validation: required","field":"Items[0].Name"}` |

### Requirement: Status, type and title are configurable
Consumers SHALL be able to set the status (400–499 only) and the type and title of validation problems. An invalid status SHALL panic at construction.

#### Scenario: Configured problem
- **Given** the validation mapper registered with `<option>`
- **When** validation fails
- **Then** the response has status `<status>`, type `<type>` and title `<title>`

##### Examples:
| option | status | type | title |
|--------|--------|------|-------|
| no options | 422 | `about:blank` | `Unprocessable Entity` |
| status 400 | 400 | `about:blank` | `Bad Request` |
| type `https://example.com/probs/validation` and title `Your request is not valid.` | 422 | `https://example.com/probs/validation` | `Your request is not valid.` |

#### Scenario: Invalid status
- **When** the validation mapper is constructed with status `<status>`
- **Then** construction panics with a message naming the status option

##### Examples:
| status |
|--------|
| 200 |
| 500 |
| 0 |

### Requirement: Translated messages
Consumers SHALL be able to supply a function that picks a translator from the request context. When it returns a translator, each entry's `detail` SHALL be the translated message. When it is absent, returns nil or panics, the default message SHALL be used.

#### Scenario: Translator choices
- **Given** the validation mapper registered with a translator function that `<translator>`
- **When** field `age` fails `min` for a request
- **Then** the entry's detail is `<detail>`

##### Examples:
| translator | detail |
|------------|--------|
| returns an Indonesian translator with `min` registered | the Indonesian translation from that translator |
| returns an English translator with `min` registered | the English translation from that translator |
| returns nil | `failed validation: min` |
| panics | `failed validation: min` |
| (no translator function) | `failed validation: min` |

### Requirement: Invalid validator usage is a server error
`*validator.InvalidValidationError` (for example, validating nil or a non-struct) and nil errors SHALL NOT be matched, so they reach the core's safe 500 fallback.

#### Scenario: Degenerate inputs
- **Given** the validation mapper used directly
- **When** it maps `<error>`
- **Then** the result is `<result>`

##### Examples:
| error | result |
|-------|--------|
| `nil` | no match |
| `&validator.InvalidValidationError{}` from validating nil | no match |
| a typed-nil `*MyError` stored in an `error` | no match |

### Requirement: Validation problem renders in XML
The `errors` extension SHALL render in the XML format as an array of `i` elements.

#### Scenario: XML client
- **Given** the validation mapper registered and a request with `Accept: application/problem+xml`
- **When** validation fails on field `age`
- **Then** the response `Content-Type` is `application/problem+xml`
- **And** the body contains `<xml>`

##### Examples:
| xml |
|-----|
| `<errors><i><detail>failed validation: min</detail><pointer>#/age</pointer></i></errors>` |
