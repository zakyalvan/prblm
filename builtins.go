package prblm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// builtins map the package's sentinel errors, then the standard library
// errors a handler commonly returns while reading a request body. Each
// detail is a fixed sentence: offsets, field names and the error text never
// reach the client.
//
// The first match wins, so the order matters for an error with several
// causes. The package's sentinels come first: the application chose them,
// while a standard library error in the same chain is usually their cause.
// Then errors whose type identifies their source come before standard
// library sentinels that could come from anywhere.
//
// A bare io.EOF is deliberately not mapped: it comes from any reader,
// including an upstream service closing early, and a 400 would blame the
// client for a server failure. It falls to the safe 500; a consumer that
// knows its io.EOF means an empty body maps it with its own mapper.
var builtins = []Mapper{
	MapperFunc(mapSentinel),
	As(func(*http.MaxBytesError) *Problem {
		return &Problem{Status: http.StatusRequestEntityTooLarge, Detail: "The request body is too large."}
	}),
	As(func(*json.UnmarshalTypeError) *Problem {
		return &Problem{Status: http.StatusUnprocessableEntity, Detail: "The request body has a value of the wrong type."}
	}),
	As(func(*json.SyntaxError) *Problem {
		return &Problem{Status: http.StatusBadRequest, Detail: "The request body is not valid JSON."}
	}),
	Is(io.ErrUnexpectedEOF, func(error) *Problem {
		return &Problem{Status: http.StatusBadRequest, Detail: "The request body is incomplete."}
	}),
	Status(context.DeadlineExceeded, http.StatusGatewayTimeout),
}

// sentinel is the problem a sentinel error resolves to.
type sentinel struct {
	err    error
	status int
	detail string
}

// sentinels lists the sentinel errors with their problems, by ascending
// status, so the lowest status wins for a chain that matches several.
var sentinels = []sentinel{
	{err: ErrInvalidInput, status: http.StatusBadRequest, detail: "The request is invalid."},
	{err: ErrUnauthenticated, status: http.StatusUnauthorized, detail: "Authentication is required."},
	{err: ErrPermissionDenied, status: http.StatusForbidden, detail: "You are not allowed to perform this action."},
	{err: ErrNotFound, status: http.StatusNotFound, detail: "The requested resource was not found."},
	{err: ErrConflict, status: http.StatusConflict, detail: "The request conflicts with the current state of the resource."},
	{err: ErrPreconditionFailed, status: http.StatusPreconditionFailed, detail: "A precondition of the request was not met."},
	{err: ErrRateLimited, status: http.StatusTooManyRequests, detail: "Too many requests."},
}

// mapSentinel maps the first sentinel that err matches, as errors.Is reports
// it, to an about:blank problem with the sentinel's status and fixed detail,
// or returns nil when err matches none. The detail never comes from the
// error text.
func mapSentinel(_ context.Context, err error) *Problem {
	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return &Problem{Status: s.status, Detail: s.detail}
		}
	}

	return nil
}
