package prblm

import (
	"context"
	"errors"
)

// Mapper turns an error into a problem.
//
// MapError returns nil when the mapper does not recognize err. A nil result
// is the only "no match" signal; no status code or other sentinel value is
// used for it.
type Mapper interface {
	MapError(ctx context.Context, err error) *Problem
}

// Describer is implemented by errors that know which problem they are. A
// describer in an error chain is used before any mapper; Problem may return
// nil to leave the error to the mappers.
type Describer interface {
	Problem() *Problem
}

// MapperFunc adapts a function to the [Mapper] interface.
type MapperFunc func(ctx context.Context, err error) *Problem

// MapError calls f(ctx, err).
func (f MapperFunc) MapError(ctx context.Context, err error) *Problem {
	return f(ctx, err)
}

// Status returns a mapper that maps any error matching target, as
// [errors.Is] reports it, to a problem with the given status. It panics if
// target is nil.
func Status(target error, status int) Mapper {
	return Is(target, func(error) *Problem {
		return &Problem{Status: status}
	})
}

// Is returns a mapper that calls fn for any error matching target, as
// [errors.Is] reports it, including wrapped and joined errors. fn receives the
// error being resolved and may return nil to decline it. Is panics if target
// or fn is nil, so a mistake shows up when the mapper is built.
func Is(target error, fn func(err error) *Problem) Mapper {
	if target == nil {
		panic("prblm: Is called with a nil target error")
	}

	if fn == nil {
		panic("prblm: Is called with a nil function")
	}

	return MapperFunc(func(_ context.Context, err error) *Problem {
		if !errors.Is(err, target) {
			return nil
		}

		return fn(err)
	})
}

// As returns a mapper that calls fn with the first error in the chain of type
// T, as [errors.As] finds it, so the problem can use the error's data. fn
// may return nil to decline the error. As panics if fn is nil.
func As[T error](fn func(t T) *Problem) Mapper {
	if fn == nil {
		panic("prblm: As called with a nil function")
	}

	return MapperFunc(func(_ context.Context, err error) *Problem {
		t, ok := errors.AsType[T](err)
		if !ok {
			return nil
		}

		return fn(t)
	})
}
