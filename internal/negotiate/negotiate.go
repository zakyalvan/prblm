// Package negotiate selects a response media type from an HTTP Accept header
// (RFC 9110, section 12.5.1).
package negotiate

import "strings"

const (
	// maxHeaderBytes bounds how much of an Accept header is parsed, so a
	// huge header costs no more than a normal one.
	maxHeaderBytes = 4 << 10
	// maxElements bounds how many elements of an Accept header are parsed,
	// valid or not.
	maxElements = 32
)

// mediaRange is one parsed element of an Accept header.
type mediaRange struct {
	typ, subtype string
	// q is the quality in thousandths, 0 to 1000.
	q int
}

// Select returns the offer the Accept header prefers, or "" when it accepts
// none of them. An empty header accepts anything, so the first offer wins.
//
// Each offer gets the quality of the most specific range that matches it
// (type/subtype, then type/*, then */*); a quality of 0 excludes it. The
// highest quality wins, and ties go to the earlier offer. Media types in the
// header compare case-insensitively, parameters other than the first q are
// ignored, and offers must be lower case.
//
// Select never fails: a malformed range, or one with a q outside the RFC 9110
// syntax, is dropped. Only the first 4 KiB and the first 32 elements of the
// header are parsed, so a huge header costs no more than a normal one; real
// clients send far less. A range cut by the size limit is dropped.
func Select(accept string, offers []string) string {
	if strings.TrimSpace(accept) == "" {
		accept = "*/*"
	}

	// The ranges live on the stack: parse never holds more than
	// maxElements, so Select does not allocate.
	var buf [maxElements]mediaRange

	ranges := parse(buf[:0], accept)

	best, bestQ := "", 0

	for _, offer := range offers {
		if q := quality(ranges, offer); q > bestQ {
			best, bestQ = offer, q
		}
	}

	return best
}

// parse appends the valid media ranges in accept, within the limits, to
// ranges and returns the result.
func parse(ranges []mediaRange, accept string) []mediaRange {
	if len(accept) > maxHeaderBytes {
		// The element cut by the limit is incomplete, and its missing part
		// could change its meaning, so it is dropped.
		head := accept[:maxHeaderBytes]
		accept = head[:max(strings.LastIndexByte(head, ','), 0)]
	}

	seen := 0

	for element := range strings.SplitSeq(accept, ",") {
		if seen == maxElements {
			break
		}

		seen++

		if r, ok := parseRange(element); ok {
			ranges = append(ranges, r)
		}
	}

	return ranges
}

// parseRange parses one media range with its parameters, and reports
// whether it is valid.
func parseRange(s string) (mediaRange, bool) {
	mediaType, params, _ := strings.Cut(s, ";")

	typ, subtype, ok := strings.Cut(strings.ToLower(strings.TrimSpace(mediaType)), "/")
	if !ok || !isToken(typ) || !isToken(subtype) || (typ == "*" && subtype != "*") {
		return mediaRange{}, false
	}

	r := mediaRange{typ: typ, subtype: subtype, q: 1000}

	for param := range strings.SplitSeq(params, ";") {
		name, value, _ := strings.Cut(param, "=")
		if !strings.EqualFold(strings.TrimSpace(name), "q") {
			continue
		}

		q, valid := parseQ(strings.TrimSpace(value))
		if !valid {
			return mediaRange{}, false
		}

		r.q = q

		break
	}

	return r, true
}

// parseQ parses a qvalue in thousandths:
//
//	qvalue = ( "0" [ "." 0*3DIGIT ] ) / ( "1" [ "." 0*3("0") ] )
func parseQ(s string) (int, bool) {
	whole, fraction, _ := strings.Cut(s, ".")
	if (whole != "0" && whole != "1") || len(fraction) > 3 {
		return 0, false
	}

	q := 0

	for i := range 3 {
		q *= 10

		if i < len(fraction) {
			c := fraction[i]
			if c < '0' || c > '9' {
				return 0, false
			}

			q += int(c - '0')
		}
	}

	switch {
	case whole == "0":
		return q, true
	case q == 0:
		return 1000, true
	default: // above 1
		return 0, false
	}
}

// isToken reports whether s is an RFC 9110 token.
func isToken(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if !isTokenChar(s[i]) {
			return false
		}
	}

	return true
}

// isTokenChar reports whether c is an RFC 9110 tchar.
func isTokenChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	default:
		return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
	}
}

// quality returns the quality the most specific range matching offer gives
// it, or -1 when no range matches.
func quality(ranges []mediaRange, offer string) int {
	typ, subtype, _ := strings.Cut(offer, "/")

	q, specificity := -1, 0

	for _, r := range ranges {
		if s := r.match(typ, subtype); s > specificity {
			q, specificity = r.q, s
		}
	}

	return q
}

// match returns how specifically r matches typ/subtype: 3 for the same type
// and subtype, 2 for typ/*, 1 for */*, and 0 when it does not match.
func (r mediaRange) match(typ, subtype string) int {
	switch {
	case r.typ == typ && r.subtype == subtype:
		return 3
	case r.typ == typ && r.subtype == "*":
		return 2
	case r.typ == "*" && r.subtype == "*":
		return 1
	default:
		return 0
	}
}
