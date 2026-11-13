package validatormap

import (
	"net/url"
	"strings"

	"github.com/go-playground/validator/v10"
)

// location returns where fe failed: a JSON Pointer in URI fragment form
// when the validator reports JSON names, or otherwise the field's namespace
// without the root struct name. Exactly one of them is non-empty.
//
// The validator reports JSON names only when a tag name function is
// registered; without one, Namespace and StructNamespace are equal. They are
// also equal when the JSON names coincide with the Go names, and then the
// field name is still correct. An error from validating a single variable
// has no namespace and points at the whole document.
func location(fe validator.FieldError) (pointer, field string) {
	ns, structNS := fe.Namespace(), fe.StructNamespace()
	if ns != "" && ns == structNS {
		// Without JSON names, a single segment is a field of an anonymous
		// struct: a root never comes without a field.
		return "", relative(ns, strings.ContainsAny(ns, ".["))
	}

	return pointerOf(relative(ns, firstSegment(ns) == firstSegment(structNS))), ""
}

// relative returns the namespace ns without its root struct name when it
// has one: "Order.items[0]" becomes "items[0]".
//
// The validator starts a namespace with the name of the root struct type,
// except for an anonymous struct type, which has no name. The root is the
// same in the namespace of JSON names and in that of Go names, while the
// field names differ, so the first segments of the two match only when
// there is a root. When the two namespaces are equal, there is no such
// signal: a namespace with a single segment is then a field of an anonymous
// struct, since a root never comes without a field. A nested field of an
// anonymous struct whose first JSON name equals its Go name is
// indistinguishable from a field of a named struct, and loses its first
// segment; validate named struct types to avoid it.
func relative(ns string, hasRoot bool) string {
	if !hasRoot || strings.HasPrefix(ns, "[") {
		return ns
	}

	i := strings.IndexAny(ns, ".[")
	if i < 0 {
		return ""
	}

	if ns[i] == '.' {
		return ns[i+1:]
	}

	return ns[i:]
}

// firstSegment returns the part of ns before the first "." or "[".
func firstSegment(ns string) string {
	if i := strings.IndexAny(ns, ".["); i >= 0 {
		return ns[:i]
	}

	return ns
}

// pointerOf returns the JSON Pointer, in URI fragment form, of the field at
// the namespace ns, which has no root struct name.
func pointerOf(ns string) string {
	var b strings.Builder

	b.WriteByte('#')

	for _, token := range tokens(ns) {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(escapeToken(token)))
	}

	return b.String()
}

// tokens splits a namespace without its root into the reference tokens of a
// JSON Pointer: names separated by "." and indexes or map keys in "[...]".
// A key may itself contain "]", ".", or "[", so it ends at the first "]"
// followed by the end, "." or "[".
func tokens(ns string) []string {
	var out []string

	for i := 0; i < len(ns); {
		switch ns[i] {
		case '.':
			i++
		case '[':
			end := keyEnd(ns, i+1)
			out = append(out, ns[i+1:end])
			i = end + 1
		default:
			end := len(ns)
			if j := strings.IndexAny(ns[i:], ".["); j >= 0 {
				end = i + j
			}

			out = append(out, ns[i:end])
			i = end
		}
	}

	return out
}

// keyEnd returns the index of the "]" that closes the key starting at from,
// or len(ns) when it is not closed.
func keyEnd(ns string, from int) int {
	for j := from; j < len(ns); j++ {
		if ns[j] != ']' {
			continue
		}

		if j+1 == len(ns) || ns[j+1] == '.' || ns[j+1] == '[' {
			return j
		}
	}

	return len(ns)
}

// escapeToken escapes a reference token as RFC 6901 requires: "~" becomes
// "~0" and "/" becomes "~1", in that order.
func escapeToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}
