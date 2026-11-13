package ginprblm

import (
	"errors"
	"syscall"
)

// isBrokenConnection reports whether err shows that the client connection is
// gone (EPIPE or ECONNRESET), so that nothing can be written to it. Unlike
// the core, it needs no Plan 9 variant: gin itself does not build there.
func isBrokenConnection(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}
