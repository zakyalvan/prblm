package prblm

import (
	"fmt"
	"strings"
)

// Type defines a problem type once, in one place: its sub-type under the
// handler's namespace, and the status, title and description every problem
// of that type shares. Create it with [DefineType], usually as a
// package-level variable, and build problems from it with [Type.New].
//
// A Type cannot change after it is defined, so it is safe to share between
// goroutines and between handlers.
type Type struct {
	subtype     string
	title       string
	description string
	status      int
}

// DefineType defines a problem type.
//
//   - subtype names the type under the namespace (see [WithNamespace]): one
//     or more "/"-separated segments of lowercase letters and digits with
//     single "-", "_" or "." between them, as in
//     "contracts/blacklisted-supplier".
//   - status is the HTTP status of its problems, from 400 to 599.
//   - title is a short summary that does not change between occurrences. It
//     must not be blank.
//   - description explains the type to people reading its documentation. It
//     may be empty.
//
// DefineType panics on an invalid sub-type, status or title, naming the
// value. Definitions are meant for package-level variables, so a mistake
// surfaces as soon as the program starts.
func DefineType(subtype string, status int, title, description string) *Type {
	if err := validateType(subtype, status, title); err != nil {
		panic("prblm: DefineType: " + err.Error())
	}

	return &Type{subtype: subtype, status: status, title: title, description: description}
}

// validateType checks the parts of a definition and returns an error naming
// the first invalid one: the sub-type, the status (400–599) or a blank
// title. It is shared by DefineType and WithTypes, which re-checks every
// registered definition, because a Type can be built without DefineType.
func validateType(subtype string, status int, title string) error {
	switch {
	case !isSubtype(subtype):
		return fmt.Errorf("invalid sub-type: %q", subtype)
	case !isErrorStatus(status):
		return fmt.Errorf("invalid status: %d, want 400-599", status)
	case strings.TrimSpace(title) == "":
		return fmt.Errorf("blank title: %q", title)
	}

	return nil
}

// New returns a new problem of type t: its sub-type as the type, its status
// and its title, followed by fields, applied in order as in [New]. A
// handler with a namespace resolves the sub-type when it answers the
// problem. Each call returns a fresh problem, which the caller may modify.
//
// On a nil Type, for example a definition that was never assigned, or on a
// zero-value &Type{} that skipped DefineType, New returns the safe 500
// problem instead of panicking, and ignores fields, so a detail meant for
// the defined type never reaches the client.
//
// Use it in a mapper:
//
//	prblm.Is(ErrBlacklisted, func(error) *prblm.Problem {
//		return BlacklistedSupplier.New(prblm.WithDetail("Supplier 42 is blacklisted."))
//	})
func (t *Type) New(fields ...Field) *Problem {
	// DefineType never accepts an empty sub-type, so one marks a Type that
	// was not defined.
	if t == nil || t.subtype == "" {
		return fallbackProblem()
	}

	p := &Problem{Type: t.subtype, Title: t.title, Status: t.status}
	p.apply(fields)

	return p
}

// Subtype returns the sub-type of t, or "" for a nil Type.
func (t *Type) Subtype() string {
	if t == nil {
		return ""
	}

	return t.subtype
}

// Status returns the HTTP status of the problems of t, or 0 for a nil Type.
func (t *Type) Status() int {
	if t == nil {
		return 0
	}

	return t.status
}

// Title returns the title of the problems of t, or "" for a nil Type.
func (t *Type) Title() string {
	if t == nil {
		return ""
	}

	return t.title
}

// Description returns the description of t, for its documentation, or ""
// for a nil Type.
func (t *Type) Description() string {
	if t == nil {
		return ""
	}

	return t.description
}

// WithTypes registers type definitions with the handler. It may be used
// several times; nil definitions are skipped.
//
// Registered types need a namespace, set with [WithNamespace] in any order.
// [NewHandler] panics when a sub-type is registered twice, including the
// same definition twice, when types are registered without a namespace, or
// when a definition is one [DefineType] would reject, such as a zero-value
// &Type{}, so a broken definition is never documented. WithTypes with no
// definitions registers nothing and needs no namespace.
func WithTypes(types ...*Type) Option {
	return func(h *Handler) {
		for _, t := range types {
			if t != nil {
				h.definitions = append(h.definitions, t)
			}
		}
	}
}
