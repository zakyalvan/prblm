// Package sqlmap maps database/sql and database/sql/driver errors to
// RFC 9457 problems. It uses only the standard library.
//
// The mapper does nothing until it is registered, so importing the package
// changes no behavior:
//
//	h := prblm.NewHandler(prblm.WithMappers(sqlmap.Mapper()))
//
// Registered, it maps an error chain containing:
//
//   - [database/sql.ErrNoRows] to 404, with the detail "The requested
//     resource was not found.". Most handlers that query a single row mean
//     "this resource does not exist" by it.
//   - [database/sql/driver.ErrBadConn] or [database/sql.ErrConnDone] to 503,
//     with the detail "The service is temporarily unavailable.".
//
// The details are fixed sentences: no SQL, error text or row values reach
// the client. The original error still reaches the handler's OnError hook,
// which is where to log it. [WithNotFound] and [WithUnavailable] replace the
// problem of either case.
//
// Every other error is left to later mappers and finally to the handler's
// safe 500 fallback. That includes, deliberately:
//
//   - [database/sql.ErrTxDone], which means a transaction was used after
//     commit or rollback: a programming error, not a client one.
//   - [database/sql/driver.ErrSkip] and [database/sql/driver.ErrRemoveArgument],
//     which are internal to drivers.
//   - Driver-specific "not found" errors that a driver returns instead of
//     [database/sql.ErrNoRows]. Map them with a mapper of your own, or with
//     the mapper for that driver.
package sqlmap
