package prblm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/kartaladev/prblm/internal/negotiate"
)

const (
	// mediaTypeHTML is the media type of an HTML documentation page.
	mediaTypeHTML = "text/html"
	// mediaTypeDocJSON is the media type of a JSON documentation page.
	mediaTypeDocJSON = "application/json"
)

// docOffers are the representations of documentation, in order of
// preference: HTML comes first, so it wins ties, because RFC 9457 asks for
// documentation people can read.
var docOffers = []string{mediaTypeHTML, mediaTypeDocJSON}

// pageShell is the layout every documentation page shares: a complete page
// with inline styles only, so the Content-Security-Policy needs no
// exception beyond inline styles. A page defines its "title" and "content";
// html/template escapes every value.
var pageShell = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{template "title" .}}</title>
<style>` + pageStyle + `</style>
</head>
<body>
<main>
<h1>{{template "title" .}}</h1>
{{template "content" .}}
</main>
</body>
</html>
`))

// docTemplate renders the HTML documentation of one type.
var docTemplate = pageTemplate(`{{define "title"}}{{.Title}}{{end}}{{define "content"}}<dl>
<dt>Status</dt><dd>{{.Status}}</dd>
<dt>Type</dt><dd><code>{{.Type}}</code></dd>
<dt>Sub-type</dt><dd><code>{{.Subtype}}</code></dd>
</dl>
{{with .Description}}<p class="description">{{.}}</p>{{end}}{{end}}`)

// indexTemplate renders the HTML index of the registered types, each
// linking to its documentation at its type URL.
var indexTemplate = pageTemplate(`{{define "title"}}Problem types{{end}}{{define "content"}}<ul>
{{range .Types}}<li><a href="{{.Type}}">{{.Title}}</a> <code>{{.Subtype}}</code> ({{.Status}})</li>
{{end}}</ul>{{end}}`)

// pageTemplate returns the page shell with the title and content blocks
// defined by blocks.
func pageTemplate(blocks string) *template.Template {
	return template.Must(template.Must(pageShell.Clone()).Parse(blocks))
}

// pageStyle is the inline CSS of the documentation pages.
const pageStyle = "body{font-family:system-ui,sans-serif;line-height:1.5;margin:0;padding:2rem;color:#1f2328;background:#fff}" +
	"main{max-width:48rem;margin:0 auto}h1{font-size:1.75rem;margin:0 0 1rem}" +
	"dl{display:grid;grid-template-columns:max-content 1fr;gap:.25rem 1rem}dt{font-weight:600}dd{margin:0}" +
	"code{font-family:ui-monospace,monospace;overflow-wrap:anywhere}.description{white-space:pre-line}" +
	"@media (prefers-color-scheme:dark){body{color:#e6edf3;background:#0d1117}}"

// WithoutDiscovery keeps the type definitions registered with [WithTypes]
// but serves no documentation for them: [Handler.DiscoveryPath] is empty and
// [Handler.Discovery] answers every request with a 404 problem. Discovery
// is otherwise on whenever types are registered, because registering them
// is the intent to document them.
func WithoutDiscovery() Option {
	return func(h *Handler) {
		h.isDiscoveryDisabled = true
	}
}

// discovery holds the documentation of the registered types, rendered once
// when the handler is created and never changed, so serving it is safe for
// concurrent use.
type discovery struct {
	// docs are the pages of each type, by sub-type.
	docs map[string]pages
	// index is the index of every type.
	index pages
	// path is the path of the namespace, without a trailing slash.
	path string
}

// pageCacheControl lets browsers and shared caches keep a page for an hour.
// Definitions change only when the application is redeployed, so an hour
// bounds how long a stale page is shown, and the ETag makes revalidation
// after that a cheap 304.
const pageCacheControl = "public, max-age=3600"

// page is one rendered documentation resource: its full response and the
// 304 that revalidates it, both built once and only read, since writing a
// response copies its header.
type page struct {
	full        Response
	notModified Response
	etag        string
}

// newPage returns the page of body, sent with contentType. Every page is
// sent with nosniff, so a browser never reads it as another type, varies on
// Accept, because HTML and JSON share the URL, and is cacheable with a
// strong ETag of its own: the first 16 hex digits of the SHA-256 of body,
// so it changes exactly when the rendered page does.
func newPage(contentType string, body []byte, extra http.Header) page {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`

	header := http.Header{
		"X-Content-Type-Options": {"nosniff"},
		"Cache-Control":          {pageCacheControl},
		"Etag":                   {etag},
	}
	maps.Copy(header, extra)

	return page{
		// newResponse adds Content-Type, Content-Length and Vary: Accept,
		// and keeps the page's own Cache-Control.
		full: newResponse(http.StatusOK, header, contentType, body),
		notModified: Response{Status: http.StatusNotModified, Header: http.Header{
			"Etag":          {etag},
			"Cache-Control": {pageCacheControl},
			"Vary":          {"Accept"},
		}},
		etag: etag,
	}
}

// response returns p as the answer to a request with the If-None-Match
// header ifNoneMatch: 304 without a body when it matches the page's ETag,
// and the page otherwise.
func (p page) response(ifNoneMatch string) Response {
	if matchesETag(ifNoneMatch, p.etag) {
		return p.notModified
	}

	return p.full
}

// matchesETag reports whether the If-None-Match header ifNoneMatch matches
// the strong entity tag etag: it is "*" or lists etag, compared weakly as
// RFC 9110 (section 13.1.2) requires, so W/"x" matches "x". The list is
// scanned tag by tag, since a quoted tag may itself contain a comma; at the
// first malformed element the scan stops, so garbage matches nothing.
func matchesETag(ifNoneMatch, etag string) bool {
	rest := ifNoneMatch

	for {
		rest = strings.TrimLeft(rest, " \t,")
		if rest == "" {
			return false
		}

		if rest[0] == '*' {
			return true
		}

		rest = strings.TrimPrefix(rest, "W/")
		if rest == "" || rest[0] != '"' {
			return false
		}

		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return false
		}

		if tag := rest[:end+2]; tag == etag {
			return true
		}

		rest = rest[end+2:]
	}
}

// pages are the representations of one documentation resource.
type pages struct {
	html, json page
}

// negotiate returns the representation the Accept header prefers: JSON when
// it ranks application/json above text/html, and HTML otherwise, including
// for a malformed header or one that accepts neither.
func (ps pages) negotiate(accept string) page {
	if negotiate.Select(accept, docOffers) == mediaTypeDocJSON {
		return ps.json
	}

	return ps.html
}

// indexDoc is the index of the registered types, sorted by sub-type.
type indexDoc struct {
	Types []indexEntry `json:"types"`
}

// indexEntry is one type in the index: its documentation without the
// description.
type indexEntry struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Status  int    `json:"status"`
	Title   string `json:"title"`
}

// typeDoc is the documentation of a type. The field order is the member
// order of the JSON document.
type typeDoc struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	Status      int    `json:"status"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// lookup returns the pages served at u: the index at the namespace path,
// with or without a trailing slash, or the documentation of the type whose
// sub-type is the rest of the path, matched exactly. A path with encoded
// characters ("%2F", "%2D") matches nothing, so every page has exactly one
// URL; the exact match also leaves no room for "..".
func (d *discovery) lookup(u *url.URL) (pages, bool) {
	path := u.Path
	if u.EscapedPath() != path {
		return pages{}, false
	}

	if path == d.path || path == d.path+"/" {
		return d.index, true
	}

	subtype, ok := strings.CutPrefix(path, d.path+"/")
	if !ok {
		return pages{}, false
	}

	doc, ok := d.docs[subtype]

	return doc, ok
}

// documents reports whether d serves documentation for subtype.
func (d *discovery) documents(subtype string) bool {
	_, ok := d.docs[subtype]

	return ok
}

// namespacePath returns the path of a checked namespace: the namespace
// itself when it is a path, or the path of the URI. A checked URI has a
// host, a path, no user info, query or fragment, and nothing escaped, so
// its path is everything from the first "/" after the host.
func namespacePath(ns string) string {
	if _, rest, ok := strings.Cut(ns, "://"); ok {
		return rest[strings.IndexByte(rest, '/'):]
	}

	return ns
}

// jsonPage renders v, the documentation of the resource name, as a JSON
// page. The values come from type definitions, whose fields are strings
// and numbers, so encoding cannot fail; should it, the handler cannot be
// created, and the panic names the resource.
func jsonPage(name string, v any) page {
	body, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("prblm: rendering the documentation of %q: %v", name, err))
	}

	return newPage(mediaTypeDocJSON, body, nil)
}

// htmlPage renders data, the documentation of the resource name, with tmpl
// as an HTML page. The templates are fixed and the data are strings and
// numbers, so rendering cannot fail; should it, the handler cannot be
// created, and the panic names the resource.
func htmlPage(name string, tmpl *template.Template, data any) page {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("prblm: rendering the documentation of %q: %v", name, err))
	}

	// The page loads nothing and runs nothing; only its inline styles apply.
	return newPage(mediaTypeHTML+"; charset=utf-8", buf.Bytes(), http.Header{
		"Content-Security-Policy": {"default-src 'none'; style-src 'unsafe-inline'"},
	})
}

// writeDiscovery writes res as the answer to r. A HEAD request gets the
// same headers and no body, whatever writer serves it.
func writeDiscovery(w http.ResponseWriter, r *http.Request, res Response) {
	if r.Method == http.MethodHead {
		res.Body = nil
	}

	track(w).send(res)
}
