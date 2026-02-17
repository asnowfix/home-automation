package static

import _ "embed"

//go:generate -command fetch go run ../../../../cmd/fetchasset

// Bulma CSS v0.9.4
//go:generate fetch -url=https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css -out=bulma.min.css -sha256=ad3a5d3b41d7042369ade00772eead0763e9839d79568fb91ad612b2734bcfef

// HTMX v2.0.4
//go:generate fetch -url=https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -out=htmx.min.js -sha256=e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447

// Alpine.js v3.14.3
//go:generate fetch -url=https://cdn.jsdelivr.net/npm/alpinejs@3.14.3/dist/cdn.min.js -out=alpine.min.js -sha256=689f513978d11d69f4d33794f7296c9a586a2e55de79bb447cddbc3f474f9f07

//go:embed bulma.min.css
var bulmaCSS []byte

//go:embed htmx.min.js
var htmxJS []byte

//go:embed alpine.min.js
var alpineJS []byte

//go:embed myhome.css
var myhomeCSS []byte

//go:embed penates.svg
var penatesSVG []byte

// Asset represents a static asset with its content and MIME type
type Asset struct {
	Content     []byte
	ContentType string
}

// Assets is a map of asset paths to their content
var Assets = map[string]Asset{
	"/static/bulma.min.css": {Content: bulmaCSS, ContentType: "text/css; charset=utf-8"},
	"/static/htmx.min.js":   {Content: htmxJS, ContentType: "application/javascript; charset=utf-8"},
	"/static/alpine.min.js": {Content: alpineJS, ContentType: "application/javascript; charset=utf-8"},
	"/static/myhome.css":    {Content: myhomeCSS, ContentType: "text/css; charset=utf-8"},
	"/static/penates.svg":   {Content: penatesSVG, ContentType: "image/svg+xml; charset=utf-8"},
}
