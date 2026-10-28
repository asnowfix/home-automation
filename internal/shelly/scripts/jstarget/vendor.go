// Package jstarget parses a JavaScript source file and resolves JSDoc
// "@target" annotations to the byte range of the construct they annotate.
//
// See https://github.com/asnowfix/home-automation/issues/568 and #544 for the
// design this implements.
package jstarget

import _ "embed"

//go:generate -command fetch go run ../../../../cmd/fetchasset

// acorn v8.14.0 (UMD build), used only as a parser hosted inside goja: it
// exposes an onComment hook that goja's own parser lacks, which is required
// to resolve JSDoc "@target" annotations to AST nodes. See the package doc
// and issue #568 for why goja's own parser cannot do this.
//go:generate fetch -url=https://unpkg.com/acorn@8.14.0/dist/acorn.js -out=acorn.js -sha256=bec194b9abb10147d3bb77e544d95cf1c7b4f9f42dad00dfc83791909ebf49c7

//go:embed acorn.js
var acornJS string
