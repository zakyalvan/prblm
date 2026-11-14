package prblm

import (
	"context"
	"encoding/json"
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

// sentinels lists the sentinel errors with their problems, by non-descending
// status. The order only breaks a tie within one error whose Is method
// reports several sentinels: the lowest status wins.
var sentinels = []sentinel{
	{err: ErrInvalidInput, status: http.StatusBadRequest, detail: "The request is invalid."},
	{err: ErrUnauthenticated, status: http.StatusUnauthorized, detail: "Authentication is required."},
	{err: ErrPermissionDenied, status: http.StatusForbidden, detail: "You are not allowed to perform this action."},
	{err: ErrNotFound, status: http.StatusNotFound, detail: "The requested resource was not found."},
	{err: ErrConflict, status: http.StatusConflict, detail: "The request conflicts with the current state of the resource."},
	{err: ErrPreconditionFailed, status: http.StatusPreconditionFailed, detail: "A precondition of the request was not met."},
	{err: ErrRateLimited, status: http.StatusTooManyRequests, detail: "Too many requests."},
	{err: ErrBadGateway, status: http.StatusBadGateway, detail: "An upstream service returned an invalid response."},
	{err: ErrUnavailable, status: http.StatusServiceUnavailable, detail: "The service is temporarily unavailable."},
	{err: ErrUpstreamTimeout, status: http.StatusGatewayTimeout, detail: "An upstream service did not respond in time."},
}

// mapSentinel maps the first sentinel found in err's chain to an about:blank
// problem with the sentinel's status and fixed detail, or returns nil when the
// chain holds none before the status hint that would apply, which then
// decides instead. The detail never comes from the error text.
func mapSentinel(_ context.Context, err error) *Problem {
	var stop error // a nil interface when no hint applies, never a typed nil
	if hint := applicableHint(err); hint != nil {
		stop = hint
	}

	s := firstSentinel(err, stop)
	if s == nil {
		return nil
	}

	return &Problem{Status: s.status, Detail: s.detail}
}

// firstSentinel walks err's chain in errors.Is order and returns the first
// sentinel an error is or reports, or nil when it reaches stop or the end
// (see the sentinel docs for why the outermost wins). errors.Is alone can't
// say where in the chain a match is.
func firstSentinel(err, stop error) *sentinel {
	if err == nil || err == stop { //nolint:errorlint // stop is one link of this chain
		return nil
	}

	if s := ownSentinel(err); s != nil {
		return s
	}

	switch u := err.(type) { //nolint:errorlint // one link of the chain, as errors.Is unwraps it
	case interface{ Unwrap() error }:
		return firstSentinel(u.Unwrap(), stop)
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			if s := firstSentinel(e, stop); s != nil {
				return s
			}
		}
	}

	return nil
}

// ownSentinel returns the sentinel err itself is or reports through its Is
// method, without looking at the errors it wraps, as errors.Is tests each
// error. Comparing with == can't panic: every sentinel is a pointer.
func ownSentinel(err error) *sentinel {
	is, hasIs := err.(interface{ Is(target error) bool })

	for i := range sentinels {
		if err == sentinels[i].err || hasIs && is.Is(sentinels[i].err) { //nolint:errorlint // one link of the chain, as errors.Is tests it
			return &sentinels[i]
		}
	}

	return nil
}
