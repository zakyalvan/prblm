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
	// ErrInvalidInput reports that the request is invalid: a malformed or
	// missing parameter, or a value the operation does not accept. It becomes
	// a 400 Bad Request problem. For field-level validation details, map the
	// validation error to a problem that lists them instead.
	ErrInvalidInput = errors.New("prblm: invalid input")

	// ErrUnauthenticated reports that the request has no valid credentials:
	// they are missing, expired or wrong. It becomes a 401 Unauthorized
	// problem. For a caller who is known but not allowed, use
	// [ErrPermissionDenied].
	//
	// RFC 9110 requires a WWW-Authenticate header on a 401, and the handler
	// cannot know the authentication scheme, so the default problem has none.
	// Add it with a consumer mapper, which replaces the default problem:
	//
	//	prblm.WithMappers(prblm.Is(prblm.ErrUnauthenticated, func(error) *prblm.Problem {
	//		return prblm.New(http.StatusUnauthorized,
	//			prblm.WithDetail("Authentication is required."),
	//			prblm.WithHeader("WWW-Authenticate", `Bearer realm="api"`))
	//	}))
	ErrUnauthenticated = errors.New("prblm: unauthenticated")

	// ErrPermissionDenied reports that the caller is known but not allowed to
	// perform the action. It becomes a 403 Forbidden problem. A 403 confirms
	// that the resource exists; when that is itself secret, return
	// [ErrNotFound] instead.
	ErrPermissionDenied = errors.New("prblm: permission denied")

	// ErrNotFound reports that the requested resource does not exist. It
	// becomes a 404 Not Found problem.
	ErrNotFound = errors.New("prblm: not found")

	// ErrConflict reports that the request conflicts with the current state
	// of the resource: a duplicate, a state that forbids the operation, or a
	// concurrent change. It becomes a 409 Conflict problem.
	ErrConflict = errors.New("prblm: conflict")

	// ErrPreconditionFailed reports that a conditional request header, such
	// as If-Match or If-Unmodified-Since, does not hold, for example in
	// optimistic concurrency control. It becomes a 412 Precondition Failed
	// problem. It is only for those headers: when the current state of the
	// resource forbids the operation, return [ErrConflict].
	ErrPreconditionFailed = errors.New("prblm: precondition failed")

	// ErrRateLimited reports that the caller sent too many requests. It
	// becomes a 429 Too Many Requests problem without a Retry-After header,
	// since a sentinel carries no delay; a client may then retry at once. Add
	// the header with a consumer mapper for ErrRateLimited, or with an
	// enricher that sets it on 429 problems:
	//
	//	prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) {
	//		if p.Status == http.StatusTooManyRequests && p.Header.Get("Retry-After") == "" {
	//			if p.Header == nil {
	//				p.Header = make(http.Header)
	//			}
	//
	//			p.Header.Set("Retry-After", "30")
	//		}
	//	})
	ErrRateLimited = errors.New("prblm: rate limited")
)
