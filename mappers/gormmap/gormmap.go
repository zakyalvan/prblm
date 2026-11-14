package gormmap

import (
	"context"
	"net/http"

	"gorm.io/gorm"

	"github.com/kartaladev/prblm"
)

// The generic details of the default problems. They never carry database
// values or constraint names. The last two are those pgxmap gives SQLSTATE
// 23503 and 23514, so a client gets the same problem whichever mapper
// answers.
const (
	detailNotFound   = "The requested resource was not found."
	detailConflict   = "The resource conflicts with an existing one."
	detailReference  = "The resource references, or is referenced by, another resource."
	detailConstraint = "A value does not satisfy a constraint."
)

// Option configures the mapper returned by [Mapper].
type Option func(*config)

// config holds the problem function of each sentinel.
type config struct {
	recordNotFound          func(err error) *prblm.Problem
	duplicatedKey           func(err error) *prblm.Problem
	foreignKeyViolated      func(err error) *prblm.Problem
	checkConstraintViolated func(err error) *prblm.Problem
}

// WithRecordNotFound replaces the problem for an error matching
// [gorm.ErrRecordNotFound]. fn receives the error being resolved; returning
// nil declines it, so later mappers and finally the handler's fallback get
// it. A nil fn keeps the default 404 problem.
func WithRecordNotFound(fn func(err error) *prblm.Problem) Option {
	return func(c *config) {
		override(&c.recordNotFound, fn)
	}
}

// WithDuplicatedKey replaces the problem for an error matching
// [gorm.ErrDuplicatedKey]. fn receives the error being resolved; returning
// nil declines it. A nil fn keeps the default 409 problem.
func WithDuplicatedKey(fn func(err error) *prblm.Problem) Option {
	return func(c *config) {
		override(&c.duplicatedKey, fn)
	}
}

// WithForeignKeyViolated replaces the problem for an error matching
// [gorm.ErrForeignKeyViolated]. fn receives the error being resolved;
// returning nil declines it. A nil fn keeps the default 409 problem.
func WithForeignKeyViolated(fn func(err error) *prblm.Problem) Option {
	return func(c *config) {
		override(&c.foreignKeyViolated, fn)
	}
}

// WithCheckConstraintViolated replaces the problem for an error matching
// [gorm.ErrCheckConstraintViolated]. fn receives the error being resolved;
// returning nil declines it. A nil fn keeps the default 422 problem.
func WithCheckConstraintViolated(fn func(err error) *prblm.Problem) Option {
	return func(c *config) {
		override(&c.checkConstraintViolated, fn)
	}
}

// override replaces *dst with fn, unless fn is nil.
func override(dst *func(error) *prblm.Problem, fn func(error) *prblm.Problem) {
	if fn != nil {
		*dst = fn
	}
}

// Mapper returns a mapper for gorm's sentinel errors, configured by opts
// (see the package documentation for what it maps). Nil options are ignored.
//
// The options are applied once, here; the returned mapper does not change
// afterwards and is safe for concurrent use. An override function that
// panics is not recovered by the mapper: the handler contains the panic,
// treats the mapper as not matching and reports the panic to its OnError
// hook.
func Mapper(opts ...Option) prblm.Mapper {
	cfg := config{
		recordNotFound:          problem(http.StatusNotFound, detailNotFound),
		duplicatedKey:           problem(http.StatusConflict, detailConflict),
		foreignKeyViolated:      problem(http.StatusConflict, detailReference),
		checkConstraintViolated: problem(http.StatusUnprocessableEntity, detailConstraint),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cases := []prblm.Mapper{
		prblm.Is(gorm.ErrRecordNotFound, cfg.recordNotFound),
		prblm.Is(gorm.ErrDuplicatedKey, cfg.duplicatedKey),
		prblm.Is(gorm.ErrForeignKeyViolated, cfg.foreignKeyViolated),
		prblm.Is(gorm.ErrCheckConstraintViolated, cfg.checkConstraintViolated),
	}

	return prblm.MapperFunc(func(ctx context.Context, err error) *prblm.Problem {
		for _, c := range cases {
			if p := c.MapError(ctx, err); p != nil {
				return p
			}
		}

		return nil
	})
}

// problem returns a problem function building a new problem with status and
// detail on every call, so no two responses share one.
func problem(status int, detail string) func(error) *prblm.Problem {
	return func(error) *prblm.Problem {
		return prblm.New(status, prblm.WithDetail(detail))
	}
}
