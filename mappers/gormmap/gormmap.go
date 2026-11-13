package gormmap

import (
	"context"
	"net/http"

	"gorm.io/gorm"

	"github.com/kartaladev/prblm"
)

// The generic details of the default problems. They never carry database
// values or constraint names.
const (
	detailNotFound = "The requested resource was not found."
	detailConflict = "The resource conflicts with an existing one."
)

// Option configures the mapper returned by [Mapper].
type Option func(*config)

// config holds the problem function of each sentinel.
type config struct {
	recordNotFound func(err error) *prblm.Problem
	duplicatedKey  func(err error) *prblm.Problem
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
	cfg := config{recordNotFound: recordNotFound, duplicatedKey: duplicatedKey}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cases := []prblm.Mapper{
		prblm.Is(gorm.ErrRecordNotFound, cfg.recordNotFound),
		prblm.Is(gorm.ErrDuplicatedKey, cfg.duplicatedKey),
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

func recordNotFound(error) *prblm.Problem {
	return prblm.New(http.StatusNotFound, prblm.WithDetail(detailNotFound))
}

func duplicatedKey(error) *prblm.Problem {
	return prblm.New(http.StatusConflict, prblm.WithDetail(detailConflict))
}
