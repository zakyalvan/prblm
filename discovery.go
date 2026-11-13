package prblm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
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

// docTemplate renders the HTML documentation of one type: a complete page
// with inline styles only, so the Content-Security-Policy needs no
// exception beyond inline styles. html/template escapes every value.
var docTemplate = template.Must(template.New("doc").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>` + pageStyle + `</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<dl>
<dt>Status</dt><dd>{{.Status}}</dd>
<dt>Type</dt><dd><code>{{.Type}}</code></dd>
<dt>Sub-type</dt><dd><code>{{.Subtype}}</code></dd>
</dl>
{{with .Description}}<p class="description">{{.}}</p>{{end}}
</main>
</body>
</html>
`))

// indexTemplate renders the HTML index of the registered types, each
// linking to its documentation at its type URL.
var indexTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Problem types</title>
<style>` + pageStyle + `</style>
</head>
<body>
<main>
<h1>Problem types</h1>
<ul>
{{range .Types}}<li><a href="{{.Type}}">{{.Title}}</a> <code>{{.Subtype}}</code> ({{.Status}})</li>
{{end}}</ul>
</main>
</body>
</html>
`))

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

// page is one rendered documentation response. Its header is built once
// and only read; writing a page copies it.
type page struct {
	header http.Header
	body   []byte
}

// newPage returns the page of body, sent with contentType.
func newPage(contentType string, body []byte) page {
	return page{
		header: http.Header{
			"Content-Type":   {contentType},
			"Content-Length": {strconv.Itoa(len(body))},
		},
		body: body,
	}
}

// response returns p as a 200 response.
func (p page) response() Response {
	return Response{Status: http.StatusOK, Header: p.header, Body: p.body}
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

// lookup returns the pages served at path: the index at the namespace path,
// with or without a trailing slash, or the documentation of the type whose
// sub-type is the rest of the path, matched exactly.
func (d *discovery) lookup(path string) (pages, bool) {
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

// namespacePath returns the path of a checked namespace: the namespace
// itself when it is a path, or the path of the URI.
func namespacePath(ns string) string {
	u, err := url.Parse(ns)
	if err != nil {
		// A checked namespace always parses.
		panic(fmt.Sprintf("prblm: namespace %q does not parse: %v", ns, err))
	}

	return u.Path
}

// jsonPage renders v as a JSON page. The values come from type definitions,
// whose fields are strings and numbers, so encoding cannot fail.
func jsonPage(v any) page {
	body, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("prblm: rendering documentation: %v", err))
	}

	return newPage(mediaTypeDocJSON, body)
}

// htmlPage renders data with tmpl as an HTML page. The templates are fixed
// and the data are strings and numbers, so rendering cannot fail.
func htmlPage(tmpl *template.Template, data any) page {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("prblm: rendering documentation: %v", err))
	}

	return newPage(mediaTypeHTML+"; charset=utf-8", buf.Bytes())
}

// writeDiscovery writes res as the answer to r: its headers merged into
// those already set, its status, and its body unless r is a HEAD request,
// which gets the same headers and no body.
func writeDiscovery(w http.ResponseWriter, r *http.Request, res Response) {
	res.MergeInto(w.Header())
	w.WriteHeader(res.Status)

	if r.Method == http.MethodHead {
		return
	}

	// A failed write means the client is gone; there is nobody left to tell.
	_, _ = w.Write(res.Body)
}
