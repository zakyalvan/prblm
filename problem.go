package prblm

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"

	"github.com/kartaladev/prblm/internal/xmlenc"
)

// The names of the standard members, which extensions cannot use.
const (
	memberType     = "type"
	memberTitle    = "title"
	memberStatus   = "status"
	memberDetail   = "detail"
	memberInstance = "instance"
)

var (
	// ErrInvalidExtensionName is returned by [Problem.Set] for a name that
	// does not follow the extension name rule.
	ErrInvalidExtensionName = errors.New("prblm: invalid extension name")
	// ErrReservedExtensionName is returned by [Problem.Set] for a name that
	// equals a standard member name.
	ErrReservedExtensionName = errors.New("prblm: reserved extension name")
)

// Problem is an RFC 9457 problem details object.
//
// The standard members are plain fields. Empty members are omitted when the
// problem is encoded, and an empty Type means "about:blank": a problem with
// no meaning beyond its HTTP status, whose title is then the reason phrase of
// that status.
//
// Extension members are set with [Problem.Set] or [WithExt], which check
// their names, and are encoded at the top level next to the standard members.
// Header carries HTTP headers for the response, and a cause recorded with
// [WithCause] keeps the underlying error reachable for logging; neither is
// ever part of the document.
//
// A Problem is also an error; its message never includes the cause.
//
//nolint:errname // "problem" is the RFC 9457 term and the library's name.
type Problem struct {
	// Type is a URI reference that identifies the problem type.
	Type string
	// Title is a short, human-readable summary of the problem type.
	Title string
	// Detail is a human-readable explanation specific to this occurrence.
	Detail string
	// Instance is a URI reference that identifies this occurrence.
	Instance string
	// Header holds HTTP headers to send with the problem response, such as
	// Retry-After or WWW-Authenticate. It is never part of the document.
	Header http.Header
	// ext holds the extension members. It is unexported so that names set by
	// consumers always go through the check in Set and WithExt; names from a
	// decoded document are kept as received.
	ext map[string]any
	// cause is the error behind the problem. It is reachable through
	// [errors.Unwrap] and is never serialized.
	cause error
	// Status is the HTTP status code for this occurrence.
	Status int
}

// Field sets a member of a problem created with [New].
type Field func(*Problem)

// New returns a problem with the given status and fields, applied in order,
// so a later field replaces an earlier one. Nil fields are ignored, so a
// field chosen conditionally can be left nil. The standard members can also
// be set directly on the returned problem.
func New(status int, fields ...Field) *Problem {
	p := &Problem{Status: status}
	p.apply(fields)

	return p
}

// apply applies fields to p in order, skipping nil fields.
func (p *Problem) apply(fields []Field) {
	for _, f := range fields {
		if f != nil {
			f(p)
		}
	}
}

// WithType sets the type member, a URI reference that identifies the
// problem type. An empty type means about:blank (see [Problem.EffectiveType])
// and, like about:blank itself, is left out of the document.
func WithType(uri string) Field {
	return func(p *Problem) {
		p.Type = uri
	}
}

// WithTitle sets the title member, a short summary of the problem type that
// should not change from occurrence to occurrence.
func WithTitle(title string) Field {
	return func(p *Problem) {
		p.Title = title
	}
}

// WithDetail sets the detail member, an explanation specific to this
// occurrence. It is sent to the client as is, so it must not carry internal
// error text; record the error with [WithCause] instead.
func WithDetail(detail string) Field {
	return func(p *Problem) {
		p.Detail = detail
	}
}

// WithCause records the error behind the problem. The cause is reachable
// with [errors.Is] and [errors.As] but never appears in the encoded problem,
// so internal error text cannot reach the client.
func WithCause(err error) Field {
	return func(p *Problem) {
		p.cause = err
	}
}

// Error returns "Title: Detail", or only the title when there is no detail.
// An empty title is replaced by the reason phrase of the status. It is safe
// to call on a nil problem.
func (p *Problem) Error() string {
	if p == nil {
		return "<nil problem>"
	}

	// Unlike finalize, which only fills the wire title of about:blank
	// problems, an error message always needs text, whatever the type.
	title := cmp.Or(p.Title, http.StatusText(p.Status))
	if title == "" {
		title = "problem status " + strconv.Itoa(p.Status)
	}

	if p.Detail == "" {
		return title
	}

	return title + ": " + p.Detail
}

// Problem returns p, so that a *Problem is a [Describer] of itself and an
// error chain containing it resolves to it.
func (p *Problem) Problem() *Problem {
	return p
}

// Unwrap returns the cause recorded with [WithCause], or nil. It is safe to
// call on a nil problem.
func (p *Problem) Unwrap() error {
	if p == nil {
		return nil
	}

	return p.cause
}

// blankType is the problem type that RFC 9457 assumes when none is given. It
// means the problem has no semantics beyond its HTTP status.
const blankType = "about:blank"

// EffectiveType returns the problem type, which is "about:blank" when Type is
// empty. It is safe to call on a nil problem.
func (p *Problem) EffectiveType() string {
	if p == nil || p.Type == "" {
		return blankType
	}

	return p.Type
}

// finalize fills in the defaults a problem needs before it is written: for
// the about:blank type, an empty title becomes the HTTP reason phrase of the
// status, as RFC 9457 recommends.
func (p *Problem) finalize() {
	if p.Title == "" && p.EffectiveType() == blankType {
		p.Title = http.StatusText(p.Status)
	}
}

// WithHeader adds a header to send with the problem response.
func WithHeader(name, value string) Field {
	return func(p *Problem) {
		if p.Header == nil {
			p.Header = make(http.Header)
		}

		p.Header.Add(name, value)
	}
}

// WithExt sets an extension member, like [Problem.Set]. It panics when name
// is invalid, so that a mistake in a package-level problem shows up when the
// program starts rather than when a request fails.
func WithExt(name string, v any) Field {
	if err := checkExtensionName(name); err != nil {
		panic(err)
	}

	return func(p *Problem) {
		p.setExt(name, v)
	}
}

// Set sets the extension member name to v, replacing any previous value.
//
// A name must be at least three characters long, start with an ASCII letter,
// contain only ASCII letters, digits and underscores, and not be one of the
// standard member names (type, title, status, detail, instance). These are the
// names RFC 9457 recommends; they are also valid XML element names. Set
// returns an error wrapping [ErrInvalidExtensionName] or
// [ErrReservedExtensionName] for any other name and leaves the problem
// unchanged.
func (p *Problem) Set(name string, v any) error {
	if err := checkExtensionName(name); err != nil {
		return err
	}

	p.setExt(name, v)

	return nil
}

// Ext returns the value of the extension member name and whether it is set.
// It is safe to call on a nil problem.
func (p *Problem) Ext(name string) (any, bool) {
	if p == nil {
		return nil, false
	}

	v, ok := p.ext[name]

	return v, ok
}

// clone returns a copy of p that shares no map with it. Extension values
// themselves are not copied.
func (p *Problem) clone() *Problem {
	c := *p
	c.ext = maps.Clone(p.ext)
	c.Header = p.Header.Clone()

	return &c
}

func (p *Problem) setExt(name string, v any) {
	if p.ext == nil {
		p.ext = make(map[string]any)
	}

	p.ext[name] = v
}

// checkExtensionName reports why name cannot be an extension member, or nil
// when it can.
func checkExtensionName(name string) error {
	switch name {
	case memberType, memberTitle, memberStatus, memberDetail, memberInstance:
		return fmt.Errorf("%w: %q", ErrReservedExtensionName, name)
	}

	if !isExtensionName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidExtensionName, name)
	}

	return nil
}

// isExtensionName reports whether name follows the extension name rule: at
// least three characters, an ASCII letter first, then ASCII letters, digits
// or underscores. The character rule is the one XML element names follow
// (see xmlenc.IsName), so every extension can be encoded as XML.
func isExtensionName(name string) bool {
	return len(name) >= 3 && xmlenc.IsName(name)
}
