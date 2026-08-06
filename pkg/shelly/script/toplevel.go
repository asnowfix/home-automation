package script

// topLevelDeclarations returns the names of every identifier declared with
// `function NAME(...)` or `var NAME [= ...]` at the outermost brace depth of
// src (i.e. not nested inside any function body, block, or object/array
// literal). This is the "renameable top-level symbol" set referenced by
// MinifyOptions.PreserveGlobals/DebugExports and by buildSymbolMap.
//
// The scan is a small hand-written state machine rather than a regexp:
// object/array literals like `var CONFIG = {a:1, b:2};` open and close
// braces WITHIN a single top-level statement, so a naive per-line or
// per-brace-toggle regexp would misclassify their contents. Tracking one
// depth counter across the whole file, incremented for every `{ ( [` and
// decremented for every `} ) ]` (with strings and comments masked out
// first), correctly returns to 0 by the end of such a statement -- so
// "depth == 0" at a `function`/`var` keyword reliably means "this is a
// genuine top-level declaration", including through nested function
// expressions used as object properties.
func topLevelDeclarations(src []byte) []string {
	masked := maskStringsAndComments(src)
	n := len(masked)

	var names []string
	seen := make(map[string]bool)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}

	wordAt := func(i int) (string, int) {
		j := i
		for j < n && isIdentPart(masked[j]) {
			j++
		}
		return string(masked[i:j]), j
	}
	precededByIdentChar := func(i int) bool {
		return i > 0 && isIdentPart(masked[i-1])
	}
	skipSpace := func(i int) int {
		for i < n && isSpaceByte(masked[i]) {
			i++
		}
		return i
	}

	depth := 0
	i := 0
	for i < n {
		c := masked[i]
		switch c {
		case '{', '(', '[':
			depth++
			i++
		case '}', ')', ']':
			if depth > 0 {
				depth--
			}
			i++
		default:
			if !isIdentStart(c) || precededByIdentChar(i) {
				i++
				continue
			}
			word, next := wordAt(i)
			if depth != 0 {
				i = next
				continue
			}
			switch word {
			case "function":
				k := skipSpace(next)
				if k < n && isIdentStart(masked[k]) {
					name, k2 := wordAt(k)
					add(name)
					i = k2
					continue
				}
				i = next
			case "var":
				i = scanVarDeclarators(masked, next, add)
			default:
				i = next
			}
		}
	}
	return names
}

// scanVarDeclarators parses a top-level `var a [= expr], b [= expr], ...;`
// statement starting right after the `var` keyword at position i, calling
// add for each declared identifier, and returns the position just past the
// statement's terminating `;` (or end of input / unmatched brace if the
// source is malformed -- best-effort, this is a diagnostic tool, not a full
// parser).
func scanVarDeclarators(masked []byte, i int, add func(string)) int {
	n := len(masked)
	for {
		for i < n && isSpaceByte(masked[i]) {
			i++
		}
		if i >= n || !isIdentStart(masked[i]) {
			break
		}
		j := i
		for j < n && isIdentPart(masked[j]) {
			j++
		}
		add(string(masked[i:j]))
		i = j
		for i < n && isSpaceByte(masked[i]) {
			i++
		}
		// Skip any initializer, tracking bracket depth locally so a nested
		// object/array literal's commas don't get mistaken for declarator
		// separators.
		local := 0
		for i < n {
			ch := masked[i]
			switch ch {
			case '{', '(', '[':
				local++
				i++
				continue
			case '}', ')', ']':
				if local > 0 {
					local--
				}
				i++
				continue
			}
			if local == 0 && (ch == ',' || ch == ';') {
				break
			}
			i++
		}
		if i < n && masked[i] == ',' {
			i++
			continue
		}
		if i < n && masked[i] == ';' {
			i++
		}
		break
	}
	return i
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// maskStringsAndComments returns a copy of src with the CONTENTS of every
// string literal and comment replaced by spaces (delimiters kept as-is
// where harmless, dropped where not). Bracket characters that appear inside
// a string or comment are erased so brace-depth tracking elsewhere never
// misreads them; everything else (whitespace, statement structure) keeps
// its original byte offsets so line/column-free callers can still reason
// about position within the file.
func maskStringsAndComments(src []byte) []byte {
	return maskSource(src, true)
}

// maskComments returns a copy of src with only comment CONTENTS blanked out
// (same string/comment recognition as maskStringsAndComments); STRING
// literal contents are left completely intact. Used by FindUnicodeEscapes
// in es5check.go: Espruino's tokenizer never re-lexes comment text for
// escape sequences, so a comment merely mentioning `\uXXXX` as prose is not
// a real device-fatal construct and must not be flagged, whereas the same
// text inside a string literal is exactly the kind of thing that crashes
// real hardware -- see FindUnicodeEscapes for the full story.
func maskComments(src []byte) []byte {
	return maskSource(src, false)
}

// maskSource is the shared scanner behind maskStringsAndComments and
// maskComments. When maskStrings is true it reproduces
// maskStringsAndComments' original behavior exactly (string contents AND
// comment contents blanked); when false, string RECOGNITION (so `//` or
// `/*` bytes inside a string aren't misread as starting a comment, and
// escaped quotes don't end the string early) still happens, but string
// bytes are left untouched in the output -- only comment contents are
// blanked.
func maskSource(src []byte, maskStrings bool) []byte {
	out := make([]byte, len(src))
	copy(out, src)

	inStr := false
	strQuote := byte(0)
	esc := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(out); i++ {
		b := out[i]

		if inStr {
			if esc {
				esc = false
			} else if b == '\\' {
				esc = true
			} else if b == strQuote {
				inStr = false
			}
			if maskStrings && b != '\n' {
				out[i] = ' '
			}
			continue
		}
		if inLineComment {
			if b == '\n' {
				inLineComment = false
			} else {
				out[i] = ' '
			}
			continue
		}
		if inBlockComment {
			if b == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i] = ' '
				out[i+1] = ' '
				i++
				inBlockComment = false
			} else if b != '\n' {
				out[i] = ' '
			}
			continue
		}

		if b == '/' && i+1 < len(out) {
			if out[i+1] == '/' {
				inLineComment = true
				out[i] = ' '
				out[i+1] = ' '
				i++
				continue
			}
			if out[i+1] == '*' {
				inBlockComment = true
				out[i] = ' '
				out[i+1] = ' '
				i++
				continue
			}
		}

		if b == '\'' || b == '"' || b == '`' {
			inStr = true
			strQuote = b
			// Keep the opening delimiter as a space too (maskStrings mode
			// only) so a lone quote byte can't be misread as anything
			// meaningful; only its position (not content) matters to
			// those callers.
			if maskStrings {
				out[i] = ' '
			}
			continue
		}

		// default: leave byte as-is
	}

	return out
}
