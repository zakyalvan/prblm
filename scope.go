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
	return context.WithValue(ctx, requestKey{}, req)
}

// RequestFrom returns the request that [Handler.Respond] or
// [Handler.Recover] is answering, for use in enrichers and hooks. It reports
// false outside of them.
func RequestFrom(ctx context.Context) (Request, bool) {
	req, ok := ctx.Value(requestKey{}).(Request)

	return req, ok
}
