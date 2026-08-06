package script

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// Engine selects which JS minifier implementation MinifyWithOptions uses.
type Engine string

const (
	// EngineTdewolff is today's minifier (github.com/tdewolff/minify). It
	// mangles LOCAL identifiers only; top-level names survive untouched.
	// This is the default everywhere -- see CLAUDE.md "Resilience" and the
	// "Default OFF" requirement for the esbuild path.
	EngineTdewolff Engine = "tdewolff"

	// EngineEsbuild is the new esbuild-based minifier
	// (github.com/evanw/esbuild/pkg/api). It supports optional top-level
	// identifier mangling (see MinifyOptions.MangleTopLevel). Must always be
	// invoked with an ES5 target -- Shelly's Espruino JS engine does not
	// support ES6 syntax, and esbuild's minifier can otherwise emit it
	// (arrow functions, template literals, shorthand properties) even from
	// ES5-only input.
	EngineEsbuild Engine = "esbuild"
)

// MinifyOptions configures MinifyWithOptions. The zero value reproduces
// today's behavior exactly: EngineTdewolff, no top-level mangling. Every new
// capability added here is opt-in for that reason.
type MinifyOptions struct {
	// Engine selects the minifier implementation. Zero value (empty string)
	// is treated as EngineTdewolff.
	Engine Engine

	// MangleTopLevel, when true and Engine is EngineEsbuild, wraps the
	// script in an IIFE so esbuild's identifier mangler also renames
	// top-level (module-scope) symbols -- normally esbuild treats top-level
	// names in a plain script as globals and leaves them untouched. Ignored
	// for EngineTdewolff (which never mangles top-level identifiers).
	MangleTopLevel bool

	// PreserveGlobals lists top-level identifiers that MUST remain
	// reachable, by their original name, as real globals after wrapping.
	// Concretely: Shelly Schedule jobs invoke handlers BY NAME via
	// `script.eval` (e.g. {"code": "handleMorningStart()"}); if that name
	// disappears into the IIFE's closure, the schedule breaks. This list is
	// per-script -- see internal/shelly/scripts.MinifyOptionsFor for the
	// registry of which scripts need which names preserved. Every entry
	// must name an actual top-level declaration in the source, or
	// MinifyWithOptions returns an error (fail fast on config typos rather
	// than silently uploading a broken script).
	PreserveGlobals []string

	// DebugExports lists additional top-level identifiers to keep reachable
	// as globals purely so a developer can inspect them live via
	// Script.Eval while debugging on real hardware (e.g. "STATE",
	// "CONFIG"). This is a deliberate, documented trade-off: every name
	// here is a live debuggability win but also a few extra bytes and a
	// (very small) global-namespace collision risk with other scripts on
	// the same device. Off by default; opt in per incident.
	DebugExports []string

	// DisableSyntaxLowering turns off esbuild's MinifySyntax pass (Engine
	// EngineEsbuild only). This exists specifically because of
	// https://github.com/asnowfix/home-automation/issues/266: with
	// MinifySyntax ON (the default here, matching esbuild's normal
	// "aggressive minification" behavior), esbuild collapses sequential
	// `x += f(...)` statements into a single comma-expression inside a
	// `return`, EXACTLY like tdewolff does today -- confirmed by direct
	// comparison against prometheus-metrics.js's `_buildSwitchMetrics`,
	// which is the function issue #266 identified as crashing real
	// hardware with "Too much recursion" (Espruino's C expression
	// evaluator is recursive; a long comma chain overflows its stack).
	// Setting this to true keeps each statement separate (semicolon, not
	// comma), which a structural comparison shows avoids that pattern
	// (verified via static analysis of the emitted code only -- NOT
	// verified on real hardware; do not treat this as a fix for #266
	// until it's been run on a device). Costs a small amount of size
	// (~180 bytes larger on prometheus-metrics.js in testing) relative to
	// leaving MinifySyntax on, but was still smaller than tdewolff's
	// current output. Off by default so today's byte-savings numbers
	// don't silently regress.
	DisableSyntaxLowering bool
}

// MinifyResult is the output of MinifyWithOptions.
type MinifyResult struct {
	// Code is the minified script, ready to upload.
	Code []byte

	// SourceMap is the Source Map v3 JSON esbuild produced, or nil when the
	// engine/options combination didn't generate one (EngineTdewolff, or
	// EngineEsbuild without MangleTopLevel -- there is nothing to map when
	// no top-level renaming happened).
	SourceMap []byte

	// Symbols is the flat mangled->original table for top-level symbols,
	// derived from SourceMap. nil under the same conditions as SourceMap.
	// See SymbolMap for why this exists alongside (not instead of) the
	// source map: Shelly crash traces carry function names and code
	// snippets but no line/column, so a source map alone cannot resolve
	// them -- this flat table can.
	Symbols *SymbolMap
}

// MinifyWithOptions minifies src according to opts. name is used only for
// error messages and to label the resulting SymbolMap; pass the script's
// filename (e.g. "pool-pump.js").
func MinifyWithOptions(name string, src []byte, opts MinifyOptions) (*MinifyResult, error) {
	switch opts.Engine {
	case "", EngineTdewolff:
		out, err := minifyJS(src)
		if err != nil {
			return nil, err
		}
		return &MinifyResult{Code: out}, nil
	case EngineEsbuild:
		return minifyEsbuild(name, src, opts)
	default:
		return nil, fmt.Errorf("minify %s: unknown minifier engine %q", name, opts.Engine)
	}
}

func minifyEsbuild(name string, src []byte, opts MinifyOptions) (*MinifyResult, error) {
	if !opts.MangleTopLevel {
		result := api.Transform(string(src), api.TransformOptions{
			Target:            api.ES5,
			Charset:           api.CharsetUTF8,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      !opts.DisableSyntaxLowering,
		})
		if err := esbuildErr(name, result.Errors); err != nil {
			return nil, err
		}
		return &MinifyResult{Code: result.Code}, nil
	}

	topLevel := topLevelDeclarations(src)
	known := make(map[string]bool, len(topLevel))
	for _, n := range topLevel {
		known[n] = true
	}

	exportNames := make([]string, 0, len(opts.PreserveGlobals)+len(opts.DebugExports))
	seenExport := make(map[string]bool, cap(exportNames))
	addExports := func(list []string, kind string) error {
		for _, n := range list {
			if !known[n] {
				return fmt.Errorf("minify %s: %s %q is not a top-level declaration in the script", name, kind, n)
			}
			if !seenExport[n] {
				seenExport[n] = true
				exportNames = append(exportNames, n)
			}
		}
		return nil
	}
	if err := addExports(opts.PreserveGlobals, "preserve-global"); err != nil {
		return nil, err
	}
	if err := addExports(opts.DebugExports, "debug-export"); err != nil {
		return nil, err
	}

	wrapped := wrapIIFE(src, exportNames)

	result := api.Transform(wrapped, api.TransformOptions{
		Target:            api.ES5,
		Charset:           api.CharsetUTF8,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      !opts.DisableSyntaxLowering,
		Sourcemap:         api.SourceMapExternal,
		SourcesContent:    api.SourcesContentInclude,
	})
	if err := esbuildErr(name, result.Errors); err != nil {
		return nil, err
	}

	symbols, err := buildSymbolMap(name, opts.Engine, topLevel, result.Code, result.Map)
	if err != nil {
		return nil, fmt.Errorf("minify %s: build symbol map: %w", name, err)
	}

	return &MinifyResult{
		Code:      result.Code,
		SourceMap: result.Map,
		Symbols:   symbols,
	}, nil
}

func esbuildErr(name string, msgs []api.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		parts = append(parts, m.Text)
	}
	return fmt.Errorf("esbuild minify %s: %s", name, strings.Join(parts, "; "))
}

// exportsVar is the name of the synthetic top-level variable that holds the
// wrapped IIFE's return value. It is deliberately verbose/unlikely to
// collide with anything in the wrapped script or with other scripts running
// on the same device (top-level names are never mangled -- see
// wrapIIFE for why).
const exportsVar = "__myhome_minify_exports__"

// wrapIIFE wraps src in an IIFE so esbuild's identifier mangler treats the
// script's top-level declarations as an ordinary (renameable) function
// scope instead of the page/module's global scope, then re-exports
// exportNames as real top-level globals under their ORIGINAL names.
//
// This works because of how esbuild's non-bundled Transform treats
// identifier scope: top-level (true script-level) declarations are never
// renamed (other code could reference them by name), but declarations
// nested inside a function body are an ordinary scope and are freely
// renamed. By moving the whole script inside a closure, its own top-level
// symbols become "nested" and thus mangle-eligible; the explicit re-export
// assignments below are themselves at the SCRIPT's true top level, so their
// left-hand identifiers are, in turn, never renamed -- giving external code
// (Shelly Schedule jobs evaluating "handleMorningStart()", or a developer
// using Script.Eval) a stable, literal name to call into the mangled
// internals through.
//
// The re-export must go through the returned object rather than a bare
// `name = name;` statement: inside the closure, `name` resolves to the
// local (about-to-be-renamed) declaration on BOTH sides of that assignment,
// so it would just reassign the local binding to itself and export nothing.
func wrapIIFE(src []byte, exportNames []string) string {
	var b strings.Builder
	b.WriteString("var ")
	b.WriteString(exportsVar)
	b.WriteString("=(function(){\n")
	b.Write(src)
	b.WriteString("\nreturn {")
	for i, n := range exportNames {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.Quote(n))
		b.WriteString(":")
		b.WriteString(n)
	}
	b.WriteString("};\n})();\n")
	for _, n := range exportNames {
		b.WriteString(n)
		b.WriteString("=")
		b.WriteString(exportsVar)
		b.WriteString(".")
		b.WriteString(n)
		b.WriteString(";\n")
	}
	return b.String()
}

// sourceMapV3 is the subset of the Source Map v3 JSON format
// (https://sourcemaps.info/spec.html) that buildSymbolMap needs.
type sourceMapV3 struct {
	Version int      `json:"version"`
	Sources []string `json:"sources"`
	Names   []string `json:"names"`
	Mappings string  `json:"mappings"`
}

// buildSymbolMap derives the flat top-level mangled->original table by
// decoding the source map's VLQ "mappings" and, for every mapping that
// carries a "names" entry matching a KNOWN top-level declaration, reading
// the actual mangled identifier text out of the generated code at that
// position. Restricting to knownTopLevel is what makes the result
// unambiguous: esbuild gives top-level (module-scope) symbols a single flat
// scope, so each one gets a unique mangled name across the whole file --
// but nested (function-local) symbols can and do reuse short mangled names
// across different scopes, so including them here would make the table
// lossy/wrong. See docs on MinifyOptions.DebugExports for the related,
// narrower re-export mechanism.
func buildSymbolMap(name string, engine Engine, topLevelNames []string, code []byte, rawMap []byte) (*SymbolMap, error) {
	var sm sourceMapV3
	if err := json.Unmarshal(rawMap, &sm); err != nil {
		return nil, fmt.Errorf("parse source map: %w", err)
	}

	known := make(map[string]bool, len(topLevelNames))
	for _, n := range topLevelNames {
		known[n] = true
	}

	lines := strings.Split(string(code), "\n")

	segs, err := decodeMappings(sm.Mappings)
	if err != nil {
		return nil, fmt.Errorf("decode mappings: %w", err)
	}

	symbols := make(map[string]string)
	for _, seg := range segs {
		if seg.nameIdx < 0 || seg.nameIdx >= len(sm.Names) {
			continue
		}
		orig := sm.Names[seg.nameIdx]
		if !known[orig] {
			continue
		}
		if seg.genLine < 0 || seg.genLine >= len(lines) {
			continue
		}
		line := lines[seg.genLine]
		mangled := identifierAt(line, seg.genCol)
		if mangled == "" {
			continue
		}
		if existing, ok := symbols[mangled]; ok && existing != orig {
			// A mangled token mapped to two different "top-level" original
			// names should not happen (top-level symbols get a unique
			// mangled name each) -- if it does, don't guess; drop the
			// ambiguous entry rather than risk a wrong demangle.
			delete(symbols, mangled)
			continue
		}
		symbols[mangled] = orig
	}

	return &SymbolMap{Script: name, Engine: engine, Symbols: symbols}, nil
}

// utf16ColumnToByteOffset converts a Source Map v3 generated column into a
// byte offset within line.
//
// Source Map v3 columns are counted in UTF-16 code units, not bytes. Go
// strings are indexed by bytes, so the two only coincide while the generated
// output is pure ASCII. esbuild's default Charset is ASCII (it escapes
// non-ASCII as \uXXXX), which made them coincide by accident — until that
// default had to be changed to CharsetUTF8, because Espruino rejects \uXXXX
// literals outright (see the Charset setting above). With raw UTF-8 in the
// output, every multi-byte character shifts byte offsets relative to columns,
// and indexing a line by a column yields the wrong position — decoding, for
// example, "handleMorningStart" as "andleMorningStart".
//
// Returns -1 if col lies beyond the end of the line.
func utf16ColumnToByteOffset(line string, col int) int {
	if col <= 0 {
		return col // 0 -> 0; negative is rejected by the caller
	}
	units := 0
	for i, r := range line {
		if units >= col {
			return i
		}
		if r > 0xFFFF {
			units += 2 // encoded as a surrogate pair in UTF-16
		} else {
			units++
		}
	}
	if units >= col {
		return len(line)
	}
	return -1
}

func identifierAt(line string, col int) string {
	col = utf16ColumnToByteOffset(line, col)
	if col < 0 || col >= len(line) || !isIdentPart(line[col]) {
		return ""
	}
	end := col
	for end < len(line) && isIdentPart(line[end]) {
		end++
	}
	return line[col:end]
}

func isIdentStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}
