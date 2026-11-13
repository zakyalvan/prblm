package prblm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
)

// ErrInvalidStatus is wrapped by the note the [OnError] hook receives when a
// resolved problem had a status outside 400–599 and 500 was sent instead.
// It is a mistake in a mapper or describer, so a hook can tell it apart
// from the error being answered with errors.Is.
var ErrInvalidStatus = errors.New("prblm: problem status is not an error status")

// Handler resolves errors into problems. Create it with [NewHandler].
type Handler struct {
	mappers []Mapper
	// builtins are the built-in mappers, removed by WithoutBuiltins.
	builtins []Mapper
	// enrichers run on every resolved problem, after defaultInstance.
	enrichers []func(ctx context.Context, p *Problem)
	// onError is the hook set with OnError, or nil.
	onError func(ctx context.Context, err error, p *Problem)
	// namespace is the namespace set with WithNamespace: as given until
	// NewHandler checks it, then without its trailing slash, or "" for none.
	namespace string
	// hasNamespace is set by WithNamespace, so that NewHandler checks the
	// final namespace once, even an empty one.
	hasNamespace bool
	// registered maps the sub-type of every registered definition to its
	// resolved type, built once in NewHandler, so resolving a documented
	// sub-type allocates nothing. It is also the set of documented
	// sub-types the undocumented-type note checks.
	registered map[string]string
	// unregistered keeps the resolved types of other sub-types, bounded.
	unregistered typeCache
	// definitions are the non-nil types passed to WithTypes, in order.
	definitions []*Type
	// discovery is the rendered documentation of the definitions, or nil
	// when discovery is off.
	discovery *discovery
	// isDiscoveryDisabled is set by WithoutDiscovery.
	isDiscoveryDisabled bool
}

// NewHandler returns a handler configured by opts. Nil options are ignored.
// With no options it is ready to use: built-in mappers on, the instance set
// to the request path, and a safe 500 for anything unrecognized. The handler
// does not change after NewHandler returns and is safe for concurrent use.
//
// NewHandler panics when an option is invalid: a malformed [WithNamespace],
// or [WithTypes] with an invalid definition, a duplicate sub-type or without
// a namespace. That is a
// startup misconfiguration, so it fails when the program starts rather than
// inside a request. The checks run after all options are applied, so their
// order does not matter, except that the last WithNamespace replaces earlier
// ones and is the only one checked.
func NewHandler(opts ...Option) *Handler {
	h := &Handler{builtins: builtins}

	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}

	if h.hasNamespace {
		h.namespace = mustParseNamespace(h.namespace)
	}

	h.registered = h.checkTypes()
	h.discovery = h.newDiscovery()

	return h
}

// Resolving errors: the steps that turn an error into a problem, in the order
// they run.

// Resolve turns err into a problem, or returns nil for a nil err.
//
// The steps run in this order, and the first one that recognizes err wins:
//
//  1. the outermost *Problem or [Describer] in the chain of err
//  2. the mappers scoped to ctx with [ScopeMappers], newest scope first
//  3. the handler's own mappers, in registration order
//  4. the built-in mappers: the sentinel errors such as [ErrNotFound], then
//     request body and deadline errors (see [WithoutBuiltins])
//  5. a status hint attached with [WithStatus]
//  6. the fallback: a 500 problem that carries none of the error's text
//
// The result is always a new problem that the caller may modify: a matched
// problem is copied, so problems shared between requests (for example
// package-level values) are never changed. A status outside 400–599 is
// replaced by 500, because a problem response must be an error; unless the
// status was missing, the mistake is reported to the [OnError] hook. An
// about:blank problem without a title gets the HTTP reason phrase of its
// status. Then the enrichers run (see [WithEnrichers]), and last the type is
// resolved against the namespace (see [WithNamespace]), so a type an
// enricher sets is resolved too. A type that becomes about:blank loses its
// title, which then becomes the reason phrase as well.
func (h *Handler) Resolve(ctx context.Context, err error) *Problem {
	if err == nil {
		return nil
	}

	p := h.match(ctx, err)

	// A matched problem may be shared between requests, so it is copied; the
	// fallback is built fresh and needs no copy.
	var resolved *Problem
	if p != nil {
		resolved = p.clone()
	} else {
		resolved = fallbackProblem()
	}

	h.complete(ctx, resolved)

	return resolved
}

// match runs the resolution steps in order and returns the problem of the
// first one that recognizes err, or nil when none does.
func (h *Handler) match(ctx context.Context, err error) *Problem {
	if p := safeCall(ctx, h, func() *Problem { return selfDescribed(err) }); p != nil {
		return p
	}

	if p := h.firstMatch(ctx, scopedMappers(ctx), err); p != nil {
		return p
	}

	if p := h.firstMatch(ctx, h.mappers, err); p != nil {
		return p
	}

	if p := h.firstMatch(ctx, h.builtins, err); p != nil {
		return p
	}

	return hinted(err)
}

// firstMatch returns the problem from the first mapper that recognizes err,
// or nil when none does. Nil mappers are skipped, wherever they were
// registered, and a mapper that panics counts as no match.
func (h *Handler) firstMatch(ctx context.Context, mappers []Mapper, err error) *Problem {
	for _, m := range mappers {
		if isNilMapper(m) {
			continue
		}

		if p := safeCall(ctx, h, func() *Problem { return m.MapError(ctx, err) }); p != nil {
			return p
		}
	}

	return nil
}

// complete applies the steps every problem goes through after it is
// matched: status normalization, title finalization, the enrichers and type
// resolution. Types are resolved after the enrichers, so a type an enricher
// sets is resolved too, and the title is finalized again at the end, for a
// problem the enrichers or type resolution left as about:blank without one.
// Enrichers see the title already finalized.
func (h *Handler) complete(ctx context.Context, p *Problem) {
	h.normalize(ctx, p)
	p.finalize()
	h.enrich(ctx, p)
	h.resolveType(ctx, p)
	p.finalize()
}

// normalize replaces a status outside 400–599 with 500. A missing status is
// an ordinary default; any other is a mistake, reported to the OnError hook.
func (h *Handler) normalize(ctx context.Context, p *Problem) {
	if isErrorStatus(p.Status) {
		return
	}

	invalid := p.Status
	p.Status = http.StatusInternalServerError

	if invalid != 0 {
		h.report(ctx, fmt.Errorf("%w: %d, sent 500 instead", ErrInvalidStatus, invalid), p)
	}
}

// enrich runs the default instance enricher and then the consumer's
// enrichers, in order, on p.
func (h *Handler) enrich(ctx context.Context, p *Problem) {
	defaultInstance(ctx, p)

	for _, enrich := range h.enrichers {
		if enrich == nil {
			continue
		}

		// A panicking enricher is skipped; changes it made before panicking
		// stay.
		safeCall(ctx, h, func() any { enrich(ctx, p); return nil })
	}
}

// resolveType turns the relative sub-type of p into a type under the
// handler's namespace. An empty type, about:blank, an absolute URI such as a
// urn: and an absolute path are kept. Any other relative type is a mistake:
// it becomes about:blank, its title is cleared so that it becomes the status
// phrase, and it is reported to the hook with a note wrapping
// ErrInvalidType. Without a namespace, every type is kept, and a relative
// one is reported with the same note, since it leads nowhere. With discovery
// on, a resolved sub-type without a definition is reported with a note
// wrapping ErrUndocumentedType, because its type URL leads to a 404.
//
// Only the configured namespace is used, never the request's Host header,
// which a client controls.
func (h *Handler) resolveType(ctx context.Context, p *Problem) {
	if isKeptType(p.Type) {
		return
	}

	if h.namespace == "" {
		// Passed through, as before namespaces existed and as RFC 9457
		// allows, but a client resolves it against the request URL.
		h.noteType(ctx, ErrInvalidType, p.Type, p)

		return
	}

	if typ, ok := h.registered[p.Type]; ok {
		p.Type = typ

		return
	}

	if isSubtype(p.Type) {
		subtype := p.Type
		p.Type = h.unregistered.resolve(h.namespace, subtype)

		if h.discovery != nil {
			h.noteType(ctx, ErrUndocumentedType, subtype, p)
		}

		return
	}

	// A title written for the lost type would now describe about:blank, so
	// it goes too, and the second finalize sets the status phrase, as RFC
	// 9457 (section 4.2.1) asks for about:blank.
	given := p.Type
	p.Type, p.Title = "", ""

	h.noteType(ctx, ErrInvalidType, given, p)
}

// noteType reports a type mistake to the hook as a note wrapping sentinel
// and naming typ. The note is built only when there is a hook to receive
// it, so resolving a type allocates nothing for notes nobody reads.
func (h *Handler) noteType(ctx context.Context, sentinel error, typ string, p *Problem) {
	if !h.isReporting(ctx) {
		return
	}

	h.callHook(ctx, fmt.Errorf("%w: %q", sentinel, typ), p)
}

// report calls the OnError hook, if any and unless ctx is silenced. A hook
// that panics is ignored: there is nowhere left to report it, and the
// response must still go out.
func (h *Handler) report(ctx context.Context, err error, p *Problem) {
	if h.isReporting(ctx) {
		h.callHook(ctx, err, p)
	}
}

// isReporting reports whether a report in ctx reaches a hook: there is one,
// and the request is not answered silently (see silentRequest). Callers
// check it before building a report, so nobody pays for one that nobody
// reads.
func (h *Handler) isReporting(ctx context.Context) bool {
	return h.onError != nil && !isSilent(ctx)
}

// callHook calls the OnError hook, which the caller has checked with
// isReporting. A hook that panics is ignored.
func (h *Handler) callHook(ctx context.Context, err error, p *Problem) {
	defer func() { _ = recover() }()

	h.onError(ctx, err, p)
}

// Responding: turning a resolved problem into a response.

// Respond turns err into a complete problem response without writing it, so
// any framework adapter can deliver the same output. It reports false when
// there is nothing to write, which is only the case for a nil err. A request
// whose context was canceled is answered like any other: a client that is
// still connected gets the problem, and the server drops a write to one that
// went away.
//
// The format follows req.Accept: application/problem+xml for a client that
// prefers XML (application/problem+xml, application/xml or text/xml), and
// application/problem+json otherwise, including when the header accepts
// neither or is malformed, so a client always gets the status and a document
// rather than a 406.
//
// The body is encoded in full before anything is returned, so a problem that
// cannot be encoded (for example an extension holding a channel or an
// infinite number, or a name XML cannot carry) is never sent in part: the
// response becomes the constant minimal JSON problem
// {"title":"Internal Server Error","status":500}, whatever the format, and
// the failure goes to the [OnError] hook.
func (h *Handler) Respond(ctx context.Context, req Request, err error) (Response, bool) {
	if err == nil {
		return Response{}, false
	}

	ctx = withRequest(ctx, req)

	return h.respond(ctx, req, err, h.Resolve(ctx, err)), true
}

// respondPanic renders a recovered panic as the response for req. A panic is
// never mapped like a returned error: whatever its value, the problem is the
// safe 500 fallback, completed by the enrichers and rendered like any other.
// The OnError hook receives pe.
func (h *Handler) respondPanic(ctx context.Context, req Request, pe *PanicError) Response {
	ctx = withRequest(ctx, req)

	p := fallbackProblem()
	h.complete(ctx, p)

	return h.respond(ctx, req, pe, p)
}

// respond renders p, the problem resolved for err, as the response for req:
// it negotiates the format, encodes p (falling back to the minimal problem)
// and reports err to the OnError hook with the problem being sent.
func (h *Handler) respond(ctx context.Context, req Request, err error, p *Problem) Response {
	res, sent := h.render(ctx, req, p)
	h.report(ctx, err, sent)

	return res
}

// render renders p as the response for req: it negotiates the format and
// encodes p, falling back to the minimal problem when that fails. It
// returns the response and the problem it carries.
func (h *Handler) render(ctx context.Context, req Request, p *Problem) (Response, *Problem) {
	f := negotiateFormat(req.Accept)

	body, ok := h.encode(ctx, f, p)
	if !ok {
		// Nothing of p is sent, not even its headers: the minimal problem is
		// a constant JSON document, so it cannot fail too.
		f, body = &jsonFormat, []byte(fallbackBody)
		p = fallbackProblem()
		p.finalize()
	}

	return newResponse(p.Status, p.Header, f.mediaType, body), p
}

// encode encodes p in format f and reports whether it succeeded. A failure,
// including a panic in the encoder of an extension value, is reported to the
// OnError hook, so the response can fall back to the minimal problem without
// losing the reason.
func (h *Handler) encode(ctx context.Context, f *format, p *Problem) ([]byte, bool) {
	var (
		body []byte
		err  error
	)

	// safeCall returns false when the encoder panicked, after reporting it.
	if completed := safeCall(ctx, h, func() bool { body, err = f.encode(p); return true }); !completed {
		return nil, false
	}

	if err != nil {
		h.report(ctx, fmt.Errorf("%w: %w", ErrEncoding, err), p)

		return nil, false
	}

	return body, true
}

// The net/http adapter.

// Wrap turns a handler function that returns an error into an [http.Handler].
// A non-nil error is written as a problem response; a nil error leaves the
// response as fn wrote it.
//
// Wrap watches what fn writes. When fn has already started the response (it
// wrote a status other than 1xx, wrote body bytes, flushed or hijacked the
// connection), the error is still reported to the [OnError] hook but nothing
// more is written: the client has the start of another response, and
// appending a problem would corrupt it. Headers fn set without writing are
// kept, except those the problem response controls.
//
// Wrap panics when h or fn is nil, so the mistake shows at startup rather
// than on the first request.
func (h *Handler) Wrap(fn func(http.ResponseWriter, *http.Request) error) http.Handler {
	h.mustExist("Wrap")

	if fn == nil {
		panic("prblm: Handler.Wrap called with a nil handler function")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := track(w)
		h.answer(tw, r, fn(tw, r))
	})
}

// Write writes err as a problem response to w, for handlers that keep the
// standard signature. A nil err writes nothing.
//
// Write cannot see what was written to w before it was called, so it assumes
// nothing was, unless w comes from [Handler.Wrap]: then a response that has
// already started is left alone, and err only reaches the [OnError] hook.
//
// Write panics when h is nil.
func (h *Handler) Write(w http.ResponseWriter, r *http.Request, err error) {
	h.mustExist("Write")
	h.answer(track(w), r, err)
}

// answer writes the problem response for err to tw, unless tw has already
// started its response; the error reaches the OnError hook even then. The
// response goes through tw, which marks it started, so an error is never
// answered twice.
func (h *Handler) answer(tw *trackingWriter, r *http.Request, err error) {
	res, ok := h.Respond(r.Context(), requestOf(r), err)
	if !ok || tw.started {
		return
	}

	tw.send(res)
}

// Recover returns middleware that turns a panic in next into a 500 problem
// response. It is opt-in: [Handler.Wrap] alone lets panics reach net/http,
// so existing recovery middleware keeps working.
//
// The problem is the safe fallback, whatever the panic value: status 500,
// title "Internal Server Error" and no detail, rendered like any other
// problem (enrichers, negotiated format, headers). The value never reaches
// the client; the [OnError] hook receives it as a [*PanicError].
//
// A panic with [http.ErrAbortHandler] itself is passed on unchanged, so
// net/http aborts the response as designed; it is not reported. When the
// handler has already started the response, or the panic shows the client
// connection is broken (EPIPE, ECONNRESET), nothing is written and the hook
// receives the panic with a nil problem.
//
// Enrichers and the hook see the context of the request Recover received,
// not values that middleware inside it added before the panic: install
// Recover inside middleware whose context values they need.
//
// Recover installs the same tracking writer as [Handler.Wrap], and Wrap
// reuses it, so Recover(h.Wrap(fn)) keeps one record of what was written.
//
// Recover panics when h or next is nil.
func (h *Handler) Recover(next http.Handler) http.Handler {
	h.mustExist("Recover")

	if next == nil {
		panic("prblm: Handler.Recover called with a nil next handler")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := track(w)

		defer h.recoverPanic(tw, r)

		next.ServeHTTP(tw, r)
	})
}

// recoverPanic is deferred by Recover: it recovers a panic in the handler
// and answers it on tw.
func (h *Handler) recoverPanic(tw *trackingWriter, r *http.Request) {
	v := recover()
	if v == nil {
		return
	}

	// net/http aborts the response on this exact value, by design.
	//nolint:errorlint // only the sentinel itself aborts, not an error wrapping it.
	if err, ok := v.(error); ok && err == http.ErrAbortHandler {
		panic(v)
	}

	// The panicking frames are still on the stack while a deferred function
	// runs, so the stack shows where the panic happened.
	pe := &PanicError{Value: v, Stack: debug.Stack()}

	// A started response cannot take a problem any more, and a broken
	// connection cannot take anything: the panic only goes to the hook.
	req := requestOf(r)
	ctx := withRequest(r.Context(), req)

	if tw.started || isBrokenConnection(pe) {
		h.report(ctx, pe, nil)

		return
	}

	// A panic while answering is a bug in the library or in the writer; the
	// constant problem cannot fail the same way.
	defer h.writeFallback(ctx, tw)

	tw.send(h.respondPanic(ctx, req, pe))
}

// writeFallback is deferred while a panic is answered: when answering panics
// too, it writes the constant minimal problem if nothing was written yet. It
// never panics itself.
func (h *Handler) writeFallback(ctx context.Context, tw *trackingWriter) {
	v := recover()
	if v == nil {
		return
	}

	// report already contains a panicking hook; this guards the write,
	// should the writer itself panic again.
	defer func() { _ = recover() }()

	h.report(ctx, &PanicError{Value: v, Stack: debug.Stack()}, nil)

	if tw.started {
		return
	}

	tw.send(newResponse(http.StatusInternalServerError, nil, mediaTypeJSON, []byte(fallbackBody)))
}

// mustExist panics when h is nil, naming the method, so that a missing
// NewHandler call fails where the mistake is rather than inside a request.
func (h *Handler) mustExist(method string) {
	if h == nil {
		panic("prblm: Handler." + method + " called on a nil problem handler; create it with NewHandler")
	}
}

// Problem types: checking the registered definitions and serving their
// documentation.

// checkTypes checks the definitions passed to WithTypes, once all options
// are applied, and returns the resolved type of each, by sub-type, or nil
// when none is registered. It panics on a missing namespace, an invalid
// definition or a duplicate sub-type. Every definition is checked again as
// DefineType checks it, because a Type such as &Type{} can be built without
// it.
func (h *Handler) checkTypes() map[string]string {
	if len(h.definitions) == 0 {
		return nil
	}

	if h.namespace == "" {
		panic("prblm: WithTypes: types need WithNamespace")
	}

	resolved := make(map[string]string, len(h.definitions))

	for _, t := range h.definitions {
		if err := validateType(t.subtype, t.status, t.title); err != nil {
			panic("prblm: WithTypes: invalid definition: " + err.Error())
		}

		if _, ok := resolved[t.subtype]; ok {
			panic(fmt.Sprintf("prblm: WithTypes: duplicate sub-type %q", t.subtype))
		}

		resolved[t.subtype] = h.namespace + "/" + t.subtype
	}

	return resolved
}

// newDiscovery returns the discovery of the handler's types, or nil when
// discovery is off. The pages are rendered on first use, so a handler that
// never serves discovery, such as the one gin's Recovery builds, never
// renders them.
func (h *Handler) newDiscovery() *discovery {
	if len(h.definitions) == 0 || h.isDiscoveryDisabled {
		return nil
	}

	ns, definitions := h.namespace, h.definitions

	d := newLazyDiscovery(namespacePath(ns), func() site { return renderSite(ns, definitions) })
	d.documented = h.registered

	return d
}

// renderSite renders the documentation of definitions under the namespace
// ns: a page per type and the index, sorted by sub-type.
func renderSite(ns string, definitions []*Type) site {
	sorted := slices.SortedFunc(slices.Values(definitions), func(a, b *Type) int {
		return cmp.Compare(a.subtype, b.subtype)
	})

	docs := make(map[string]pages, len(sorted))
	index := indexDoc{Types: make([]indexEntry, 0, len(sorted))}

	for _, t := range sorted {
		doc := typeDoc{
			Type:        ns + "/" + t.subtype,
			Subtype:     t.subtype,
			Status:      t.status,
			Title:       t.title,
			Description: t.description,
		}
		docs[t.subtype] = pages{html: htmlPage(t.subtype, docTemplate, doc), json: jsonPage(t.subtype, doc)}
		index.Types = append(index.Types, indexEntry{Type: doc.Type, Subtype: doc.Subtype, Status: doc.Status, Title: doc.Title})
	}

	return site{
		docs:  docs,
		index: pages{html: htmlPage("index", indexTemplate, index), json: jsonPage("index", index)},
	}
}

// DiscoveryPath returns the path the documentation of the registered types
// is served under, which is the path of the namespace, or "" when discovery
// is off (no types registered, or [WithoutDiscovery]). Mount
// [Handler.Discovery] there:
//
//	if path := h.DiscoveryPath(); path != "" {
//		mux.Handle("GET "+path+"/", h.Discovery())
//	}
//
// DiscoveryPath panics when h is nil.
func (h *Handler) DiscoveryPath() string {
	h.mustExist("DiscoveryPath")

	if h.discovery == nil {
		return ""
	}

	// Every adapter and every net/http mount calls DiscoveryPath when it is
	// set up, so rendering here makes a rendering failure surface at
	// startup.
	h.discovery.site()

	return h.discovery.path
}

// Discovery returns an [http.Handler] that serves the documentation of the
// registered types at their type URLs, so a client can dereference a
// problem's type, as RFC 9457 recommends. It is never nil. It answers with
// [Handler.RespondDiscovery], and otherwise with a problem: with discovery
// off, a 404 to every request; with discovery on, a 405 with Allow: GET,
// HEAD to any method other than GET and HEAD, and a 404 to a GET or HEAD
// outside the discovery path. It is meant to be mounted at the discovery
// path, where every request is for it.
//
// Discovery panics when h is nil.
func (h *Handler) Discovery() http.Handler {
	h.mustExist("Discovery")

	// Rendered here, when the handler is mounted, so a rendering failure
	// surfaces at startup rather than on the first request.
	if h.discovery != nil {
		h.discovery.site()
	}

	return http.HandlerFunc(h.serveDiscovery)
}

// serveDiscovery serves one discovery request over net/http.
func (h *Handler) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	base := requestOf(r)
	req := DiscoveryRequest{
		Method:      base.Method,
		Path:        r.URL.EscapedPath(),
		Accept:      base.Accept,
		IfNoneMatch: strings.Join(r.Header.Values("If-None-Match"), ","),
	}

	res, ok := h.RespondDiscovery(r.Context(), req)
	if !ok {
		var p *Problem
		if h.discovery == nil || isReadMethod(r.Method) {
			p = New(http.StatusNotFound)
		} else {
			p = New(http.StatusMethodNotAllowed, WithHeader("Allow", "GET, HEAD"))
		}

		res = headless(req.Method, h.discoveryProblem(r.Context(), req, p))
	}

	track(w).send(res)
}

// RespondDiscovery returns the discovery response to req without writing
// it, so every framework adapter serves discovery by the same rules, and
// sends the same bytes as [Handler.Discovery]. It reports false when req is
// not for discovery: discovery is off, the method is not GET or HEAD, or
// req.Path is neither the discovery path (see [Handler.DiscoveryPath]),
// that path with a trailing "/", nor under it. The adapter then handles the
// request as usual.
//
// Otherwise the response is the documentation or the index, negotiated by
// req.Accept, a 304 when req.IfNoneMatch matches its ETag, or a 404 problem
// for a path that names no registered type. The path is matched exactly in
// its escaped form, so a sub-type spelled with escapes ("%2D"), a malformed
// escape ("%zz") or a traversal ("%2e%2e") is a 404, and every page has
// exactly one URL. For HEAD the body is empty and the headers are those of
// the GET. The problems discovery sends never reach the [OnError] hook,
// because no application error occurred.
//
// RespondDiscovery panics when h is nil.
func (h *Handler) RespondDiscovery(ctx context.Context, req DiscoveryRequest) (Response, bool) {
	h.mustExist("RespondDiscovery")

	d := h.discoveryFor(req)
	if d == nil {
		return Response{}, false
	}

	var res Response

	if doc, ok := d.lookup(req.Path); ok {
		res = doc.negotiate(req.Accept).response(req.IfNoneMatch)
	} else {
		res = h.discoveryProblem(ctx, req, New(http.StatusNotFound))
	}

	return headless(req.Method, res), true
}

// NoteShadowedRoute reports that req was answered by the application route
// with the pattern route, so the OnError hook learns when that route hides
// type documentation. A framework adapter calls it for every request a
// route answers, with the route's pattern (gin's c.FullPath(), fiber's
// c.Route().Path); net/http gives no such hook point. Only req.Method and
// req.Path are read, so an adapter need not read the headers.
//
// When discovery would have served a documentation page or the index for
// req, the hook receives a note wrapping [ErrShadowedDocumentation] that
// names the route and the path, with a nil problem. Each route is noted
// once, at its first conflicting request, so one conflict cannot flood the
// log, and at most 256 routes are noted. There is no note when discovery is
// off, the method is not GET or HEAD, the path is outside the discovery
// path, or discovery would have answered it with a 404, since an unknown
// sub-type hides nothing. Without a hook nothing is built or remembered.
//
// NoteShadowedRoute panics when h is nil.
func (h *Handler) NoteShadowedRoute(ctx context.Context, route string, req DiscoveryRequest) {
	h.mustExist("NoteShadowedRoute")

	d := h.discoveryFor(req)
	if d == nil || !h.isReporting(ctx) {
		return
	}

	if !d.hasPage(req.Path) || !d.noted.add(route) {
		return
	}

	h.callHook(ctx, fmt.Errorf("%w: route %q hides %q", ErrShadowedDocumentation, route, req.Path), nil)
}

// discoveryFor returns the discovery that req is for, or nil when it is
// not for discovery: discovery is off, the method is not GET or HEAD, or
// the path is outside the discovery path. It is the one rule every adapter
// relies on, through RespondDiscovery and NoteShadowedRoute.
func (h *Handler) discoveryFor(req DiscoveryRequest) *discovery {
	d := h.discovery
	if d == nil || !isReadMethod(req.Method) || !d.isUnder(req.Path) {
		return nil
	}

	return d
}

// discoveryProblem renders p as the answer to the discovery request req,
// completed and rendered like any other problem, but answered silently:
// the OnError hook is not called, not even for a note about a faulty
// enricher or a failed encoding, because no application error occurred.
// The instance is the path as the client sent it.
func (h *Handler) discoveryProblem(ctx context.Context, req DiscoveryRequest, p *Problem) Response {
	r := Request{Method: req.Method, Path: req.Path, Accept: req.Accept}
	ctx = silentRequest(ctx, r)

	h.complete(ctx, p)
	res, _ := h.render(ctx, r, p)

	return res
}

// headless returns res as the answer to a request with method: for HEAD,
// the same headers and no body.
func headless(method string, res Response) Response {
	if method == http.MethodHead {
		res.Body = nil
	}

	return res
}

// isReadMethod reports whether method is GET or HEAD, the only methods
// discovery serves.
func isReadMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// defaultInstance sets the instance of p to the path of the request being
// answered, when p has no instance. The path excludes the query string, so
// tokens in it cannot leak.
func defaultInstance(ctx context.Context, p *Problem) {
	if p.Instance != "" {
		return
	}

	if req, ok := RequestFrom(ctx); ok {
		p.Instance = req.Path
	}
}

// isErrorStatus reports whether status is a client or server error status.
func isErrorStatus(status int) bool {
	return status >= 400 && status <= 599
}

// selfDescribed returns the problem of the outermost [Describer] in the
// chain of err, as [errors.As] finds it, or nil when there is none or it
// returns nil. A *Problem is a Describer of itself.
func selfDescribed(err error) *Problem {
	var d Describer
	if !errors.As(err, &d) {
		return nil
	}

	return d.Problem()
}

// hinted returns a problem for the outermost status hint in the chain of err,
// or nil when there is none or it is not an error status.
func hinted(err error) *Problem {
	hint, ok := errors.AsType[*statusError](err)
	if !ok || !isErrorStatus(hint.status) {
		return nil
	}

	return &Problem{Status: hint.status}
}

// isNilMapper reports whether m is nil or a nil [MapperFunc]. A nil
// MapperFunc can never match: calling it panics, which would send a panic
// note to the hook on every request. Other typed-nil mappers are called,
// because a method on a nil pointer may be valid; a panic there is contained
// like any other.
func isNilMapper(m Mapper) bool {
	if m == nil {
		return true
	}

	f, ok := m.(MapperFunc)

	return ok && f == nil
}

// fallbackProblem is the safe answer for an error that nothing recognizes:
// a 500 with no detail, so no error text, stack or internals reach the client.
func fallbackProblem() *Problem {
	return &Problem{Status: http.StatusInternalServerError}
}
