package script

import (
	"fmt"
	"strings"
)

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

// uXXXXDeviceError is the exact error text real Shelly hardware (a modified
// Espruino JS engine) raises when its tokenizer encounters a `\uXXXX` or
// `\u{...}` escape sequence anywhere in a script's source:
//
//	Uncaught SyntaxError: \uXXXX literals are disallowed
//
// Reproduced here so FindUnicodeEscapes' error messages name the concrete
// on-device failure a passing goja test would otherwise hide -- goja
// accepts \uXXXX escapes happily, so without this check the whole class of
// bug only surfaces when someone plugs a real device in.
const uXXXXDeviceError = `\uXXXX literals are disallowed`

// FindUnicodeEscapes scans src for the literal SOURCE TEXT of a `\uXXXX` /
// `\u{...}` escape sequence -- backslash, "u", then hex digits or a
// "{...}" block, as bytes actually present in src, NOT any character such
// an escape might decode to -- and returns each distinct offending snippet
// found, in order of first appearance. An empty result means src is clear.
//
// RAW NON-ASCII IS PERMITTED BY DESIGN. READ THIS BEFORE "fixing" this
// into a non-ASCII / unicode.IsPrint check -- that would be wrong and
// would break every script in this repo's default path:
//
// real Shelly hardware's tokenizer rejects the LITERAL ESCAPE SEQUENCE
// TEXT (see uXXXXDeviceError above: "SyntaxError: \uXXXX literals are
// disallowed") but is completely happy with raw, un-escaped non-ASCII
// UTF-8 bytes. pool-pump.js ships ~30 raw non-ASCII bytes today (em dashes
// in comments, one checkmark character in a log string) via the default
// tdewolff minifier, and that exact script is running correctly on the
// production Pro1 and on a Plus1 right now. What actually crashed a Plus1
// was esbuild's default ASCII charset turning that same raw checkmark
// character into the 6-character escape sequence in its OUTPUT -- see
// minify_esbuild.go's `Charset: api.CharsetUTF8` fix, which is what makes
// esbuild emit raw UTF-8 instead. So, concretely:
//
//   - print("<raw checkmark char, bytes E2 9C 93>") -- MUST PASS.
//   - print("<the literal 6-char escape sequence: backslash, u, 2, 7, 1, 3>")
//     -- MUST FAIL: this is what Espruino rejects.
//   - print("<escaped backslash followed by literal u2713>") -- MUST PASS:
//     not an escape sequence at all (see the backslash-run note below).
//
// Rejecting raw non-ASCII here would fail that known-good production
// script; only the ESCAPE SEQUENCE is the bug. Dedicated regression tests
// (unicode_escape_test.go) assert all three cases above explicitly and
// exist specifically to catch anyone tightening this into a non-ASCII
// check by mistake -- if you're about to do that, those tests should
// already be telling you not to.
//
// The scan intentionally does NOT mask string contents (unlike
// FindES6Syntax/topLevelDeclarations): the device error above fires
// wherever the escape appears in source, including inside a string
// literal -- that is exactly where pool-pump.js's checkmark character was,
// before the Charset fix. It DOES mask comment contents (see maskComments)
// because the device's tokenizer never re-lexes comment text for escapes,
// so a comment merely mentioning the escape sequence in prose is not a
// real device-fatal construct.
//
// To avoid flagging an escaped backslash immediately followed by a
// literal "u" (e.g. the JS source text "\\u2713" is an escaped backslash
// character followed by the four literal characters u, 2, 7, 1, 3 -- NOT
// a unicode escape), consecutive backslashes are counted as a run: only an
// ODD number of them leaves one active, unpaired escape introducer for the
// byte that follows. An even run is entirely escaped-backslash pairs and
// introduces nothing.
func FindUnicodeEscapes(src []byte) []string {
	masked := maskComments(src)
	n := len(masked)

	var found []string
	seen := make(map[string]bool)

	i := 0
	for i < n {
		if masked[i] != '\\' {
			i++
			continue
		}
		// Consume the whole run of consecutive backslashes starting at i.
		j := i
		for j < n && masked[j] == '\\' {
			j++
		}
		run := j - i
		if run%2 == 1 && j < n && masked[j] == 'u' {
			// masked[j-1] is the one active (unpaired) backslash; masked[j]
			// is 'u'. Capture as much of the escape as is present so the
			// reported snippet is meaningful, without requiring it to be
			// well-formed (the device rejects \uXXXX as a lexical class,
			// not just well-formed instances of it).
			end := j + 1
			if end < n && masked[end] == '{' {
				k := end + 1
				for k < n && masked[k] != '}' {
					k++
				}
				if k < n {
					k++ // include the closing brace
				}
				end = k
			} else {
				limit := end + 4
				for end < limit && end < n && isHexDigit(masked[end]) {
					end++
				}
			}
			snippet := string(masked[j-1 : end])
			if !seen[snippet] {
				seen[snippet] = true
				found = append(found, snippet)
			}
			i = end
			continue
		}
		i = j
	}
	return found
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// rejectUnicodeEscapes is the single call site shared by every caller that
// wants to enforce the device-fidelity guard (Minify, MinifyWithOptions,
// Run/RunWithDeviceState): it runs FindUnicodeEscapes over code and turns
// a non-empty result into a descriptive error naming the real device
// error message, or returns nil when code is clear. label is a short
// caller-supplied string (e.g. a script name or "minify test.js") used to
// prefix the error; named "label" rather than "context" to avoid shadowing
// the stdlib context package that several callers of this file's package
// also import.
//
// In practice, today's minifiers (both tdewolff and esbuild, the latter
// via minify_esbuild.go's Charset: api.CharsetUTF8) NORMALIZE an existing
// \uXXXX escape in the input into the equivalent raw UTF-8 character in
// their output, rather than passing the escape through unchanged -- so
// this validation, when called after minification, mostly guards against
// a FUTURE minifier engine/config regression reintroducing escapes (which
// is exactly what an ASCII-charset esbuild config did before the Charset
// fix), rather than something reachable through today's minify paths. It
// is still wired into Minify/MinifyWithOptions for that reason; see
// unicode_escape_test.go for a direct unit test of this function using
// synthetic escaped input, since real minifiers can't be coaxed into
// producing escaped output to test against.
func rejectUnicodeEscapes(label string, code []byte) error {
	found := FindUnicodeEscapes(code)
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("%s: contains device-rejected escape sequence(s) %v -- real Shelly firmware raises %q for these (see FindUnicodeEscapes)", label, found, uXXXXDeviceError)
}
