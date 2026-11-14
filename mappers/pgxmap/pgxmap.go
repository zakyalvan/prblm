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
	// Capacity: the database is overloaded.
	"53300": unavailable,
	// Query canceled, mostly by statement_timeout: pgx surfaces a canceled
	// request context as a context error, not as this code. A retry would
	// most likely time out again, so there is no Retry-After; 504 matches
	// the core's mapping of context.DeadlineExceeded.
	"57014": {status: http.StatusGatewayTimeout, detail: "The request took too long to complete."},
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
// retryable codes (40001, 40P01 and 53300), one second by default.
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
// code must be a SQLSTATE code as PostgreSQL reports it: five digits or
// upper-case ASCII letters, such as "23505" or "40P01". Any other code would
// never match, so the function would silently never run.
//
// fn must not keep or modify the error. WithCode panics when code is not a
// SQLSTATE code or fn is nil.
func WithCode(code string, fn func(pe *pgconn.PgError) *prblm.Problem) Option {
	if !isSQLState(code) {
		panic(fmt.Sprintf("pgxmap: WithCode needs a SQLSTATE code of five digits or upper-case letters, got %q", code))
	}

	if fn == nil {
		panic("pgxmap: WithCode needs a problem function")
	}

	return func(c *config) {
		c.codes[code] = fn
	}
}

// isSQLState reports whether code has the form of a SQLSTATE code as
// PostgreSQL reports it: five characters, each a digit or an upper-case
// ASCII letter.
func isSQLState(code string) bool {
	if len(code) != 5 {
		return false
	}

	for _, c := range []byte(code) {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return false
		}
	}

	return true
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
