// Package pgxmap maps PostgreSQL errors reported by pgx ([*pgconn.PgError])
// to RFC 9457 problems, by their SQLSTATE code.
//
// It is its own module, so pgx never becomes a dependency of the core
// module. The mapper does nothing until it is registered, so importing the
// package changes no behavior:
//
//	h := prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper()))
//
// # Default mapping
//
// Registered, the mapper maps a PostgreSQL error in the chain by its code:
//
//	Code   Status  Detail
//	23505  409     The resource conflicts with an existing one.
//	23503  409     The resource references, or is referenced by, another resource.
//	23502  422     A required value is missing.
//	23514  422     A value does not satisfy a constraint.
//	40001  409     The request conflicted with a concurrent change; retry it.
//	40P01  409     The request conflicted with a concurrent change; retry it.
//	53300  503     The service is temporarily unavailable.
//	57014  504     The request took too long to complete.
//
// 40001, 40P01 and 53300 are retryable: their problems carry a Retry-After
// header, one second unless [WithRetryAfter] sets another value. 57014
// (query_canceled) is mostly a statement_timeout, since pgx reports a
// canceled request context as a context error rather than this code. A
// retry would most likely time out again, so its problem has no
// Retry-After, and its 504 matches the handler's mapping of
// context.DeadlineExceeded.
//
// Every other code, such as an undefined table (42P01) or a syntax error
// (42601), is a server fault: it is left to later mappers and finally to the
// handler's safe 500 fallback. So are errors without a PostgreSQL error in
// the chain. That includes pgx.ErrNoRows, which wraps sql.ErrNoRows: the
// mapper of github.com/kartaladev/prblm/mappers/sqlmap maps it to 404.
//
// # Privacy
//
// A PostgreSQL error describes the database: its message, detail and hint
// name tables, columns and constraints and quote row values, such as
// "Key (email)=(alice@example.com) already exists.". The default problems
// contain only the generic sentence above, so none of that reaches the
// client. The original error still reaches the handler's OnError hook,
// which is where to log it.
//
// [WithDatabaseDetail] is the one option that loosens this default: it puts
// the PostgreSQL detail into the problem. Use it only for internal APIs.
//
// # Custom problems
//
// [WithConstraint] gives one constraint its own problem, for example a
// friendly message for a duplicate email, and [WithCode] replaces the
// problem of a code or maps a code the table does not list. A function that
// returns nil falls back to the next step: constraint, then code, then the
// table above.
//
// # gorm
//
// Register this mapper before the gorm mapper of
// github.com/kartaladev/prblm/mappers/gormmap:
//
//	h := prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper(), gormmap.Mapper()))
//
// With gorm's TranslateError off, gorm returns the driver's error, which
// this mapper matches. With it on, gorm's PostgreSQL driver (v1.6.2 and
// later) wraps both its sentinel, such as gorm.ErrDuplicatedKey, and the
// driver's error. Registered first, this mapper answers those errors too,
// with the problems of [WithConstraint] and [WithCode]; registered after the
// gorm mapper, it would never see them.
package pgxmap
