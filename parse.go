package prblm

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

// maxParseBytes bounds how much of a response body Parse reads. Problem
// documents are small; the bound keeps a misbehaving server from making a
// client buffer an unbounded body.
const maxParseBytes = 1 << 20

// errTooLarge is returned by Parse for a body larger than maxParseBytes.
var errTooLarge = fmt.Errorf("prblm: problem body larger than %d bytes", maxParseBytes)

// ErrNotProblem is returned by [Parse] for a response that is not a problem
// document: its Content-Type is neither application/problem+json nor
// application/problem+xml.
var ErrNotProblem = errors.New("prblm: response is not a problem")

// Parse decodes the problem in res, choosing JSON or XML by its
// Content-Type, for clients and tests of services that answer with problem
// details. Decoding is lenient (see [Problem.UnmarshalJSON] and
// [Problem.UnmarshalXML]), and the problem's status is the one in the
// document, not the response status.
//
// Parse consumes and always closes the body. It reads at most 1 MiB: problem
// documents are small, and the bound keeps a misbehaving server from making
// the client buffer an unbounded body.
//
// It returns an error wrapping [ErrNotProblem] when the response is not a
// problem document, the read error when the body cannot be read, an error
// when the body is larger than 1 MiB, and a decode error when the body is not
// a valid document.
func Parse(res *http.Response) (*Problem, error) {
	if res == nil {
		return nil, errors.New("prblm: parsing a nil response")
	}

	if res.Body == nil {
		return nil, errors.New("prblm: parsing a response without a body")
	}

	defer func() { _ = res.Body.Close() }()

	decode, err := decoderFor(res.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxParseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("prblm: reading problem: %w", err)
	}

	if len(body) > maxParseBytes {
		return nil, errTooLarge
	}

	var p Problem
	if err := decode(body, &p); err != nil {
		return nil, err
	}

	return &p, nil
}

// decoderFor returns the decoder for a problem document of contentType, or
// an error wrapping ErrNotProblem.
func decoderFor(contentType string) (func([]byte, any) error, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)

	switch {
	case err != nil:
		return nil, fmt.Errorf("%w: Content-Type %q: %w", ErrNotProblem, contentType, err)
	case mediaType == mediaTypeJSON:
		return json.Unmarshal, nil
	case mediaType == mediaTypeXML:
		return xml.Unmarshal, nil
	default:
		return nil, fmt.Errorf("%w: Content-Type %q", ErrNotProblem, contentType)
	}
}
