package prblm

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// WithNamespace sets the application's namespace for problem types: an
// absolute http or https URI with a host (https://kartala.id/probs), or an
// absolute path (/v1/api/probs). One trailing slash is ignored.
//
// Without a namespace, types are written exactly as problems carry them.
//
// The namespace must have a path other than "/", every segment non-empty
// and needing no escaping, and no ".", "..", query, fragment or user info.
// [NewHandler] panics on an invalid namespace, including an empty one: it is
// a startup misconfiguration, like an invalid pattern passed to
// regexp.MustCompile.
func WithNamespace(ns string) Option {
	return func(h *Handler) {
		h.namespace = mustParseNamespace(ns)
	}
}

// ErrInvalidType is wrapped by the note the [OnError] hook receives when a
// handler with a namespace (see [WithNamespace]) met a relative type that is
// not a valid sub-type, and sent about:blank instead. A client would resolve
// such a type against the request URL, which leads nowhere. Like
// [ErrInvalidStatus], it is a mistake in a mapper, describer or enricher,
// so a hook can tell it apart from the error being answered with errors.Is.
var ErrInvalidType = errors.New("prblm: problem type is not a valid sub-type")

// isKeptType reports whether typ is never resolved against a namespace:
// empty (about:blank), an absolute path, or a URI with a scheme, which
// includes about:blank itself.
func isKeptType(typ string) bool {
	return typ == "" || strings.HasPrefix(typ, "/") || hasScheme(typ)
}

// hasScheme reports whether s starts with a URI scheme followed by ":"
// (RFC 3986, section 3.1: a letter, then letters, digits, "+", "-" or ".").
// It scans bytes because it runs for every problem with a type.
func hasScheme(s string) bool {
	if s == "" || !isLetter(s[0]) {
		return false
	}

	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case c == ':':
			return true
		case !isLetter(c) && !isDigit(c) && c != '+' && c != '-' && c != '.':
			return false
		}
	}

	return false
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

// parseNamespace checks ns as a namespace and returns it without its
// trailing slash, or an error naming what is wrong.
func parseNamespace(ns string) (string, error) {
	u, err := url.Parse(ns)
	if err != nil {
		return "", errors.New("not a URI reference")
	}

	if u.Scheme != "" {
		err = checkAbsoluteURI(u)
	} else if !strings.HasPrefix(ns, "/") || strings.HasPrefix(ns, "//") {
		// A relative path, or a network-path reference ("//host/path").
		err = errors.New("must be an absolute http or https URI or an absolute path")
	}

	if err != nil {
		return "", err
	}

	if err := checkQueryAndFragment(ns); err != nil {
		return "", err
	}

	if err := checkSegments(u); err != nil {
		return "", err
	}

	return strings.TrimSuffix(ns, "/"), nil
}

// checkAbsoluteURI checks a namespace with a scheme: http or https, with a
// host and without user info.
func checkAbsoluteURI(u *url.URL) error {
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return errors.New("scheme must be http or https")
	case u.Host == "":
		return errors.New("absolute URI needs a host")
	case u.User != nil:
		return errors.New("user info is not allowed")
	}

	return nil
}

// checkQueryAndFragment rejects a query or a fragment, which would swallow
// the sub-type appended after them. It checks the raw namespace, because a
// literal "?" or "#" always starts one, while the parsed URL cannot tell an
// empty fragment ("/probs#") from none.
func checkQueryAndFragment(ns string) error {
	switch {
	case strings.Contains(ns, "?"):
		return errors.New("query is not allowed")
	case strings.Contains(ns, "#"):
		return errors.New("fragment is not allowed")
	}

	return nil
}

// checkSegments checks the path of a namespace: not "/", every segment
// non-empty except a trailing one, none "." or "..", and nothing that needs
// escaping, so the path is the same however a client encodes it.
func checkSegments(u *url.URL) error {
	if u.EscapedPath() != u.Path {
		return errors.New("path must not need escaping")
	}

	path := strings.TrimSuffix(u.Path, "/")
	if path == "" {
		return errors.New("path must not be empty or /")
	}

	for segment := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		switch segment {
		case "":
			return errors.New("path must not have empty segments")
		case ".", "..":
			return errors.New("path must not have . or .. segments")
		}
	}

	return nil
}

// isSubtype reports whether s is a sub-type: one or more segments joined by
// "/", each of lowercase letters and digits with single "-", "_" or "."
// between them, as in "contracts/blacklisted-supplier". The grammar leaves no
// room for "..", "//", a query, a fragment or anything that needs escaping,
// so a sub-type appended to a namespace stays under it.
//
// It scans bytes rather than using a regexp because it runs for every
// problem with a relative type, and must not allocate.
func isSubtype(s string) bool {
	// needAlnum holds at the start of a segment and after a separator.
	needAlnum := true

	for i := range len(s) {
		c := s[i]

		switch {
		case 'a' <= c && c <= 'z' || isDigit(c):
			needAlnum = false
		case needAlnum:
			return false
		case c == '/' || c == '-' || c == '_' || c == '.':
			needAlnum = true
		default:
			return false
		}
	}

	return !needAlnum
}

// mustParseNamespace returns ns checked by parseNamespace, and panics with a
// message naming WithNamespace when it is invalid.
func mustParseNamespace(ns string) string {
	parsed, err := parseNamespace(ns)
	if err != nil {
		panic(fmt.Sprintf("prblm: WithNamespace: %v: %q", err, ns))
	}

	return parsed
}
