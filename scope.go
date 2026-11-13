package prblm

import (
	"context"
	"slices"
)

type (
	// scopeKey is the context key of the request-scoped mappers.
	scopeKey struct{}
	// requestKey is the context key of the request being answered.
	requestKey struct{}
)

// ScopeMappers returns a copy of ctx carrying mappers for that request only,
// for example from a middleware on a group of routes. Scoped mappers run
// before the handler's own mappers, in the order given. Scoping a context that
// already has scoped mappers adds to them, the newer ones first; ctx itself is
// never changed, so sibling scopes do not see each other. Nil mappers,
// including a nil [MapperFunc], are skipped.
func ScopeMappers(ctx context.Context, mappers ...Mapper) context.Context {
	// slices.Concat always allocates, so nested scopes never share storage.
	return context.WithValue(ctx, scopeKey{}, slices.Concat(mappers, scopedMappers(ctx)))
}

// scopedMappers returns the mappers attached to ctx with [ScopeMappers].
func scopedMappers(ctx context.Context) []Mapper {
	mappers, _ := ctx.Value(scopeKey{}).([]Mapper)

	return mappers
}

// withRequest returns ctx carrying req, for [RequestFrom].
func withRequest(ctx context.Context, req Request) context.Context {
	return context.WithValue(ctx, requestKey{}, answered{req: req})
}

// silentRequest returns a copy of ctx carrying req, answered without
// calling the OnError hook: discovery answers its 404 and 405 with
// problems, but no application error occurred, so a note about them would
// mislead. The mark lives with the request, so a request answered inside
// it, with its own withRequest, is reported as usual.
func silentRequest(ctx context.Context, req Request) context.Context {
	return context.WithValue(ctx, requestKey{}, answered{req: req, isSilent: true})
}

// answered is the request being answered, as a context carries it.
type answered struct {
	req      Request
	isSilent bool
}

// isSilent reports whether the request ctx carries is answered silently.
// The request is the newest context value, so the lookup stops early.
func isSilent(ctx context.Context) bool {
	a, ok := ctx.Value(requestKey{}).(answered)

	return ok && a.isSilent
}

// RequestFrom returns the request that [Handler.Respond] or
// [Handler.Recover] is answering, for use in enrichers and hooks. It reports
// false outside of them.
func RequestFrom(ctx context.Context) (Request, bool) {
	a, ok := ctx.Value(requestKey{}).(answered)

	return a.req, ok
}
