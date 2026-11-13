package pgxmap

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kartaladev/prblm"
)

// entry is the default problem of a SQLSTATE code.
type entry struct {
	detail string
	status int
	// isRetryable marks codes whose problems carry a Retry-After header.
	isRetryable bool
}

// The entries shared by several codes.
var (
	retryConflict = entry{
		status:      http.StatusConflict,
		detail:      "The request conflicted with a concurrent change; retry it.",
		isRetryable: true,
	}
	unavailable = entry{
		status:      http.StatusServiceUnavailable,
		detail:      "The service is temporarily unavailable.",
		isRetryable: true,
	}
)

// defaults maps SQLSTATE codes to their default problems. It is never
// modified.
var defaults = map[string]entry{
	// Integrity constraint violations: the client sent conflicting or
	// incomplete data.
	"23505": {status: http.StatusConflict, detail: "The resource conflicts with an existing one."},
	"23503": {status: http.StatusConflict, detail: "The resource references, or is referenced by, another resource."},
	"23502": {status: http.StatusUnprocessableEntity, detail: "A required value is missing."},
	"23514": {status: http.StatusUnprocessableEntity, detail: "A value does not satisfy a constraint."},
	// Transaction conflicts: the same request may succeed when retried.
	"40001": retryConflict,
	"40P01": retryConflict,
	// Capacity: the database is overloaded or canceled a slow statement.
	"57014": unavailable,
	"53300": unavailable,
}

// problemFunc builds the problem for a PostgreSQL error, or returns nil to
// decline it.
type problemFunc = func(pe *pgconn.PgError) *prblm.Problem

// Option configures the mapper returned by [Mapper].
type Option func(*config)

type config struct {
	// constraints maps constraint names to their problem functions.
	constraints map[string]problemFunc
	// codes maps SQLSTATE codes to their problem functions.
	codes map[string]problemFunc
	// retryAfter is the Retry-After header of retryable problems.
	retryAfter prblm.Field
	// isDetailExposed is set by WithDatabaseDetail.
	isDetailExposed bool
}

// WithRetryAfter sets the Retry-After header of the default problems for
// retryable codes (40001, 40P01, 57014 and 53300), one second by default.
// The header carries whole seconds, so d is rounded down. Problems from
// [WithConstraint] and [WithCode] functions are used as they are; add the
// header there with prblm.WithHeader when they need it. WithRetryAfter
// panics when d is less than one second.
func WithRetryAfter(d time.Duration) Option {
	if d < time.Second {
		panic(fmt.Sprintf("pgxmap: WithRetryAfter needs at least one second, got %v", d))
	}

	return func(c *config) {
		c.retryAfter = retryAfterHeader(d)
	}
}

// WithDatabaseDetail puts the PostgreSQL error's detail text, when it has
// one, into the problem's detail instead of the generic sentence of its
// code.
//
// Warning: this loosens a safety default. The PostgreSQL detail contains row
// values and schema names, for example
// "Key (email)=(alice@example.com) already exists.", so use it only for
// internal APIs whose clients may see them. The message, hint, schema,
// table, column, data type and where fields are never used, with or without
// this option. Problems from [WithConstraint] and [WithCode] functions are
// used as they are.
func WithDatabaseDetail() Option {
	return func(c *config) {
		c.isDetailExposed = true
	}
}

// WithConstraint registers fn as the problem function for errors on the
// constraint with the given name, matched exactly and case-sensitively. It
// takes precedence over [WithCode] and the default SQLSTATE mapping for
// errors on that constraint, whatever their code; a nil result falls back to
// them. A later registration for the same name replaces an earlier one.
//
// fn must not keep or modify the error. WithConstraint panics when name is
// empty or fn is nil.
func WithConstraint(name string, fn func(pe *pgconn.PgError) *prblm.Problem) Option {
	if name == "" {
		panic("pgxmap: WithConstraint needs a constraint name")
	}

	if fn == nil {
		panic("pgxmap: WithConstraint needs a problem function")
	}

	return func(c *config) {
		c.constraints[name] = fn
	}
}

// WithCode registers fn as the problem function for the SQLSTATE code,
// including a code the default mapping does not list. It replaces the
// default problem of that code; a nil result falls back to the default, or
// to no match for an unlisted code. A later registration for the same code
// replaces an earlier one.
//
// fn must not keep or modify the error. WithCode panics when code is empty
// or fn is nil.
func WithCode(code string, fn func(pe *pgconn.PgError) *prblm.Problem) Option {
	if code == "" {
		panic("pgxmap: WithCode needs a SQLSTATE code")
	}

	if fn == nil {
		panic("pgxmap: WithCode needs a problem function")
	}

	return func(c *config) {
		c.codes[code] = fn
	}
}

// retryAfterHeader returns the Retry-After header for d, in whole seconds.
func retryAfterHeader(d time.Duration) prblm.Field {
	return prblm.WithHeader("Retry-After", strconv.FormatInt(int64(d/time.Second), 10))
}

// Mapper returns a mapper for PostgreSQL errors, configured by opts (see the
// package documentation for what it maps). Nil options are ignored.
//
// For the first [*pgconn.PgError] in the error chain, the mapper tries in
// order: the [WithConstraint] function for its constraint, the [WithCode]
// function for its code, and the default mapping of its code. The first
// problem wins. Without one, or without a PostgreSQL error in the chain, it
// reports no match.
//
// The options are applied once, here; the returned mapper does not change
// afterwards and is safe for concurrent use. A problem function that panics
// is not recovered by the mapper: the handler contains the panic, treats the
// mapper as not matching and reports the panic to its OnError hook.
func Mapper(opts ...Option) prblm.Mapper {
	cfg := config{
		constraints: make(map[string]problemFunc),
		codes:       make(map[string]problemFunc),
		retryAfter:  retryAfterHeader(time.Second),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return prblm.As(func(pe *pgconn.PgError) *prblm.Problem {
		// A typed nil stored in an error matches the type.
		if pe == nil {
			return nil
		}

		if p := lookup(cfg.constraints, pe.ConstraintName, pe); p != nil {
			return p
		}

		if p := lookup(cfg.codes, pe.Code, pe); p != nil {
			return p
		}

		return cfg.standard(pe)
	})
}

// lookup returns the problem of the function registered in fns under key,
// or nil when there is none or it declines pe.
func lookup(fns map[string]problemFunc, key string, pe *pgconn.PgError) *prblm.Problem {
	fn, ok := fns[key]
	if !ok {
		return nil
	}

	return fn(pe)
}

// standard returns the default problem for the code of pe, or nil for an
// unlisted code.
func (c *config) standard(pe *pgconn.PgError) *prblm.Problem {
	e, ok := defaults[pe.Code]
	if !ok {
		return nil
	}

	detail := e.detail
	if c.isDetailExposed && pe.Detail != "" {
		detail = pe.Detail
	}

	var retry prblm.Field
	if e.isRetryable {
		retry = c.retryAfter
	}

	return prblm.New(e.status, prblm.WithDetail(detail), retry)
}
