package script

import "strings"

// es6Markers are syntax fragments that must never appear in code targeted
// at ES5 -- Shelly's Espruino JS engine does not support them, and
// esbuild's minifier can emit some of them (arrow functions, shorthand
// properties, template literals) from otherwise-ES5 input unless
// Target: api.ES5 is set explicitly (see minifyEsbuild). let/const are
// included because, while a handful of this repo's existing scripts
// already use them directly in source (and esbuild's ES5 target currently
// refuses to downlevel let/const at all -- see MinifyWithOptions doc on
// that limitation), freshly-generated minifier OUTPUT introducing a *new*
// let/const that wasn't in the source would be a minifier bug worth
// catching.
var es6Markers = []string{"=>", "let ", "const ", "`"}

// FindES6Syntax scans code (with string and comment CONTENTS masked out
// first, so a log message like log("a", "=>", "b") or a comment mentioning
// "let" doesn't produce a false positive) for ES6-only syntax markers and
// returns the distinct markers found, in es6Markers order. An empty result
// means the code is clear of the markers this function knows to look for.
//
// This is a heuristic, not a parser: it cannot catch every ES6 construct
// (e.g. object-literal shorthand methods have no single distinctive
// token), but it catches the constructs most likely to actually break
// Shelly's Espruino engine and that a minifier could plausibly introduce.
func FindES6Syntax(code []byte) []string {
	masked := string(maskStringsAndComments(code))
	var found []string
	for _, marker := range es6Markers {
		if strings.Contains(masked, marker) {
			found = append(found, marker)
		}
	}
	return found
}
