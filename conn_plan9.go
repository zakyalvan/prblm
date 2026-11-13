package prblm

// isBrokenConnection always reports false on Plan 9, which has no EPIPE or
// ECONNRESET errors to recognize: a write to a client that is gone fails
// quietly instead.
func isBrokenConnection(error) bool {
	return false
}
