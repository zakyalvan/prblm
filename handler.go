package prblm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
}

// NewHandler returns a handler configured by opts. Nil options are ignored.
// With no options it is ready to use: built-in mappers on, the instance set
// to the request path, and a safe 500 for anything unrecognized. The handler
// does not change after NewHandler returns and is safe for concurrent use.
func NewHandler(opts ...Option) *Handler {
	h := &Handler{builtins: builtins}

	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}

	return h
}

// Resolve turns err into a problem, or returns nil for a nil err.
//
// The steps run in this order, and the first one that recognizes err wins:
//
//  1. the outermost *Problem or [Describer] in the chain of err
//  2. the mappers scoped to ctx with [ScopeMappers], newest scope first
//  3. the handler's own mappers, in registration order
//  4. the built-in mappers for request body and deadline errors
//     (see [WithoutBuiltins])
//  5. a status hint attached with [WithStatus]
//  6. the fallback: a 500 problem that carries none of the error's text
//
// The result is always a new problem that the caller may modify: a matched
// problem is copied, so problems shared between requests (for example
// package-level values) are never changed. A status outside 400–599 is
// replaced by 500, because a problem response must be an error; unless the
// status was missing, the mistake is reported to the [OnError] hook. An
// about:blank problem without a title gets the HTTP reason phrase of its
// status. Finally the enrichers run (see [WithEnrichers]).
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

// complete applies the steps every problem goes through after it is
// matched: status normalization, title finalization and the enrichers.
func (h *Handler) complete(ctx context.Context, p *Problem) {
	h.normalize(ctx, p)
	p.finalize()
	h.enrich(ctx, p)
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

// report calls the OnError hook, if any. A hook that panics is ignored:
// there is nowhere left to report it, and the response must still go out.
func (h *Handler) report(ctx context.Context, err error, p *Problem) {
	if h.onError == nil {
		return
	}

	defer func() { _ = recover() }()

	h.onError(ctx, err, p)
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
