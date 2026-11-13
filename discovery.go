package prblm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

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

// ErrShadowedDocumentation is wrapped by the note the [OnError] hook
// receives when an application route answers a GET or HEAD request for
// which discovery would have served a documentation page or the index (see
// [Handler.NoteShadowedRoute]). Such a page cannot be reached, and the
// library never logs, so the note makes the conflict visible. It names the
// route and the path, and comes with a nil problem, because the request
// succeeded.
var ErrShadowedDocumentation = errors.New("prblm: an application route hides type documentation")

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

// DiscoveryRequest describes a request for [Handler.RespondDiscovery],
// independently of any web framework.
type DiscoveryRequest struct {
	// Method is the HTTP method.
	Method string
	// Path is the URL path as the client sent it, still escaped:
	// r.URL.EscapedPath() in net/http and gin, the raw request path in
	// fiber. Without the query string.
	Path string
	// Accept is the value of the Accept header, repeated lines joined with
	// ",".
	Accept string
	// IfNoneMatch is the value of the If-None-Match header, repeated lines
	// joined with ",".
	IfNoneMatch string
}

// discovery serves the documentation of the registered types. Its pages
// are rendered once, on first use, and never changed, so serving them is
// safe for concurrent use.
type discovery struct {
	// site returns the rendered pages, rendering them on its first call.
	site func() site
	// path is the path of the namespace, without a trailing slash, and
	// prefix is path followed by "/", built once.
	path, prefix string
	// renders counts how many times the pages were rendered: 0 until
	// discovery is first used, 1 after.
	renders atomic.Int32
	// documented are the registered sub-types, known without rendering, so
	// noting a hidden page renders nothing.
	documented map[string]string
	// noted are the routes already reported as hiding documentation.
	noted notedRoutes
}

// maxNotedRoutes caps the routes remembered as hiding documentation. Route
// patterns come from code, so a real application stays far below it; the
// cap only bounds the memory a misbehaving adapter could take.
const maxNotedRoutes = 256

// notedRoutes is the set of routes already noted as hiding documentation,
// so each is noted once. It is safe for concurrent use.
type notedRoutes struct {
	routes map[string]struct{}
	mu     sync.Mutex
}

// add adds route and reports whether it was new. Once the set holds
// maxNotedRoutes routes, it adds nothing and reports false.
func (n *notedRoutes) add(route string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, ok := n.routes[route]; ok || len(n.routes) >= maxNotedRoutes {
		return false
	}

	if n.routes == nil {
		n.routes = make(map[string]struct{})
	}

	n.routes[route] = struct{}{}

	return true
}

// site is the rendered documentation: the pages of each type and the index.
type site struct {
	// docs are the pages of each type, by sub-type.
	docs map[string]pages
	// index is the index of every type.
	index pages
}

// newLazyDiscovery returns the discovery served under path, whose pages
// render returns on first use. When render panics, as for a page that
// cannot be rendered, every later use panics with the same value, so the
// failure is never mistaken for missing pages.
func newLazyDiscovery(path string, render func() site) *discovery {
	d := &discovery{path: path, prefix: path + "/"}
	d.site = sync.OnceValue(func() site {
		d.renders.Add(1)

		return render()
	})

	return d
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

// isUnder reports whether the escaped path is for discovery: the
// discovery path, or that path followed by "/" and anything.
func (d *discovery) isUnder(path string) bool {
	return path == d.path || strings.HasPrefix(path, d.prefix)
}

// lookup returns the pages served at the escaped path, which isUnder has
// accepted: the index at the namespace path, with or without a trailing
// slash, or the documentation of the type whose sub-type is the rest of the
// path, matched exactly. A sub-type never needs escaping, so a path with an
// escape ("%2F", "%2D", "%zz") matches nothing and every page has exactly
// one URL; the exact match also leaves no room for "..".
func (d *discovery) lookup(path string) (pages, bool) {
	rendered := d.site()

	subtype := d.subtypeAt(path)
	if subtype == "" {
		return rendered.index, true
	}

	doc, ok := rendered.docs[subtype]

	return doc, ok
}

// hasPage reports whether lookup finds a page at the escaped path, which
// isUnder has accepted, without rendering anything.
func (d *discovery) hasPage(path string) bool {
	subtype := d.subtypeAt(path)
	_, ok := d.documented[subtype]

	return subtype == "" || ok
}

// subtypeAt returns the sub-type named by the escaped path, which isUnder
// has accepted, or "" for the index: the namespace path, with or without a
// trailing slash.
func (d *discovery) subtypeAt(path string) string {
	if path == d.path {
		return ""
	}

	return strings.TrimPrefix(path, d.prefix)
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
