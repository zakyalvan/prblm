package prblm

// statusError attaches a status hint to an error.
type statusError struct {
	err    error
	status int
}

// WithStatus attaches a status hint to err. When nothing else describes or
// maps the error, the hint produces an about:blank problem with that status
// and no detail, so the error text still never reaches the client. A hint
// outside 400–599 is ignored, and the outermost hint in a chain wins.
//
// A hint is the last resort before the 500 fallback: a mapper, a built-in
// mapper or a sentinel error such as [ErrNotFound] in the chain takes
// precedence, so WithStatus(prblm.ErrNotFound, 400) is still a 404.
//
// WithStatus returns nil for a nil err, so it can wrap a call's result
// directly.
func WithStatus(err error, status int) error {
	if err == nil {
		return nil
	}

	return &statusError{err: err, status: status}
}

// Error returns the message of the wrapped error unchanged.
func (e *statusError) Error() string { return e.err.Error() }

// Unwrap returns the wrapped error.
func (e *statusError) Unwrap() error { return e.err }
