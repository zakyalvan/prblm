package prblm

import "context"

// Option configures a [Handler].
type Option func(*Handler)

// WithMappers registers consumer mappers. They run in registration order and
// the first one that returns a problem wins. Nil mappers, including a nil
// [MapperFunc], are skipped.
func WithMappers(mappers ...Mapper) Option {
	return func(h *Handler) {
		h.mappers = append(h.mappers, mappers...)
	}
}

// WithoutBuiltins disables the built-in mappers for malformed, truncated and
// oversized request bodies and for deadline errors. Without them those errors
// resolve like any other unmapped error, to a 500.
func WithoutBuiltins() Option {
	return func(h *Handler) {
		h.builtins = nil
	}
}

// WithEnrichers registers functions that run, in order, on every resolved
// problem before it is written, for example to add a trace ID from the
// request context. They receive the request's own copy of the problem.
// Before them, a default enricher sets the instance to the request path
// (without the query string) when the problem has none.
func WithEnrichers(enrichers ...func(ctx context.Context, p *Problem)) Option {
	return func(h *Handler) {
		h.enrichers = append(h.enrichers, enrichers...)
	}
}

// OnError registers a hook that is called once for every error the handler
// answers, with the request context, the original error and the problem
// being sent. It is the place to log: the library never logs by itself, and
// the problem sent to the client deliberately leaves out the error text.
// The hook must not modify the problem.
//
// Before that call, the hook may also receive notes about mistakes the
// handler worked around: a [*PanicError] from consumer code that panicked
// (with a nil problem), an error wrapping [ErrInvalidStatus] for a status
// outside 400–599 that was replaced, an error wrapping [ErrInvalidType] for a
// relative type that is not a valid sub-type and was replaced by
// about:blank, or an error wrapping [ErrEncoding] for a problem that could
// not be encoded. Use errors.Is and errors.As to tell
// notes from the error being answered. The encoding note carries the problem
// that failed to encode; the call that follows it carries the minimal 500
// problem that was sent instead. A hook that panics is ignored.
func OnError(hook func(ctx context.Context, err error, p *Problem)) Option {
	return func(h *Handler) {
		h.onError = hook
	}
}
