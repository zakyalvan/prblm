// Package paritytest holds the cross-adapter parity tests: for the same
// configured problem handler and request, net/http, gin and fiber must send
// the same status, Content-Type, Vary and body bytes. It is an unpublished
// module, so neither adapter module depends on the other framework.
package paritytest
