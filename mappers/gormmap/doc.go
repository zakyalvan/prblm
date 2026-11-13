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
//     conflicts with an existing one.". No constraint name or value is
//     included.
//
// [WithRecordNotFound] and [WithDuplicatedKey] replace either problem. Every
// other error, including other gorm sentinels such as
// [gorm.ErrInvalidTransaction], is left to later mappers and finally to the
// handler's safe 500 fallback. The original error still reaches the
// handler's OnError hook, which is where to log it.
//
// # Duplicate keys need TranslateError
//
// gorm returns [gorm.ErrDuplicatedKey] only when it translates driver
// errors, which is off by default:
//
//	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
//
// Without it, a unique violation reaches the handler as the driver's own
// error, which this mapper does not match. Register a mapper for that driver
// after this one, for example the PostgreSQL mapper of
// github.com/kartaladev/prblm/mappers/pgxmap:
//
//	h := prblm.NewHandler(prblm.WithMappers(gormmap.Mapper(), pgxmap.Mapper()))
package gormmap
