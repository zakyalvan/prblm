package prblm

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/kartaladev/prblm/internal/xmlenc"
)

// xmlNamespace is the namespace of an application/problem+xml document
// (RFC 9457, Appendix B).
const xmlNamespace = "urn:ietf:rfc:7807"

// MarshalXML encodes the problem as an application/problem+xml document
// (RFC 9457, Appendix B): a problem element in the urn:ietf:rfc:7807
// namespace holding the members in the same order and with the same
// omissions as [Problem.MarshalJSON], then the extensions sorted by name.
//
// Extension values have the same structure as in JSON, because they are
// encoded with the [encoding/json] rules first: objects become child elements
// sorted by key, array items become <i> elements and null becomes an empty
// element. A value JSON cannot encode, or an object key or extension name
// that is not an ASCII letter followed by ASCII letters, digits or
// underscores, makes MarshalXML fail.
func (p Problem) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	start := xml.StartElement{Name: xml.Name{Space: xmlNamespace, Local: "problem"}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}

	status := ""
	if p.Status != 0 {
		status = strconv.Itoa(p.Status)
	}

	for _, m := range []struct{ name, value string }{
		{memberType, p.wireType()},
		{memberTitle, p.Title},
		{memberStatus, status},
		{memberDetail, p.Detail},
		{memberInstance, p.Instance},
	} {
		if m.value == "" {
			continue
		}

		if err := xmlenc.Encode(e, m.name, m.value); err != nil {
			return err
		}
	}

	for _, name := range slices.Sorted(maps.Keys(p.ext)) {
		if err := xmlenc.Encode(e, name, p.ext[name]); err != nil {
			return fmt.Errorf("prblm: encoding extension %q: %w", name, err)
		}
	}

	return e.EncodeToken(start.End())
}

// marshalXML encodes p as a complete XML document, with the XML declaration.
func marshalXML(p *Problem) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(xml.Header)

	if err := xml.NewEncoder(&buf).Encode(p); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// maxXMLDepth bounds element nesting when decoding, as encoding/xml bounds
// its own Unmarshal, so a hostile document cannot recurse without limit.
const maxXMLDepth = 10000

// errXMLTooDeep is returned for a document nested beyond maxXMLDepth.
var errXMLTooDeep = errors.New("prblm: decoding problem: elements nested too deeply")

// UnmarshalXML decodes an application/problem+xml document leniently, like
// [Problem.UnmarshalJSON]: a standard member that is not plain text, or a
// status that is not an integer, is ignored, and other members are kept as
// extensions. XML has no types, so extension values come back as text,
// lists (elements holding only <i> items) and objects (maps). Text is kept
// as written, except that the status may be surrounded by whitespace;
// attributes and comments are ignored. An element other than problem is an
// error; its namespace is not checked.
func (p *Problem) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if start.Name.Local != "problem" {
		return fmt.Errorf("prblm: decoding problem: root element is %q, not problem", start.Name.Local)
	}

	root, err := readNode(d, 1)
	if err != nil {
		return err
	}

	var decoded Problem

	for _, member := range root.children {
		switch member.name {
		case memberType:
			decoded.Type, _ = member.node.text()
		case memberTitle:
			decoded.Title, _ = member.node.text()
		case memberStatus:
			if text, ok := member.node.text(); ok {
				decoded.Status = decodeStatus(json.Number(strings.TrimSpace(text)))
			}
		case memberDetail:
			decoded.Detail, _ = member.node.text()
		case memberInstance:
			decoded.Instance, _ = member.node.text()
		default:
			decoded.setExt(member.name, member.node.value())
		}
	}

	*p = decoded

	return nil
}

// xmlNode is a decoded element: its text and its child elements.
type xmlNode struct {
	chars    strings.Builder
	children []xmlChild
}

// xmlChild is a named child element.
type xmlChild struct {
	name string
	node *xmlNode
}

// readNode reads the content of the element whose start was just read, up
// to and including its end.
func readNode(d *xml.Decoder, depth int) (*xmlNode, error) {
	if depth > maxXMLDepth {
		return nil, errXMLTooDeep
	}

	n := &xmlNode{}

	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("prblm: decoding problem: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			child, err := readNode(d, depth+1)
			if err != nil {
				return nil, err
			}

			n.children = append(n.children, xmlChild{name: t.Name.Local, node: child})
		case xml.CharData:
			// The text of an element with children is never read.
			if len(n.children) == 0 {
				n.chars.Write(t)
			}
		case xml.EndElement:
			return n, nil
		}
	}
}

// text returns the text of n, and reports false when n has child elements.
func (n *xmlNode) text() (string, bool) {
	if len(n.children) > 0 {
		return "", false
	}

	return n.chars.String(), true
}

// value returns n as an extension value: its text when it has no child
// elements, a list when every child is an <i> item, and otherwise an object
// whose later duplicate keys win.
func (n *xmlNode) value() any {
	if text, ok := n.text(); ok {
		return text
	}

	isList := true

	for _, c := range n.children {
		if c.name != "i" {
			isList = false

			break
		}
	}

	if isList {
		items := make([]any, 0, len(n.children))
		for _, c := range n.children {
			items = append(items, c.node.value())
		}

		return items
	}

	object := make(map[string]any, len(n.children))
	for _, c := range n.children {
		object[c.name] = c.node.value()
	}

	return object
}
