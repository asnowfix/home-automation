package script

import (
	"strings"
	"testing"
)

// TestMinify_PreservesUnusedCatchBinding proves that Minify (pkg/shelly/script)
// does not collapse `catch (e) { ... }` into the ES2019 optional-catch-binding
// form `catch { ... }` when the catch parameter is never referenced inside the
// block.
//
// Shelly's modified Espruino engine cannot parse optional catch binding — see
// the "Never empty catch blocks" rule in CLAUDE.md's Shelly JavaScript section.
// Without an explicit ES5 target on the tdewolff minifier, its default
// behaviour (unbounded ECMAScript version) strips the unused binding and
// produces a syntax error on-device.
func TestMinify_PreservesUnusedCatchBinding(t *testing.T) {
	src := []byte(`
		function f() {
			try {
				risky();
			} catch (e) {
				fallback();
			}
		}
	`)

	out, err := Minify(src)
	if err != nil {
		t.Fatalf("Minify error: %v", err)
	}

	if strings.Contains(string(out), "catch{") || strings.Contains(string(out), "catch(){") {
		t.Fatalf("minifier collapsed the catch binding into optional-catch-binding form (ES2019), "+
			"which Shelly's Espruino engine cannot parse; got: %s", out)
	}
	if !strings.Contains(string(out), "catch(e)") {
		t.Fatalf("expected minified output to keep an explicit catch binding (catch(e){...}); got: %s", out)
	}
}
