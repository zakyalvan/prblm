package prblm

import "errors"

// Errors to return.
//
// An application returns one of these errors, or wraps it, to say what went
// wrong, and the handler answers with the matching problem without any
// mapper to register:
//
//	return fmt.Errorf("vessel %d: %w", id, prblm.ErrNotFound)
//
// The problem is about:blank with the error's status, the status phrase as
// title and a fixed detail. The wrap text ("vessel 7"), the error string and
// any wrapped cause never reach the client; the [OnError] hook receives the
// error as returned, so log it there.
//
// A sentinel is found anywhere in the chain, as [errors.Is] reports it:
// wrapped with %w, joined with [errors.Join], or reported by the Is method of
// a domain error type. Test for it with errors.Is, never with ==, since a
// wrapped error is not equal to the sentinel. Do not reassign these
// variables: the handler keeps the original values, so errors built from a
// new value no longer match.
//
// The mappings are built-in mappers (see [Handler.Resolve]): a problem or
// [Describer] in the chain, a scoped mapper and a consumer mapper all come
// first, so prblm.Is(prblm.ErrNotFound, ...) replaces the default problem.
// [WithoutBuiltins] turns them off together with the other built-ins.
//
// A sentinel names what happened, while a [WithStatus] hint only sets a
// status when nothing else recognizes the error, so a sentinel in the chain
// decides the status even when the error also carries a hint. When an error
// matches several sentinels, the lowest status wins, and a sentinel wins over
// a standard library error such as [context.DeadlineExceeded] in the same
// chain, which is usually its cause.
//
// The sentinels are the application's own classification. A data layer's
// not-found error, such as sql.ErrNoRows, is not one of them: map it with a
// mapper, or wrap it with the sentinel where the application decides that it
// means "not found".
var (
	// ErrNotFound reports that the requested resource does not exist. It
	// becomes a 404 Not Found problem.
	ErrNotFound = errors.New("prblm: not found")
)
