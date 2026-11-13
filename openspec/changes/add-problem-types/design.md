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

8. **Review fixes.**
   - At its end: the findings of `/opsx:verify` and the PR #3 code review are fixed in the working system, one behavior at a time. Every earlier scenario still passes.
   - Scenarios:
     - problem-types: the new rows of Namespace validated, No namespace, Invalid relative type, Nil definition and Registration validated; the new scenario Nil definition accessors.
     - type-discovery: the new rows of Unknown type; the new requirement "Discovery responses for adapters".
     - type-discovery: the new requirement "Shadowed documentation is noted".
     - gin-adapter and fiber-adapter: the method-not-allowed rows of Discovery through gin and Discovery through fiber; the gin HEAD row and the fiber HEAD row; the new requirements "Middleware reports shadowed documentation" (gin) and "Discovery middleware reports shadowed documentation" (fiber), and the fiber discovery middleware's construction row.
   - Hot paths: `resolveType` (no-namespace note, title reset, note built only with a hook), `RespondDiscovery`, `NoteShadowedRoute`, the gin and fiber discovery branches, the fiber discovery middleware, the `xmlenc` decode guard.

Every slice ends releasable. Slice 2 is useful without definitions (hand-written sub-types). Slice 3 is useful without discovery (one source of truth for status and title). Slice 4 works on net/http before any adapter serves it.

## Decisions

### D1. Adapters build their handler from options

`Middleware(opts ...prblm.Option)` calls `prblm.NewHandler(opts...)` once, at construction, and captures the handler in the returned closure. `Recovery(opts...)` and `ErrorHandler(opts...)` do the same. `fiberprblm.Recovery()` is unchanged, because it renders nothing.

- *Alternatives:*
  - Keep `Middleware(h)` and add an options form. Rejected by the user: one way to configure.
  - A `prblm.WithHandler(h)` option. Rejected: an option that replaces the thing being configured is confusing.
- **Failure modes:**
  - **Recovery and Middleware built from the same options** build two handlers. Both run the same hook and mappers, so the outcome is identical. Documentation pages are rendered on first use (D4), and `Recovery` never uses discovery, so its handler holds only the definitions, never rendered pages. Documented: pass the same slice.
  - **Invalid options:** `NewHandler` panics at construction (D2, D3), so the adapter constructor panics at startup, never inside a request.
  - **Nil options:** skipped by `NewHandler`, as today.

### D2. Namespace and type resolution

`WithNamespace(ns)` stores the raw string, and a later `WithNamespace` replaces an earlier one. `NewHandler` validates only the final value, after applying every option, so an invalid namespace replaced by a valid one is accepted, and a valid one replaced by an invalid one panics (problem-types / Namespace validated, the option-order rows). The option itself never validates or panics. Validation uses `net/url.Parse`:

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
- anything else → `p.Type = ""`, `p.Title = ""` (so the second `finalize` sets the status phrase, as RFC 9457 section 4.2.1 asks for `about:blank`), and a note `fmt.Errorf("%w: %q", ErrInvalidType, given)`

Without a namespace, every type is kept. A relative type (not empty, no scheme, not starting with `/`) also gets the `ErrInvalidType` note: the client resolves it against the request URL, which leads nowhere, but RFC 9457 allows relative references and passing them through is the existing behavior, so the output doesn't change.

**Notes are built only when a hook is set.** `resolveType` checks `h.onError != nil` before formatting any note (`ErrInvalidType`, `ErrUndocumentedType`), so resolution allocates nothing without a hook. A test with `testing.AllocsPerRun` pins this.

**Resolved types are not rebuilt per request.** Resolving a sub-type would otherwise allocate `ns + "/" + subtype` on every problem (decided during task 8.7, after measuring that this concatenation, not the note, was the remaining allocation):
- `NewHandler` resolves every registered sub-type once, into an immutable map. It is also the eager set of documented sub-types for the `ErrUndocumentedType` check (D4).
- Other valid sub-types go into a per-handler cache, so only the first resolution of each allocates. Reads load an immutable map through an `atomic.Pointer` and allocate nothing. A writer copies the map under a mutex and publishes the copy. `sync.Map` was rejected because boxing the string key into an interface allocates on every lookup.
- The cache is bounded: at most 256 sub-types of at most 128 bytes. Past the bound, a sub-type is resolved by concatenation and not kept.

The sub-type grammar is shared with `DefineType`: segments joined by `/`, each matching `[a-z0-9]+([-_.][a-z0-9]+)*`. It is checked by a hand-written scanner, not `regexp`, to keep the hot path allocation-free.

- *Alternatives:*
  - Building absolute types from the request host. Rejected on security grounds (Host header injection), see the problem-types requirement on request hosts.
  - Resolving in `WithType` itself. Rejected: a `Field` has no handler, and package-level problems are built before any handler exists.
  - Leaving invalid relative types unchanged. Rejected: the client would resolve them against the request URL, which is meaningless.
- **Failure modes:**
  - **Shared package-level problem:** `resolveType` works on the copy made by `Resolve`. Covered by the "Shared problem stays relative" rows under `-race`.
  - **A hook that panics on the note:** `report` already recovers.
  - **The title of an invalid type:** a custom title written for that type would now describe `about:blank`, so it is replaced by the status phrase. Status, detail, instance and extensions are kept.
  - **A consumer puts request data into a type:** every distinct valid sub-type would be cached, so the cache stops growing at its bound and later sub-types are concatenated per request. Covered by the bounded-cache and concurrent-cache tests under `-race`.
  - **Several `WithNamespace` options (several modules each adding one):** the last wins, as for other single-valued options. A typo in an earlier one is silently replaced, which is the cost of order-independent validation.
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

`DefineType` panics on an invalid sub-type, a status outside 400–599, or a title that is empty after `strings.TrimSpace`. The checks live in one unexported `validateType(subtype, status, title) error`, shared with `WithTypes`. The message has the form `prblm: DefineType: <what>: <value>`. Definitions are meant for package-level `var`s, so a mistake surfaces when the program starts.

`NewHandler` builds `h.types map[string]*Type` after all options are applied:
- A duplicate sub-type panics (`prblm: WithTypes: duplicate sub-type "<s>"`).
- Any type without a namespace panics (`prblm: WithTypes: types need WithNamespace`).
- Every definition is re-validated with `validateType`, and an invalid one panics (`prblm: WithTypes: invalid definition: <reason>`). `Type` is an exported struct, so `&prblm.Type{}` can be built without `DefineType`; it would otherwise be documented with status 0, a blank title and a link to the index.
- `WithTypes()` with no non-nil types registers nothing, which turns discovery off. It does not panic, even without a namespace.

- *Alternatives:*
  - Exported struct fields. Rejected: a definition changed after registration would desynchronize the docs.
  - A global registry filled by `DefineType`. Rejected: package state, and test isolation.
  - `Type` implementing `error`. Postponed: not needed by any scenario.
- **Failure modes:**
  - **Nil `*Type` passed to `WithTypes`:** skipped.
  - **Nil receiver on `New`:** `(*Type)(nil).New()` returns the safe 500 `about:blank` problem rather than panicking (problem-types / Nil definition).
  - **Zero-value definition:** `(&prblm.Type{}).New()` is treated like nil (an empty sub-type marks it), so it also returns the safe 500 and ignores fields.
  - **Nil receiver on the accessors:** `Subtype`, `Status`, `Title` and `Description` return zero values on a nil `*Type` instead of panicking inside a request (problem-types / Nil definition accessors).
  - **The same `*Type` registered twice:** a duplicate, so it panics. This is consistent and catches copy-paste mistakes.

### D4. Discovery handler

`DiscoveryPath()` returns the namespace path (the URI's path for an absolute namespace) when discovery is on, and `""` otherwise.

**One entry point for every adapter:**

```go
type DiscoveryRequest struct {
	Method      string // request method
	Path        string // path as the client sent it, still escaped
	Accept      string // Accept header, repeated lines joined with ","
	IfNoneMatch string // If-None-Match header, repeated lines joined with ","
}

func (h *Handler) RespondDiscovery(ctx context.Context, req DiscoveryRequest) (Response, bool)
```

- It returns `false` when the request is not for discovery: discovery is off, the method is not GET or HEAD, or `Path` is neither the discovery path, that path plus `/`, nor under `path + "/"`. The adapter then handles the request as usual.
- Otherwise it returns `true` with the page, its 304, or a 404 problem for an unknown sub-type. For HEAD the body is empty and the headers are those of the GET.
- **Path:** `Path` is the escaped form (`r.URL.EscapedPath()` in net/http and gin, the raw request path in fiber). A valid namespace and a valid sub-type need no escaping, so any `%` in the remainder gives a 404, which covers `%2e%2e` and `%00`. The remainder after `path + "/"` is looked up exactly in the definitions, without cleaning, so no traversal is possible. The 404 problem's `instance` is that path.
- gin and fiber call it directly. fiber needs no `http.Request` and no recording writer, and the method and path rule is written once.

`Discovery()` wraps it and always returns a non-nil `http.Handler`:
- **Method:** not GET or HEAD gets a 405 problem with `Allow: GET, HEAD`.
- **Not served (`false`):** off, or outside the path, gets a 404 problem.
- **Otherwise:** the response from `RespondDiscovery`.

**Discovery problems are silent.** Discovery's 404 and 405 problems are completed and rendered like any other problem, but the hook is never called for them, not even for a note from a panicking enricher or a failed encoding, since no application error occurred. `report` honors an unexported context marker that the discovery path sets.

Representations:
- **Negotiation:** JSON when the Accept parser gives `application/json` a higher q than `text/html`, otherwise HTML. Malformed or unrelated Accept values give HTML. This is a small negotiator over the existing parser, separate from the problem negotiator.
- **JSON:** `encoding/json` with HTML escaping on (the default), fields `type`, `subtype`, `status`, `title`, `description`. The index is `{"types":[...]}` without descriptions, sorted by sub-type.
- **HTML:** one `html/template` per kind (doc, index), parsed once at package init. Inline CSS only.
- **Headers:** `X-Content-Type-Options: nosniff`, `Vary: Accept`, and for HTML `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'`.
- **Pre-rendering:** both representations of every page are rendered once, on first use, into immutable byte slices. A `sync.Once` runs the rendering the first time `DiscoveryPath`, `Discovery` or `RespondDiscovery` is called. Every adapter calls `DiscoveryPath` at construction, and net/http code calls it to mount discovery, so a render failure still surfaces at startup. A handler that never uses discovery (gin's `Recovery`) never renders. The set of documented sub-types, which the undocumented-type note needs, is built eagerly in `NewHandler` and costs one map. A request only negotiates and writes, which makes it concurrency-safe and fast. HEAD writes the same headers and no body.
- **Templates:** HTML uses the standard library's `html/template` only. The templates are embedded as string constants, parsed with `template.Must` at package init, and produce a complete, self-contained page (`<!DOCTYPE html>`, `<title>`, inline CSS, no external assets), so the CSP needs no exceptions beyond inline styles.
- **Caching, so browsers can reuse a page:** every 200 page carries `Cache-Control: public, max-age=3600` and a strong `ETag`. The `ETag` is the first 16 hex characters of the SHA-256 of the rendered bytes, quoted, and is computed once, next to the bytes, when the page is rendered on first use.
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
  - **A template fails to render a definition:** this happens on first use, which is construction for every adapter, and panics with the sub-type. Covered by the hostile-content rows, which must still render.
  - **A client disconnects mid-write:** the write error is ignored, as in the other writers.
  - **A malformed `If-None-Match`:** no tag matches, so the full 200 is sent (type-discovery / Conditional requests, `;;;garbage` row).
  - **A browser keeps a stale page after a deploy:** bounded by `max-age=3600`. After that, the changed `ETag` forces a full 200 (type-discovery / ETag changes with the definition).
  - **A shared cache mixes HTML and JSON:** prevented by `Vary: Accept` and per-representation `ETag`s.
  - **An absolute namespace on a host that doesn't reach the app:** discovery is served, but at the wrong host. Documented: the operator routes it.

**Shadowed documentation (from the `/opsx:verify` and code review findings).** An application route registered inside the namespace answers requests that discovery would otherwise answer, so the documentation at that path cannot be reached; in gin a HEAD there even gets the documentation while GET gets the route (D5). The library never logs, so the conflict goes to the hook:

```go
var ErrShadowedDocumentation = errors.New("prblm: an application route hides type documentation")

func (h *Handler) NoteShadowedRoute(ctx context.Context, route string, req DiscoveryRequest)
```

- An adapter calls it for every request a route answers, with the route's pattern (gin `c.FullPath()`, fiber `c.Route().Path`).
- It does nothing when discovery is off, the method is not GET or HEAD, the path is outside the discovery path, or discovery would answer the path with a 404 (an unknown sub-type is not hidden). Otherwise it reports `fmt.Errorf("%w: route %q hides %q", ErrShadowedDocumentation, route, path)` with a nil problem.
- **No rendering:** whether discovery would serve a page is answered from the registered sub-types `NewHandler` builds eagerly, not from the rendered pages, so an adapter that only notes (fiber's `Discovery` middleware, which builds its own handler) renders no second copy of the documentation (`TestNoteShadowedRoute_DoesNotRender`; found by `/simplify` during task 8.19).
- **Once per route:** a mutex-guarded set of noted routes, capped at 256 entries. It is only touched on a conflict, so other requests pay the on/off, method and prefix checks only. Route patterns come from code, so the cap only guards against misuse.
- The note is built only when a hook is set (`isReporting`), like every other note.
- **The hook contract changes:** this note arrives on a request that succeeded, with no answered error after it. The `OnError` godoc says so.
- *Alternatives:*
  - Scanning the routes at startup. Rejected: gin middleware cannot reach the engine (`gin.Context` exposes no engine or route list), and fiber's `ErrorHandler` has no app at construction.
  - Logging a warning. Rejected: the library never logs; the hook is its logging channel.
  - A note on every request. Rejected: one conflict would flood the log.
- **net/http:** a more specific mux pattern wins over the mounted `Discovery()`, with no hook point in between, so the conflict is documented only.
- **Failure modes:** a panicking hook is contained by `callHook`; concurrent first requests to the same route note it once (the set is checked and filled under the mutex); more than 256 conflicting routes are noted up to the cap, then silently.

### D5. gin serving

In `Middleware`, before `c.Next()`, when `c.FullPath() == ""` and gin's recorded status is 404 (`c.Writer.Status() == http.StatusNotFound`), the middleware calls `RespondDiscovery` with the escaped path. When it returns `true`, the middleware sends the response and calls `c.Abort()`. The method, path and on/off checks are the core's.

**The 404 condition (code review finding, PR #3).** With `engine.HandleMethodNotAllowed = true`, a request whose path has a route only for another method also runs global middleware with an empty `FullPath()`. Checking `FullPath()` alone served documentation instead of gin's 405. gin's `serveError` records the status (404 for no route, 405 for no method) before running the handlers, so the recorded status tells the two apart.
- **Unverified, rule 8:** this relies on reading gin's source. Task 8.1 proves it (`TestGinNoMethod_RecordsStatus`) before the fix is built. If the recorded status is not reliable, the fallback is to document that discovery paths must not share a path with routes of other methods while `HandleMethodNotAllowed` is on, and to re-plan the gin rows.
- **Verified (task 8.1, gin v1.12.0):** `TestGinNoMethod_RecordsStatus` in `ginprblm/noroute_test.go` passes. Global middleware runs with an empty `FullPath()` in both cases, and `c.Writer.Status()` is already 405 for a GET or HEAD whose path has only a POST route (at a sub-path and at the namespace root) when `HandleMethodNotAllowed` is on, and 404 for a path without routes, or with `HandleMethodNotAllowed` off. The fallback is not needed.

**Unverified gin behavior.** This relies on two claims from reading gin's source:
1. Middleware added with `engine.Use` runs for unmatched paths, through `allNoRoute`.
2. `c.FullPath()` is empty there.

Under CLAUDE.md rule 8, the first gin task proves both with tests before building on them. If either fails, slice 5 is re-planned: the fallback is `engine.NoRoute(ginprblm.Discovery(opts...))`, which would need a spec change.

The discovery branch runs before `deferredWriter` is installed: that writer exists so a problem can replace the handlers' response, which a discovery response never needs. It writes to gin's own writer and calls `c.Writer.WriteHeaderNow()`, because a 304 or a HEAD response has no body and its status would otherwise only be recorded (gin-adapter / Discovery through gin, 304 and HEAD rows).

**HEAD on a path with a route only for GET (found by `/opsx:verify` and the code review).** gin routes HEAD on its own: with `GET /v1/api/probs/out-of-credit` registered, a HEAD there finds no route, `FullPath()` is empty and gin records 404, so the middleware serves the documentation's headers while GET gets the route. Without the middleware, gin answers that HEAD with 404, so no response the application served is replaced. Decided with the user: specify it (gin-adapter / Discovery through gin, the HEAD row) and make the conflict visible through the shadowed-documentation note (D4), which `Middleware` reports for every request a route answers (`c.FullPath()` non-empty). Deferring to the GET route is not possible: middleware cannot see gin's routes.
- **Verified (task 8.18):** the row "head beside a get route" of `TestMiddleware_Discovery` passed before any change (status 200, `application/json`, empty body); it stays as a regression row.

**Verified (task 5.1, gin v1.12.0):** `TestGinNoRoute_RunsGlobalMiddleware` confirms both claims, with no `NoRoute` handler and with one registered before or after `Use`. The fallback was not needed.

- **Failure modes:**
  - **Middleware only on a group:** it never sees unmatched paths, so discovery isn't served. Specified, and documented.
  - **A consumer `NoRoute` handler:** gin runs global middleware first, so discovery wins under the namespace path, and the consumer's `NoRoute` still handles every other path (gin-adapter / Discovery through gin, `NoRoute` rows).
  - **A route at a documentation path:** `FullPath` is non-empty, so the route wins, and the hook gets the shadowed-documentation note once.
  - **An earlier global middleware sets a status (code review):** `c.Status(...)` before `Middleware` runs would change the recorded 404, so discovery would not be served. Task 8.18 proves or drops the claim; if it holds, the ordering rule (install `Middleware` first) is documented.
    - **Confirmed (task 8.18, gin v1.12.0):** the row "status set before the middleware" of `TestMiddleware_Discovery` first asserted the documentation and failed with `response 200 "" , want the JSON documentation of out-of-credit`. The ordering rule is documented in the `Middleware` godoc, the README gin section and the framework-differences "Ordering rule" row; the row now pins the documented behavior (200, empty body), next to the row "status set after the middleware", which is served.

### D6. fiber serving

In `ErrorHandler`, before resolving, the handler serves discovery through `RespondDiscovery` when all of these hold:
- discovery is on
- the method is GET or HEAD
- the error is fiber's route-not-found
- the path is the discovery path or under it

`ErrorHandler` calls `RespondDiscovery` with the raw request path and the `Accept` and `If-None-Match` headers, and sends a `true` result through the adapter's existing `send`, the same path a problem response takes. No `http.Request` is built and no response is recorded, so a request URI net/http cannot parse (a malformed escape such as `%zz`) needs no special case: it contains `%`, so it gets the discovery 404 problem when under the namespace path.

A path that has a route only for another method gets fiber's `ErrMethodNotAllowed`, not `ErrNotFound`, so it is not served (fiber-adapter / Discovery through fiber, the method-not-allowed row).

**HEAD (verified during the review, fiber v3.7.0):** `app.Get` registers HEAD as well (`app.GetRoutes()` lists both), so a GET route answers HEAD with its own headers and discovery is not involved (fiber-adapter / Discovery through fiber, the HEAD row).

**Discovery middleware.** `ErrorHandler` only sees requests that end in an error, so it cannot notice a route that hides documentation. `fiberprblm.Discovery(opts...)`, installed with `app.Use`, builds its handler from the same options (like gin's `Recovery`, pass the same slice), calls `c.Next()`, and then, when `c.Matched()`, reports `c.Route().Path` to `NoteShadowedRoute`. It returns the error from `c.Next()` unchanged and writes nothing, so serving stays in `ErrorHandler`.
- **Unverified, rule 8:** this relies on `c.Matched()` and `c.Route()` describing the endpoint route once `c.Next()` returns in an `app.Use` middleware. Task 8.16 proves it before the middleware is built; if it does not hold, stop and re-plan.
- **Verified (task 8.16, fiber v3.7.0):** `TestFiberMiddleware_SeesRoute` in `fiberprblm/notfound_test.go` passes. After `c.Next()` returns in an `app.Use` middleware, `c.Matched()` is true and `c.Route().Path` is the pattern (`/v1/api/probs/:name`) for a GET and for the HEAD fiber registers with it, and `c.Matched()` is false for an unmatched path. No re-plan is needed.
- **Failure modes:** with discovery off, the middleware only calls `c.Next()`; invalid options panic at construction; a panic below it is left to `Recovery`.

**Verified (task 6.1, fiber v3.7.0):** the router returns the sentinel `fiber.ErrNotFound` itself, so comparing the error by identity cannot tell a router 404 from a handler returning `fiber.ErrNotFound`. The signal is `c.Matched()`, which fiber sets only when an endpoint route (not `app.Use` middleware) matched; `TestFiberNotFound_Distinguishable` proves it with and without middleware. The condition is "the error is `fiber.ErrNotFound` and `!c.Matched()`".

- **Failure modes:**
  - **A streamed body:** impossible on route-not-found.
  - **`WithoutDiscovery`:** `DiscoveryPath()` is `""`, so the branch is skipped.
  - **A malformed escape in the path:** handled by the core's `%` rule, with no conversion error to handle.

### D7. Parity

`internal/paritytest` builds all three servers from one `[]prblm.Option`. net/http mounts `Discovery()` at `"GET " + DiscoveryPath() + "/"` on the mux next to the routes. Rows follow both parity scenarios. Only status, `Content-Type`, `Vary` and the body are compared (as today).

## Defaults

| Behavior | Default | Why | Opt-in |
|---|---|---|---|
| Namespace | none: types passed through unchanged, with an `ErrInvalidType` note for a relative type | no output change for existing users, but a broken relative link is made visible | `WithNamespace` |
| Relative type with a namespace | resolved, or `about:blank` with the status phrase as title plus a note when invalid | a broken link is worse than none, and an `about:blank` title should be the status phrase (RFC 9457 section 4.2.1) | — |
| Type definitions | none | — | `WithTypes` |
| Discovery | off without types; on with types | registering types is the explicit intent to document them | `WithoutDiscovery` to keep definitions but serve nothing |
| Discovery format | HTML unless JSON is preferred | RFC 9457 asks for human-readable docs | `Accept: application/json` |
| Discovery caching | `Cache-Control: public, max-age=3600`, strong `ETag`, 304 on match | browsers reuse pages, and definitions only change per deploy | — (configurable max-age postponed) |
| Host-derived types | never | Host header injection | — |
| Application route hiding documentation | served by the route; a note to the hook, once per route | the conflict is visible without the library logging | fiber: `fiberprblm.Discovery` middleware |

## Hot Paths

- **`complete`:** `resolveType` (no namespace with and without a relative type / kept / resolved from the registered map / resolved through the cache / invalid with the title reset / undocumented, each note with and without a hook) and the second `finalize`.
- **Type cache:** hit, miss, too long to keep, full, and concurrent writers.
- **Sub-type scanner:** valid, and every invalid character class.
- **`NewHandler`:** namespace validation and the types map (once per handler, but every branch is a startup failure path).
- **`Type.New`:** fields, nil fields, nil receiver, zero value. The accessors on a nil receiver.
- **`RespondDiscovery`:** off, method, outside the path, index, doc, unknown, `%` in the path, negotiation (HTML / JSON / malformed), HEAD, `If-None-Match` (match, weak match, `*`, list, no match, malformed), and rendering on first use under concurrency.
- **`Discovery()`:** the 405, the 404 for a `false` result, and the silent hook.
- **`xmlenc.Encode`:** the decode guard, extracted as `decodeTree` and tested with malformed input.
- **gin `Middleware`:** discovery branch (served, route wins, recorded 405 under `HandleMethodNotAllowed`, wrong method, outside the path, off).
- **fiber `ErrorHandler`:** discovery branch (served, handler-returned not-found, method not allowed, wrong method, outside the path, off).
- **`NoteShadowedRoute`:** off, wrong method, outside the path, unknown sub-type, page, index, once per route, the cap, no hook, a panicking hook, concurrent first requests.
- **gin `Middleware`:** the shadowed-documentation report for a matched route.
- **fiber `Discovery` middleware:** matched route reported, unmatched passed through, error returned unchanged, off.

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
