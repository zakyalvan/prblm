//go:build !plan9

package prblm

import (
	"errors"
	"syscall"
)

// isBrokenConnection reports whether err shows that the client connection is
// gone (EPIPE or ECONNRESET), so that nothing can be written to it.
func isBrokenConnection(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}
