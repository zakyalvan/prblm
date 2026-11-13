# Design

## Context

Motivation is in proposal.md (Why). Behavior is in `specs/problem-types`, `specs/type-discovery`, `specs/gin-adapter` and `specs/fiber-adapter`.

What exists today:
- **Core:** `NewHandler(opts ...Option)` ignores nil options and never panics. `Resolve` copies the matched problem, then `complete` runs `normalize` → `finalize` (title) → `enrich`. `report` sends notes such as `ErrInvalidStatus` to the hook before the main call. `Respond` negotiates problem+json/xml through an unexported Accept parser.
- **`Problem.Type`** is stored as given. `WithType` documents it as a URI reference.
- **Adapters:** `ginprblm.Middleware(h)`, `ginprblm.Recovery(h)` and `fiberprblm.ErrorHandler(h)` take a `*prblm.Handler`. They panic on nil, and use only `h.Respond`.
- **`internal/paritytest`** builds one handler and three servers from it.
- **Release:** nothing is tagged. The release is slice 7 of `add-mapper-plugins`, which follows this change.

## Goals / Non-Goals

**Goals:**
- All type logic stays in the core. The adapters only route requests to it, so parity with net/http stays byte-exact.
- Type definitions are immutable values, built once at startup and safe to share across goroutines.
- Mistakes fail at startup (panics in constructors) or are reported to the hook at runtime. They never fail a response.

**Non-Goals:**
- `prblm.Group` route-group prefixes (postponed, see proposal).
- Custom documentation templates, localization, Markdown rendering of descriptions.
- Serving discovery from the net/http adapter automatically. net/http has no "unclaimed route" hook, so the consumer mounts it.
- Changing `WithType` for handlers without a namespace.

## Slices

1. **The adapters take options.**
   - At its end: `ginprblm.Middleware(opts...)`, `ginprblm.Recovery(opts...)` and `fiberprblm.ErrorHandler(opts...)` build their handler with `prblm.NewHandler(opts...)`. The parity test builds all three servers from one options slice. Behavior is otherwise unchanged.
   - Scenarios: gin-adapter / Options configure the middleware, Shared options across middleware and recovery; fiber-adapter / Options configure the error handler; both parity scenarios' existing rows.
   - Hot paths: the adapters' construction path (otherwise unchanged).
2. **Namespace resolution, end to end.**
   - At its end: `WithNamespace` validates at construction. Relative sub-types resolve during completion. Invalid ones become `about:blank` with an `ErrInvalidType` note. This works through net/http, gin and fiber with no adapter change.
   - Scenarios: problem-types / Type resolved, No namespace, Hostile host headers, Namespace validated, Invalid relative type, Faulty hook on a type note, Problem sources, Shared problem stays relative; the adapters' "Invalid options fail at construction" namespace rows; the parity `ErrBlacklisted` row.
   - Hot paths: `resolveType` (resolved, kept, invalid) inside `complete`.
3. **Type definitions.**
   - At its end: `DefineType`, `Type.New` and `WithTypes`, with every construction-time check. Mappers can return `t.New()`. Nothing is served yet.
   - Scenarios: problem-types / Problem from a definition, Each problem is a fresh copy, Definition validated, Registration validated; the adapters' `WithTypes` construction rows.
   - Hot paths: `Type.New`.
4. **Discovery in the core, mounted in net/http.**
   - At its end: `Handler.DiscoveryPath()` and `Handler.Discovery()` serve the HTML and JSON documentation, the index, 404s, 405s and the security headers. `WithoutDiscovery` turns it off, and the `ErrUndocumentedType` note works.
   - Scenarios: all of type-discovery; problem-types / Undocumented sub-type.
   - Hot paths: the discovery handler's routing, negotiation and rendering, and the undocumented-type check in `resolveType`.
5. **gin serves discovery.**
   - At its end: `Middleware` serves unclaimed GET/HEAD requests under the discovery path.
   - Scenarios: gin-adapter / Discovery through gin; the gin parity documentation rows.
   - Hot paths: the pre-`Next` discovery branch in `Middleware`.
6. **fiber serves discovery.**
   - At its end: `ErrorHandler` serves route-not-found GET/HEAD requests under the discovery path.
   - Scenarios: fiber-adapter / Discovery through fiber; the fiber parity documentation rows.
   - Hot paths: the discovery branch in `ErrorHandler`.
7. **Parity and documentation.**
   - At its end: the full parity tables pass, and the READMEs, godoc and Examples cover namespaces, definitions and discovery for all three frameworks.

Every slice ends releasable. Slice 2 is useful without definitions (hand-written sub-types). Slice 3 is useful without discovery (one source of truth for status and title). Slice 4 works on net/http before any adapter serves it.

## Decisions

### D1. Adapters build their handler from options

`Middleware(opts ...prblm.Option)` calls `prblm.NewHandler(opts...)` once, at construction, and captures the handler in the returned closure. `Recovery(opts...)` and `ErrorHandler(opts...)` do the same. `fiberprblm.Recovery()` is unchanged, because it renders nothing.

- *Alternatives:*
  - Keep `Middleware(h)` and add an options form. Rejected by the user: one way to configure.
  - A `prblm.WithHandler(h)` option. Rejected: an option that replaces the thing being configured is confusing.
- **Failure modes:**
  - **Recovery and Middleware built from the same options** build two handlers. Both run the same hook and mappers, so the outcome is identical. It costs only a second set of definitions in memory. Documented: pass the same slice.
  - **Invalid options:** `NewHandler` panics at construction (D2, D3), so the adapter constructor panics at startup, never inside a request.
  - **Nil options:** skipped by `NewHandler`, as today.

### D2. Namespace and type resolution

`WithNamespace(ns)` stores the raw string. `NewHandler` validates it after applying every option, so option order doesn't matter. Validation uses `net/url.Parse`:

| Form | Accepted when |
|---|---|
| absolute URI | scheme is `http` or `https`; host non-empty; no user info, query or fragment; path not empty and not `/` |
| absolute path | starts with `/` but not `//`; no query or fragment; not `/` |

Every path segment must be non-empty except a trailing one, contain no `.` or `..`, and need no escaping (so `/v1/api probs` is rejected). One trailing `/` is trimmed. Failure panics with `prblm: WithNamespace: <reason>: "<ns>"`.

`resolveType(ctx, p)` runs in `complete`, which becomes `normalize` → `finalize` → `enrich` → `resolveType` → `finalize`:
- **Last, after `enrich`,** because an enricher may set a type, and the spec resolves those too.
- **`finalize` runs a second time** because an invalid type may have just become `about:blank`. `finalize` only fills an empty `about:blank` title, so the second call is idempotent for every other problem.

Resolution rules, for a handler with a namespace:
- empty or `about:blank` → kept
- a value that parses with a scheme (`https:`, `urn:`) → kept
- a value starting with `/` → kept
- a valid sub-type → `ns + "/" + subtype`
- anything else → `p.Type = ""` and a note `fmt.Errorf("%w: %q", ErrInvalidType, given)` via `report`

Without a namespace, `resolveType` returns at once.

The sub-type grammar is shared with `DefineType`: segments joined by `/`, each matching `[a-z0-9]+([-_.][a-z0-9]+)*`. It is checked by a hand-written scanner, not `regexp`, to keep the hot path allocation-free.

- *Alternatives:*
  - Building absolute types from the request host. Rejected on security grounds (Host header injection), see the problem-types requirement on request hosts.
  - Resolving in `WithType` itself. Rejected: a `Field` has no handler, and package-level problems are built before any handler exists.
  - Leaving invalid relative types unchanged. Rejected: the client would resolve them against the request URL, which is meaningless.
- **Failure modes:**
  - **Shared package-level problem:** `resolveType` works on the copy made by `Resolve`. Covered by the "Shared problem stays relative" rows under `-race`.
  - **A hook that panics on the note:** `report` already recovers.
  - **An enricher sets an invalid type:** it is resolved after enrichers, so it gets the same fallback and note.
  - **The fallback and panic paths:** they go through `complete` as well, so an enricher that sets a sub-type on the 500 fallback is resolved too.

### D3. Type definitions

```go
type Type struct{ subtype, title, description string; status int }   // unexported fields: immutable
func DefineType(subtype string, status int, title, description string) *Type
func (t *Type) New(fields ...Field) *Problem   // New(t.status, WithType(t.subtype), WithTitle(t.title), fields...)
func (t *Type) Subtype() string                // read-only accessors for docs and tests
func (t *Type) Status() int
func (t *Type) Title() string
func (t *Type) Description() string
func WithTypes(types ...*Type) Option          // appends; nil skipped
func WithoutDiscovery() Option
```

`DefineType` panics on an invalid sub-type, a status outside 400–599, or a title that is empty after `strings.TrimSpace`. The message has the form `prblm: DefineType: <what>: <value>`. Definitions are meant for package-level `var`s, so a mistake surfaces when the program starts.

`NewHandler` builds `h.types map[string]*Type` after all options are applied:
- A duplicate sub-type panics (`prblm: WithTypes: duplicate sub-type "<s>"`).
- Any type without a namespace panics (`prblm: WithTypes: types need WithNamespace`).
- `WithTypes()` with no non-nil types registers nothing, which turns discovery off. It does not panic, even without a namespace.

- *Alternatives:*
  - Exported struct fields. Rejected: a definition changed after registration would desynchronize the docs.
  - A global registry filled by `DefineType`. Rejected: package state, and test isolation.
  - `Type` implementing `error`. Postponed: not needed by any scenario.
- **Failure modes:**
  - **Nil `*Type` passed to `WithTypes`:** skipped.
  - **Nil receiver on `New`:** `(*Type)(nil).New()` returns the safe 500 `about:blank` problem rather than panicking (problem-types / Nil definition).
  - **The same `*Type` registered twice:** a duplicate, so it panics. This is consistent and catches copy-paste mistakes.

### D4. Discovery handler

`DiscoveryPath()` returns the namespace path (the URI's path for an absolute namespace) when discovery is on, and `""` otherwise. `Discovery()` always returns a non-nil `http.Handler`:
- **Off:** every request gets a 404 problem.
- **Method:** not GET or HEAD gets a 405 problem with `Allow: GET, HEAD`.
- **Path:** the path is `r.URL.Path`. `EscapedPath` must equal the path, so encoded segments such as `%2e%2e` or `%00` fall to 404. Path equal to the discovery path, or that path plus `/`, gets the index. Otherwise the remainder after `path + "/"` is looked up exactly in `h.types`; no cleaning, so no traversal is possible.
- **Not found:** a 404 problem rendered through the handler's own negotiation and encoding (`respond` with a fresh `New(404)`). The hook is skipped, since there is no application error.

Representations:
- **Negotiation:** JSON when the Accept parser gives `application/json` a higher q than `text/html`, otherwise HTML. Malformed or unrelated Accept values give HTML. This is a small negotiator over the existing parser, separate from the problem negotiator.
- **JSON:** `encoding/json` with HTML escaping on (the default), fields `type`, `subtype`, `status`, `title`, `description`. The index is `{"types":[...]}` without descriptions, sorted by sub-type.
- **HTML:** one `html/template` per kind (doc, index), parsed once at package init. Inline CSS only.
- **Headers:** `X-Content-Type-Options: nosniff`, `Vary: Accept`, and for HTML `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'`.
- **Pre-rendering:** both representations of every page are rendered once, in `NewHandler`, into immutable byte slices. A request only negotiates and writes, which makes it concurrency-safe and fast. HEAD writes the same headers and no body.
- **Templates:** HTML uses the standard library's `html/template` only. The templates are embedded as string constants, parsed with `template.Must` at package init, and produce a complete, self-contained page (`<!DOCTYPE html>`, `<title>`, inline CSS, no external assets), so the CSP needs no exceptions beyond inline styles.
- **Caching, so browsers can reuse a page:** every 200 page carries `Cache-Control: public, max-age=3600` and a strong `ETag`. The `ETag` is the first 16 hex characters of the SHA-256 of the rendered bytes, quoted, and is computed once at construction next to the bytes.
  - **Each representation has its own `ETag`:** HTML and JSON differ, and `Vary: Accept` keeps them apart in shared caches.
  - **`If-None-Match`:** parsed as a comma-separated list. A tag matches by weak comparison, as RFC 9110 section 13.1.2 requires, and `*` matches any page. A match gives 304 with `ETag`, `Cache-Control` and `Vary`, and no body.
  - **Problem responses (404, 405)** get no caching headers.
  - **Why one hour:** definitions change only when the application is redeployed. One hour bounds how long a browser shows stale text after a deploy, and the `ETag` makes revalidation after expiry a cheap 304. A configurable max-age is left for later; nothing needs it yet.

The `ErrUndocumentedType` note is added to `resolveType`: when discovery is on and a valid sub-type resolves but isn't in `h.types`, `report` gets `fmt.Errorf("%w: %q", ErrUndocumentedType, subtype)`.

- *Alternatives:*
  - `Last-Modified` instead of `ETag`. Rejected: a definition has no meaningful modification time, and a build time would change on every deploy even when the text doesn't.
  - Rendering per request. Rejected: needless work and a template-error path on every request.
  - Serving Markdown. Rejected: needs a dependency or a renderer in the core.
  - Making `*Handler` itself an `http.Handler`. Rejected: ambiguous with error rendering.
- **Failure modes:**
  - **A template fails to render a definition:** this happens at construction and panics with the sub-type. Covered by the hostile-content rows, which must still render.
  - **A client disconnects mid-write:** the write error is ignored, as in the other writers.
  - **A malformed `If-None-Match`:** no tag matches, so the full 200 is sent (type-discovery / Conditional requests, `;;;garbage` row).
  - **A browser keeps a stale page after a deploy:** bounded by `max-age=3600`. After that, the changed `ETag` forces a full 200 (type-discovery / ETag changes with the definition).
  - **A shared cache mixes HTML and JSON:** prevented by `Vary: Accept` and per-representation `ETag`s.
  - **An absolute namespace on a host that doesn't reach the app:** discovery is served, but at the wrong host. Documented: the operator routes it.

### D5. gin serving

In `Middleware`, before `c.Next()`, the middleware serves discovery directly through the core `http.Handler` and calls `c.Abort()` when all of these hold:
- discovery is on
- the method is GET or HEAD
- `c.FullPath() == ""` (no route matched)
- the path is the discovery path or under it

**Unverified gin behavior.** This relies on two claims from reading gin's source:
1. Middleware added with `engine.Use` runs for unmatched paths, through `allNoRoute`.
2. `c.FullPath()` is empty there.

Under CLAUDE.md rule 8, the first gin task proves both with tests before building on them. If either fails, slice 5 is re-planned: the fallback is `engine.NoRoute(ginprblm.Discovery(opts...))`, which would need a spec change.

The discovery response goes through `deferredWriter`. A 200 commits on its body write. A 304, or any HEAD response, has no body, so the branch commits the recorded status explicitly (`commitNow`) before returning. Otherwise the status would only be recorded (gin-adapter / Discovery through gin, 304 and HEAD rows).

- **Failure modes:**
  - **Middleware only on a group:** it never sees unmatched paths, so discovery isn't served. Specified, and documented.
  - **A consumer `NoRoute` handler:** gin runs global middleware first, so discovery wins under the namespace path, and the consumer's `NoRoute` still handles every other path (gin-adapter / Discovery through gin, `NoRoute` rows).
  - **A route at a documentation path:** `FullPath` is non-empty, so the route wins.

### D6. fiber serving

In `ErrorHandler`, before resolving, the handler serves discovery through the core `http.Handler` when all of these hold:
- discovery is on
- the method is GET or HEAD
- the error is fiber's route-not-found
- the path is the discovery path or under it

fiber has no `http.ResponseWriter`, so the core handler writes into a minimal unexported in-package writer that records status, headers and body. `ErrorHandler` copies that result to `c` with `c.Status`, `c.Set` and `c.Send`. This avoids the `adaptor` middleware and its `net/http` conversion of the whole request.

**Unverified fiber behavior.** Telling a router 404 apart from a handler that returns `fiber.ErrNotFound` depends on fiber v3's `c.Route()` on unmatched requests. Under rule 8, the first fiber task proves the distinguishing signal with a test (the "returning `fiber.ErrNotFound`" spec row). If no reliable signal exists, the fallback is to compare the error by identity with fiber's routing error *and* require that no route path matches. The spec row stays the acceptance criterion either way.

- **Failure modes:**
  - **A streamed body:** impossible on route-not-found.
  - **`WithoutDiscovery`:** `DiscoveryPath()` is `""`, so the branch is skipped.
  - **The recorder:** pre-rendered bodies are small and fixed, so there's no size concern.

### D7. Parity

`internal/paritytest` builds all three servers from one `[]prblm.Option`. net/http mounts `Discovery()` at `"GET " + DiscoveryPath() + "/"` on the mux next to the routes. Rows follow both parity scenarios. Only status, `Content-Type`, `Vary` and the body are compared (as today).

## Defaults

| Behavior | Default | Why | Opt-in |
|---|---|---|---|
| Namespace | none: types passed through unchanged | no behavior change for existing users | `WithNamespace` |
| Relative type with a namespace | resolved, or `about:blank` plus a note when invalid | a broken link is worse than none | — |
| Type definitions | none | — | `WithTypes` |
| Discovery | off without types; on with types | registering types is the explicit intent to document them | `WithoutDiscovery` to keep definitions but serve nothing |
| Discovery format | HTML unless JSON is preferred | RFC 9457 asks for human-readable docs | `Accept: application/json` |
| Discovery caching | `Cache-Control: public, max-age=3600`, strong `ETag`, 304 on match | browsers reuse pages, and definitions only change per deploy | — (configurable max-age postponed) |
| Host-derived types | never | Host header injection | — |

## Hot Paths

- **`complete`:** `resolveType` (no namespace / kept / resolved / invalid / undocumented) and the second `finalize`.
- **Sub-type scanner:** valid, and every invalid character class.
- **`NewHandler`:** namespace validation and the types map (once per handler, but every branch is a startup failure path).
- **`Type.New`:** fields, nil fields, nil receiver.
- **Discovery handler:** off, method, index, doc, unknown, encoded path, negotiation (HTML / JSON / malformed), HEAD, and `If-None-Match` (match, weak match, `*`, list, no match, malformed).
- **gin `Middleware`:** discovery branch (served, route wins, wrong method, outside the path, off).
- **fiber `ErrorHandler`:** discovery branch (served, handler-returned not-found, wrong method, outside the path, off).

## Risks / Trade-offs

- [Path namespaces make the type string host-dependent (staging vs production)] → Documented. Consumers who need stable identifiers use an absolute namespace.
- [gin's `allNoRoute`/`FullPath` behavior differs from the source reading] → Rule 8 proof first (D5), with a specified fallback.
- [fiber can't tell a router 404 from a handler-returned `ErrNotFound`] → Rule 8 proof first (D6). The spec row is the acceptance criterion.
- [Two handlers per app when Recovery and Middleware share options] → Negligible memory: definitions are pointers and pages are pre-rendered per handler. Documented.
- [`NewHandler` can now panic] → Only for the new options, with messages naming the option. Called out as a compatibility decision in the proposal.

## Migration Plan

Nothing is released, so no consumer has to migrate. In this repository:
- Update the adapter tests, examples, READMEs and `internal/paritytest` to the options form.
- `add-mapper-plugins` keeps its release slice and runs after this change. Its tasks don't reference the adapter constructors.
