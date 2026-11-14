// Package gormmap maps gorm's sentinel errors to RFC 9457 problems.
//
// It is its own module, so gorm never becomes a dependency of the core
// module. The mapper does nothing until it is registered, so importing the
// package changes no behavior:
//
//	h := prblm.NewHandler(prblm.WithMappers(gormmap.Mapper()))
//
// Registered, it maps an error chain containing:
//
//   - [gorm.ErrRecordNotFound] to 404, with the detail "The requested
//     resource was not found.".
//   - [gorm.ErrDuplicatedKey] to 409, with the detail "The resource
//     conflicts with an existing one.".
//   - [gorm.ErrForeignKeyViolated] to 409, with the detail "The resource
//     references, or is referenced by, another resource.".
//   - [gorm.ErrCheckConstraintViolated] to 422, with the detail "A value
//     does not satisfy a constraint.".
//
// No constraint name or value is included. The last three are the problems
// the PostgreSQL mapper of github.com/kartaladev/prblm/mappers/pgxmap gives
// SQLSTATE 23505, 23503 and 23514, so a client gets the same problem
// whichever mapper answers.
//
// [WithRecordNotFound], [WithDuplicatedKey], [WithForeignKeyViolated] and
// [WithCheckConstraintViolated] replace these problems. Every other error,
// including other gorm sentinels such as [gorm.ErrInvalidTransaction] and
// [gorm.ErrInvalidField] (an undefined column, which is a server fault), is
// left to later mappers and finally to the handler's safe 500 fallback. The
// original error still reaches the handler's OnError hook, which is where
// to log it.
//
// # Translated errors need TranslateError
//
// gorm returns the last three sentinels only when it translates driver
// errors, which is off by default:
//
//	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
//
// Not every driver translates every one: gorm's SQLite and MySQL drivers
// translate unique and foreign key violations, and only its PostgreSQL
// driver translates check constraint violations. Without TranslateError, a
// violation reaches the handler as the driver's own error, which this
// mapper does not match.
//
// # Register the driver mapper first
//
// Register a mapper for the driver before this one, for example the
// PostgreSQL mapper:
//
//	h := prblm.NewHandler(prblm.WithMappers(pgxmap.Mapper(), gormmap.Mapper()))
//
// With TranslateError off, the driver mapper answers the driver's errors.
// With it on, gorm's PostgreSQL driver (v1.6.2 and later) wraps both the
// sentinel and the driver's error, so the driver mapper still answers, with
// the problems its constraint and code options give. Registered after this
// mapper, it would never see those errors. Sentinels without a driver error
// in the chain, as older drivers and other databases return them, fall
// through to this mapper.
package gormmap
