package jstarget

import (
	"encoding/json"
	"fmt"

	"github.com/dop251/goja"
)

// Target is a recognised value of a "@target" JSDoc annotation.
type Target string

const (
	// TargetDevice marks code that must ship to the Shelly device only.
	TargetDevice Target = "device"
	// TargetDaemon marks code that must run on the myhome daemon only.
	TargetDaemon Target = "daemon"
	// TargetBoth marks code explicitly kept on both sides. This is also the
	// implicit default for unannotated code, but that default is applied by
	// the (not-yet-built) subtractive filter, never by this package -- see
	// the package doc and issue #568, "What is deliberately NOT decided
	// here".
	TargetBoth Target = "both"
)

// validTargets is the set of Target values Regions will accept. Anything
// else -- most importantly a typo such as "demon" -- is a hard error rather
// than a silent fallback to TargetBoth, per #568: shipping daemon-only code
// to a device because a typo was quietly ignored is exactly the failure this
// package exists to prevent.
var validTargets = map[string]Target{
	string(TargetDevice): TargetDevice,
	string(TargetDaemon): TargetDaemon,
	string(TargetBoth):   TargetBoth,
}

// Region is a source span annotated with a "@target" JSDoc comment.
//
// Start and End are byte offsets into the source passed to Regions, with the
// same half-open [Start, End) convention as Go's slicing: src[Start:End] is
// exactly the annotated construct, JSDoc comment excluded.
type Region struct {
	// Target is the resolved, validated annotation value.
	Target Target
	// Start and End are byte offsets into the parsed source.
	Start, End int
	// Name is a best-effort identifier for diagnostics: a function or
	// variable name, an object property key, or a comma-joined list of
	// names for a multi-declarator var statement. It is not guaranteed
	// unique and must not be used to resolve a Region back to source --
	// use Start/End for that.
	Name string
}

// Regions parses a JavaScript source file and returns, for each JSDoc
// "/** @target X */" annotation found, the resolved Region it annotates.
//
// Resolution rules (see #568 for the full rationale):
//
//   - A JSDoc block comment "/** @target X */" annotates the next construct
//     in source order: a function declaration, a var declaration (including
//     a var assigned a function expression), or an object property. Only
//     whitespace and/or other comments may separate the annotation from the
//     construct it annotates.
//   - A var declaration with multiple declarators (`var a = 1, b = 2;`) is
//     treated as a single construct: one Region spans the whole statement,
//     and Name is the comma-joined declarator names. The annotation cannot
//     target one declarator without the others.
//   - X must be one of "device", "daemon", "both" (case-sensitive, exactly
//     as written). Any other value is an error.
//   - An annotation that does not resolve to a recognised construct (a
//     trailing comment at end of file, a comment followed only by more
//     comments and then nothing, or a comment sitting inside an expression
//     rather than before a statement/property) is an error.
//   - An annotation whose resolved Region would fall strictly inside another
//     resolved Region (a "@target daemon" function nested inside a
//     "@target daemon" region, for instance) is an error: nested
//     annotations are reported rather than silently producing overlapping
//     ranges.
//
// Unannotated code produces no Region at all -- Regions never synthesises a
// TargetBoth region for code that carries no annotation. That default
// belongs to the (not-yet-built) subtractive filter, not to this parser.
func Regions(src []byte) ([]Region, error) {
	vm := goja.New()
	if _, err := vm.RunString(acornJS); err != nil {
		return nil, fmt.Errorf("jstarget: failed to load vendored acorn: %w", err)
	}

	if err := vm.Set("__jstargetSrc", string(src)); err != nil {
		return nil, fmt.Errorf("jstarget: failed to bind source into goja VM: %w", err)
	}
	parseResult, err := vm.RunString(jstargetParseScript)
	if err != nil {
		return nil, fmt.Errorf("jstarget: acorn parse failed: %w", err)
	}

	var out parseOutput
	if err := json.Unmarshal([]byte(parseResult.String()), &out); err != nil {
		return nil, fmt.Errorf("jstarget: failed to decode acorn output: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("jstarget: %s", out.Error)
	}

	var ast map[string]any
	if err := json.Unmarshal(out.AST, &ast); err != nil {
		return nil, fmt.Errorf("jstarget: failed to decode acorn AST: %w", err)
	}

	return resolve(src, ast, out.Comments)
}

// jstargetParseScript runs inside the goja VM. It parses __jstargetSrc with
// acorn, requesting byte ranges and a full comment list, and returns a JSON
// envelope: either {"error": "..."} on a JS-level parse failure, or
// {"ast": <acorn Program node>, "comments": [<acorn comment>, ...]}.
//
// The AST and comment list are deliberately handed back as plain JSON rather
// than kept as goja.Value: JSON round-tripping keeps the goja/acorn boundary
// narrow, exactly as the byte-range API does for this package's own callers.
const jstargetParseScript = `
(function () {
  var comments = [];
  try {
    var ast = acorn.parse(__jstargetSrc, {
      ecmaVersion: 2022,
      sourceType: "script",
      ranges: true,
      onComment: comments
    });
    return JSON.stringify({ ast: ast, comments: comments });
  } catch (e) {
    return JSON.stringify({ error: String(e) });
  }
})()
`

type parseOutput struct {
	Error    string          `json:"error,omitempty"`
	AST      json.RawMessage `json:"ast,omitempty"`
	Comments []comment       `json:"comments,omitempty"`
}

// comment mirrors the shape acorn's onComment hook produces for each
// collected comment.
type comment struct {
	Type  string `json:"type"` // "Block" or "Line"
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}
