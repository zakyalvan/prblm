package prblm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// problemJSON is the wire form of the standard members. Its field order is
// the member order of the encoded document, so the output is deterministic.
type problemJSON struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// MarshalJSON encodes the problem as an application/problem+json document.
// Members are written in a fixed order (type, title, status, detail,
// instance) and empty members are omitted. The type is also omitted when it
// is "about:blank", which is what a client assumes for a missing type.
// Extension members follow at the top level, sorted by name.
//
// Extension values are encoded with the [encoding/json] rules, including
// struct tags. A value that cannot be encoded (a channel, NaN, a cycle) makes
// MarshalJSON fail rather than produce a partial document.
//
// It has a value receiver so that both Problem and *Problem encode the same
// way.
func (p Problem) MarshalJSON() ([]byte, error) {
	std, err := json.Marshal(problemJSON{
		Type:     p.wireType(),
		Title:    p.Title,
		Status:   p.Status,
		Detail:   p.Detail,
		Instance: p.Instance,
	})
	if err != nil || len(p.ext) == 0 {
		return std, err
	}

	// encoding/json writes map keys sorted, which gives the extension order.
	ext, err := json.Marshal(p.ext)
	if err != nil {
		return nil, fmt.Errorf("prblm: encoding extensions: %w", err)
	}

	if len(std) == len("{}") {
		return ext, nil
	}

	// Join {"title":…} and {"balance":…} into {"title":…,"balance":…}.
	std = append(std[:len(std)-1], ',')

	return append(std, ext[1:]...), nil
}

// wireType is the type as written to a document: empty for "about:blank",
// which is what a client assumes for a missing type.
func (p Problem) wireType() string {
	if p.Type == blankType {
		return ""
	}

	return p.Type
}

// UnmarshalJSON decodes an application/problem+json document leniently: a
// standard member whose value has the wrong type is ignored, and members
// other than the standard ones are kept as extensions. Extension numbers are
// decoded as [json.Number], so no precision is lost. Extension names from a
// decoded document are not validated, since they come from another party.
// Input that is not a single JSON object is an error.
func (p *Problem) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var members map[string]any
	if err := dec.Decode(&members); err != nil {
		return fmt.Errorf("prblm: decoding problem: %w", err)
	}

	if members == nil {
		return errors.New("prblm: decoding problem: document is null")
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("prblm: decoding problem: data after the document")
	}

	var decoded Problem

	for name, v := range members {
		switch name {
		case memberType:
			decoded.Type, _ = v.(string)
		case memberTitle:
			decoded.Title, _ = v.(string)
		case memberStatus:
			decoded.Status = decodeStatus(v)
		case memberDetail:
			decoded.Detail, _ = v.(string)
		case memberInstance:
			decoded.Instance, _ = v.(string)
		default:
			decoded.setExt(name, v)
		}
	}

	*p = decoded

	return nil
}

// decodeStatus returns v as a status code, or 0 when v is not an integer.
func decodeStatus(v any) int {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}

	status, err := strconv.Atoi(n.String())
	if err != nil {
		return 0
	}

	return status
}
