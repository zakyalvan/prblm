package prblm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/kartaladev/prblm/internal/negotiate"
)

const (
	// mediaTypeJSON is the media type of a JSON problem document.
	mediaTypeJSON = "application/problem+json"
	// mediaTypeXML is the media type of an XML problem document.
	mediaTypeXML = "application/problem+xml"
	// fallbackBody is the encoded fallbackProblem, sent when encoding the
	// resolved problem fails. It is a constant so that it cannot fail itself.
	fallbackBody = `{"title":"Internal Server Error","status":500}`
)

// ErrEncoding is wrapped by the note the [OnError] hook receives when a
// resolved problem could not be encoded (for example an extension holding a
// channel) and the minimal 500 problem was sent instead. The note also wraps
// the encoder's own error.
var ErrEncoding = errors.New("prblm: problem could not be encoded")

// Request describes the HTTP request an error occurred in, independently of
// any web framework.
type Request struct {
	// Method is the HTTP method.
	Method string
	// Path is the URL path, without the query string.
	Path string
	// Accept is the value of the Accept header.
	Accept string
}

// Response is a problem response ready to be written by a framework adapter.
type Response struct {
	// Header holds the headers to set on the response.
	Header http.Header
	// Body is the encoded problem document.
	Body []byte
	// Status is the HTTP status code.
	Status int
}

// MergeInto merges the response headers into dst, the headers a handler or
// a framework already set on the response, so that every adapter applies the
// same rules: response headers replace those in dst, except that Vary keeps
// the field names already in dst (adding Accept without duplicates), and a
// Cache-Control already in dst is kept, because the handler already chose how
// its response may be cached.
//
// The values are copied, so dst never shares storage with r. A nil dst has
// nothing to merge into, and a nil r.Header has nothing to merge: both leave
// dst unchanged.
func (r Response) MergeInto(dst http.Header) {
	if dst == nil {
		return
	}

	for name, values := range r.Header {
		switch name {
		case "Vary":
			addVary(dst, values...)
		case "Cache-Control":
			if len(dst.Values(name)) == 0 {
				dst[name] = slices.Clone(values)
			}
		default:
			dst[name] = slices.Clone(values)
		}
	}
}

// Respond turns err into a complete problem response without writing it, so
// any framework adapter can deliver the same output. It reports false when
// there is nothing to write, which is only the case for a nil err. A request
// whose context was canceled is answered like any other: a client that is
// still connected gets the problem, and the server drops a write to one that
// went away.
//
// The format follows req.Accept: application/problem+xml for a client that
// prefers XML (application/problem+xml, application/xml or text/xml), and
// application/problem+json otherwise, including when the header accepts
// neither or is malformed, so a client always gets the status and a document
// rather than a 406.
//
// The body is encoded in full before anything is returned, so a problem that
// cannot be encoded (for example an extension holding a channel or an
// infinite number, or a name XML cannot carry) is never sent in part: the
// response becomes the constant minimal JSON problem
// {"title":"Internal Server Error","status":500}, whatever the format, and
// the failure goes to the [OnError] hook.
func (h *Handler) Respond(ctx context.Context, req Request, err error) (Response, bool) {
	if err == nil {
		return Response{}, false
	}

	ctx = withRequest(ctx, req)

	return h.respond(ctx, req, err, h.Resolve(ctx, err)), true
}

// respondPanic renders a recovered panic as the response for req. A panic is
// never mapped like a returned error: whatever its value, the problem is the
// safe 500 fallback, completed by the enrichers and rendered like any other.
// The OnError hook receives pe.
func (h *Handler) respondPanic(ctx context.Context, req Request, pe *PanicError) Response {
	ctx = withRequest(ctx, req)

	p := fallbackProblem()
	h.complete(ctx, p)

	return h.respond(ctx, req, pe, p)
}

// respond renders p, the problem resolved for err, as the response for req:
// it negotiates the format, encodes p (falling back to the minimal problem)
// and reports err to the OnError hook with the problem being sent.
func (h *Handler) respond(ctx context.Context, req Request, err error, p *Problem) Response {
	res, sent := h.render(ctx, req, p)
	h.report(ctx, err, sent)

	return res
}

// render renders p as the response for req: it negotiates the format and
// encodes p, falling back to the minimal problem when that fails. It
// returns the response and the problem it carries.
func (h *Handler) render(ctx context.Context, req Request, p *Problem) (Response, *Problem) {
	f := negotiateFormat(req.Accept)

	body, ok := h.encode(ctx, f, p)
	if !ok {
		// Nothing of p is sent, not even its headers: the minimal problem is
		// a constant JSON document, so it cannot fail too.
		f, body = &jsonFormat, []byte(fallbackBody)
		p = fallbackProblem()
		p.finalize()
	}

	return newResponse(p.Status, p.Header, f.mediaType, body), p
}

// format is a problem document format the handler can answer with.
type format struct {
	// encode encodes a problem as a complete document.
	encode func(p *Problem) ([]byte, error)
	// mediaType is the Content-Type of the document.
	mediaType string
	// accepts lists the media types an Accept header may name to select the
	// format, most specific first.
	accepts []string
}

var (
	// jsonFormat is the default format, and the format of the fallback.
	jsonFormat = format{
		encode:    (*Problem).MarshalJSON,
		mediaType: mediaTypeJSON,
		accepts:   []string{mediaTypeJSON, "application/json"},
	}
	// xmlFormat is sent to clients that prefer XML.
	xmlFormat = format{
		encode:    marshalXML,
		mediaType: mediaTypeXML,
		accepts:   []string{mediaTypeXML, "application/xml", "text/xml"},
	}
	// formats are the formats in order of preference: JSON comes first, so
	// it wins ties.
	formats = []*format{&jsonFormat, &xmlFormat}
	// offers are the media types of formats, in order, for negotiation.
	offers = acceptedTypes(formats)
)

// acceptedTypes returns the media types that select one of formats, in order.
func acceptedTypes(fs []*format) []string {
	var types []string
	for _, f := range fs {
		types = append(types, f.accepts...)
	}

	return types
}

// negotiateFormat returns the format the Accept header prefers, or JSON when it
// accepts none: a client that cannot read the problem still gets the status.
func negotiateFormat(accept string) *format {
	selected := negotiate.Select(accept, offers)

	for _, f := range formats {
		if slices.Contains(f.accepts, selected) {
			return f
		}
	}

	return &jsonFormat
}

// encode encodes p in format f and reports whether it succeeded. A failure,
// including a panic in the encoder of an extension value, is reported to the
// OnError hook, so the response can fall back to the minimal problem without
// losing the reason.
func (h *Handler) encode(ctx context.Context, f *format, p *Problem) ([]byte, bool) {
	var (
		body []byte
		err  error
	)

	// safeCall returns false when the encoder panicked, after reporting it.
	if completed := safeCall(ctx, h, func() bool { body, err = f.encode(p); return true }); !completed {
		return nil, false
	}

	if err != nil {
		h.report(ctx, fmt.Errorf("%w: %w", ErrEncoding, err), p)

		return nil, false
	}

	return body, true
}

// newResponse builds a problem response. It takes ownership of header,
// which must not be shared; Respond passes the header of the problem that
// Resolve copied for this request. The library owns Content-Type and
// Content-Length, so a problem's own values for them are replaced: a wrong
// length would corrupt the response.
//
// Every problem response varies on Accept, and is not stored by caches
// unless the problem sets its own Cache-Control: an error is a statement
// about one moment, and an intermediary replaying it would hide recovery.
func newResponse(status int, header http.Header, mediaType string, body []byte) Response {
	header = canonicalize(header)

	header.Set("Content-Type", mediaType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	addVary(header, "Accept")

	if len(header.Values("Cache-Control")) == 0 {
		header.Set("Cache-Control", "no-store")
	}

	return Response{Status: status, Header: header, Body: body}
}

// canonicalize moves the values of non-canonical keys in h to their
// canonical keys, so that a key put straight into the map, such as
// "content-type", cannot slip past the headers the library controls. It
// returns a new header for a nil h.
func canonicalize(h http.Header) http.Header {
	if h == nil {
		return make(http.Header)
	}

	for name, values := range h {
		if canonical := http.CanonicalHeaderKey(name); canonical != name {
			delete(h, name)
			h[canonical] = append(h[canonical], values...)
		}
	}

	return h
}

// addVary adds each field name listed in values to the Vary header of h,
// unless h already lists it. Field names compare case-insensitively, and a
// value may list several names separated by commas.
func addVary(h http.Header, values ...string) {
	for name := range listElements(values) {
		if !varies(h, name) {
			h.Add("Vary", name)
		}
	}
}

// varies reports whether the Vary header of h lists name.
func varies(h http.Header, name string) bool {
	for listed := range listElements(h.Values("Vary")) {
		if strings.EqualFold(listed, name) {
			return true
		}
	}

	return false
}

// listElements yields the non-empty elements of comma-separated header
// values, without surrounding whitespace.
func listElements(values []string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, value := range values {
			for element := range strings.SplitSeq(value, ",") {
				if element = strings.TrimSpace(element); element != "" && !yield(element) {
					return
				}
			}
		}
	}
}
