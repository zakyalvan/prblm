package sqlmap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"

	"github.com/kartaladev/prblm"
)

// The generic details of the default problems. They never mention SQL.
const (
	detailNotFound    = "The requested resource was not found."
	detailUnavailable = "The service is temporarily unavailable."
)

// Option configures the mapper returned by [Mapper].
type Option func(*config)

// config holds the problem function of each case.
type config struct {
	notFound    func(err error) *prblm.Problem
	unavailable func(err error) *prblm.Problem
}

// WithNotFound replaces the problem for an error matching [sql.ErrNoRows].
// fn receives the error being resolved; returning nil declines it, so later
// mappers and finally the handler's fallback get it. A nil fn keeps the
// default 404 problem.
func WithNotFound(fn func(err error) *prblm.Problem) Option {
	return func(c *config) {
		override(&c.notFound, fn)
	}
}

// WithUnavailable replaces the problem for an error matching
// [driver.ErrBadConn] or [sql.ErrConnDone]. fn receives the error being
// resolved; returning nil declines it. A nil fn keeps the default 503
// problem.
func WithUnavailable(fn func(err error) *prblm.Problem) Option {
	return func(c *config) {
		override(&c.unavailable, fn)
	}
}

// override replaces *dst with fn, unless fn is nil.
func override(dst *func(error) *prblm.Problem, fn func(error) *prblm.Problem) {
	if fn != nil {
		*dst = fn
	}
}

// Mapper returns a mapper for database/sql errors, configured by opts (see
// the package documentation for what it maps). Nil options are ignored.
//
// The options are applied once, here; the returned mapper does not change
// afterwards and is safe for concurrent use. An override function that
// panics is not recovered by the mapper: the handler contains the panic,
// treats the mapper as not matching and reports the panic to its OnError
// hook.
func Mapper(opts ...Option) prblm.Mapper {
	cfg := config{notFound: notFound, unavailable: unavailable}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cases := []prblm.Mapper{
		prblm.Is(sql.ErrNoRows, cfg.notFound),
		prblm.Is(driver.ErrBadConn, cfg.unavailable),
		prblm.Is(sql.ErrConnDone, cfg.unavailable),
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

func notFound(error) *prblm.Problem {
	return prblm.New(http.StatusNotFound, prblm.WithDetail(detailNotFound))
}

func unavailable(error) *prblm.Problem {
	return prblm.New(http.StatusServiceUnavailable, prblm.WithDetail(detailUnavailable))
}
