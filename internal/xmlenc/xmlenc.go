// Package xmlenc encodes values as XML elements following RFC 9457
// Appendix B.
package xmlenc

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// ErrInvalidName is returned for a name that is not an ASCII letter followed
// by ASCII letters, digits or underscores (see [IsName]).
var ErrInvalidName = errors.New("xmlenc: invalid element name")

// Encode writes value to e as the element name, following RFC 9457
// Appendix B: objects become child elements sorted by key, array items
// become <i> elements, null becomes an empty element, and other values
// become text.
//
// The value goes through encoding/json first, so its XML has the same
// structure as its JSON: json tags and options apply, and a value JSON cannot
// encode is an error. Names, including object keys, must be an ASCII letter
// followed by ASCII letters, digits or underscores; any other name is an
// error wrapping [ErrInvalidName].
//
// On error, e may hold part of the element, so the caller must discard what
// was written.
func Encode(e *xml.Encoder, name string, value any) error {
	var tree any

	b, err := json.Marshal(value)
	if err == nil {
		// json.Marshal produced b and validated it, so decoding cannot fail;
		// decodeTree still reports it, should that ever change.
		tree, err = decodeTree(b)
	}

	if err != nil {
		return err
	}

	return walk(e, name, tree)
}

// decodeTree decodes the JSON document b into maps, slices and scalars,
// keeping numbers as json.Number so they are written exactly as encoded.
func decodeTree(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()

	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}

	return tree, nil
}

// walk writes the decoded JSON value v as the element name.
func walk(e *xml.Encoder, name string, v any) error {
	if !IsName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}

	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}

	if err := children(e, v); err != nil {
		return err
	}

	return e.EncodeToken(start.End())
}

// children writes the content of the element holding v.
func children(e *xml.Encoder, v any) error {
	switch v := v.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			if err := walk(e, key, v[key]); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := walk(e, "i", item); err != nil {
				return err
			}
		}
	case string:
		return e.EncodeToken(xml.CharData(v))
	case json.Number:
		return e.EncodeToken(xml.CharData(v))
	case bool:
		return e.EncodeToken(xml.CharData(strconv.FormatBool(v)))
	}

	// nil: an empty element.
	return nil
}

// IsName reports whether name is an ASCII letter followed by ASCII letters,
// digits or underscores. Such a name is a valid XML element name and a valid
// JSON member name that needs no escaping. The rule is stricter than XML on
// purpose: it is the RFC 9457 extension name rule, so names such as
// "file.name" that XML allows are rejected, as they are for extensions.
func IsName(name string) bool {
	if name == "" || !isLetter(name[0]) {
		return false
	}

	for i := 1; i < len(name); i++ {
		if !isNameChar(name[i]) {
			return false
		}
	}

	return true
}

// isNameChar reports whether c may follow the first character of a name.
func isNameChar(c byte) bool {
	return isLetter(c) || ('0' <= c && c <= '9') || c == '_'
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}
