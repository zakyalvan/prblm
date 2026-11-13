package prblm

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// xmlNamespace is the namespace of an application/problem+xml document
// (RFC 9457, Appendix B).
const xmlNamespace = "urn:ietf:rfc:7807"

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
