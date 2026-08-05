package script

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

const esbuildTestScript = `
var CONFIG = {};
var STATE = { count: 0 };

function log(msg) {
  print(msg);
}

function handleMorningStart() {
  STATE.count = STATE.count + 1;
  log("morning start, count=" + STATE.count);
}

function handleEveningStop() {
  STATE.count = 0;
  log("evening stop");
}

function internalHelper() {
  return STATE.count * 2;
}

handleMorningStart();
`

func assertES5(t *testing.T, code []byte) {
	t.Helper()
	if found := FindES6Syntax(code); len(found) > 0 {
		t.Errorf("minified output contains ES6-only marker(s) %v:\n%s", found, code)
	}
}

// commaCollapseTestScript mirrors the shape of prometheus-metrics.js's
// _buildSwitchMetrics (see internal/shelly/scripts/prometheus-metrics.js),
// the function identified in issue #266 as crashing a real Shelly device
// with "Too much recursion": a run of sequential `result += f(...)`
// statements. tdewolff's minifier collapses these into a single
// comma-expression inside the `return`, and (confirmed by direct
// comparison during this change) esbuild's MinifySyntax pass does exactly
// the same thing by default -- MinifyOptions.DisableSyntaxLowering exists
// to opt out of that specific behavior.
const commaCollapseTestScript = `
function buildMetrics(sw) {
  var result = "";
  result += metric("a", sw);
  result += metric("b", sw);
  result += metric("c", sw);
  result += metric("d", sw);
  result += metric("e", sw);
  result += metric("f", sw);
  return result;
}
function metric(name, sw) {
  return name + ":" + sw;
}
`

// TestMinifyWithOptions_EsbuildCollapsesSequentialStatementsByDefault
// documents that esbuild's default behavior (MinifySyntax on, same as
// MinifyWithOptions' EngineEsbuild default) reproduces the exact
// comma-expression-in-return pattern from issue #266 -- i.e. esbuild does
// NOT sidestep that bug by default. See DisableSyntaxLowering for the
// opt-out (unverified on real hardware).
func TestMinifyWithOptions_EsbuildCollapsesSequentialStatementsByDefault(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(commaCollapseTestScript), MinifyOptions{
		Engine: EngineEsbuild,
	})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	code := string(res.Code)
	// The six `result += metric(...)` statements collapse into one
	// comma-expression: "return a+=...,a+=...,...,a" -- i.e. the `return`
	// keyword is followed by an expression containing at least 5 top-level
	// commas before the closing `}` of the function, with NO `;` in
	// between (statements got joined, not just whitespace-compacted).
	if !strings.Contains(code, "return") {
		t.Fatalf("expected output to contain a return statement, got:\n%s", code)
	}
	returnIdx := strings.Index(code, "return")
	closeBraceIdx := strings.Index(code[returnIdx:], "}")
	if closeBraceIdx < 0 {
		t.Fatalf("could not find end of the return statement in:\n%s", code)
	}
	returnStmt := code[returnIdx : returnIdx+closeBraceIdx]
	if strings.Contains(returnStmt, ";") {
		t.Fatalf("expected statements to be collapsed into the return (no ';' inside it) by default, got: %q", returnStmt)
	}
	if commas := strings.Count(returnStmt, ","); commas < 5 {
		t.Fatalf("expected at least 5 top-level commas in the collapsed return statement, got %d: %q", commas, returnStmt)
	}
}

// TestMinifyWithOptions_DisableSyntaxLoweringKeepsStatementsSeparate is the
// counterpart to the test above: with DisableSyntaxLowering set, the same
// six statements must stay separate (terminated by ';', not folded into a
// comma-expression) -- avoiding the structural pattern issue #266
// identified, though this is a static-analysis result only, not a
// hardware-verified fix.
func TestMinifyWithOptions_DisableSyntaxLoweringKeepsStatementsSeparate(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(commaCollapseTestScript), MinifyOptions{
		Engine:                EngineEsbuild,
		DisableSyntaxLowering: true,
	})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	assertES5(t, res.Code)
	code := string(res.Code)
	returnIdx := strings.Index(code, "return")
	if returnIdx < 0 {
		t.Fatalf("expected output to contain a return statement, got:\n%s", code)
	}
	// With syntax lowering disabled, the statement immediately preceding
	// "return" must be semicolon-terminated (statements kept separate),
	// not comma-joined into the return expression.
	before := strings.TrimRight(code[:returnIdx], " \t\n")
	if !strings.HasSuffix(before, ";") && !strings.HasSuffix(before, "{") {
		t.Fatalf("expected the statement before 'return' to end with ';' (kept separate), got tail: %q", before[max(0, len(before)-20):])
	}
}

func TestMinifyWithOptions_TdewolffDefault(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), MinifyOptions{})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	if len(res.Code) == 0 {
		t.Fatal("expected non-empty minified code")
	}
	if res.Symbols != nil {
		t.Fatalf("tdewolff engine should never produce a symbol map, got %v", res.Symbols)
	}
	// tdewolff never mangles top-level names -- they must all survive verbatim.
	for _, name := range []string{"CONFIG", "STATE", "handleMorningStart", "handleEveningStop"} {
		if !strings.Contains(string(res.Code), name) {
			t.Errorf("expected top-level name %q to survive tdewolff minification, output:\n%s", name, res.Code)
		}
	}
}

func TestMinifyWithOptions_EsbuildNoTopLevelMangling(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), MinifyOptions{
		Engine: EngineEsbuild,
	})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	assertES5(t, res.Code)
	if res.Symbols != nil {
		t.Fatalf("esbuild without MangleTopLevel should not produce a symbol map, got %v", res.Symbols)
	}
	for _, name := range []string{"CONFIG", "STATE", "handleMorningStart", "handleEveningStop"} {
		if !strings.Contains(string(res.Code), name) {
			t.Errorf("expected top-level name %q to survive un-mangled esbuild minification, output:\n%s", name, res.Code)
		}
	}
}

func TestMinifyWithOptions_EsbuildTopLevelMangling(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), MinifyOptions{
		Engine:          EngineEsbuild,
		MangleTopLevel:  true,
		PreserveGlobals: []string{"handleMorningStart", "handleEveningStop"},
	})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	assertES5(t, res.Code)

	code := string(res.Code)

	// Preserved names must remain reachable as literal top-level tokens.
	for _, name := range []string{"handleMorningStart", "handleEveningStop"} {
		if !strings.Contains(code, name) {
			t.Errorf("expected preserved global %q to remain reachable, output:\n%s", name, code)
		}
	}

	// internalHelper was never preserved or debug-exported, and is
	// referenced nowhere outside the closure -- it should have been
	// renamed away (i.e. its literal name should no longer appear).
	if strings.Contains(code, "internalHelper") {
		t.Errorf("expected non-preserved top-level name %q to be mangled away, output:\n%s", "internalHelper", code)
	}

	if res.SourceMap == nil {
		t.Fatal("expected a source map when MangleTopLevel is set")
	}
	if res.Symbols == nil || len(res.Symbols.Symbols) == 0 {
		t.Fatal("expected a non-empty symbol map when MangleTopLevel is set")
	}

	// internalHelper must appear as an ORIGINAL name somewhere in the
	// symbol map (i.e. we can recover it even though it's not preserved).
	found := false
	for _, orig := range res.Symbols.Symbols {
		if orig == "internalHelper" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected symbol map to contain an entry for internalHelper, got %v", res.Symbols.Symbols)
	}
}

func TestMinifyWithOptions_UnknownPreserveGlobalErrors(t *testing.T) {
	_, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), MinifyOptions{
		Engine:          EngineEsbuild,
		MangleTopLevel:  true,
		PreserveGlobals: []string{"doesNotExist"},
	})
	if err == nil {
		t.Fatal("expected an error for a PreserveGlobals name that isn't a top-level declaration")
	}
}

func TestMinifyWithOptions_DebugExports(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), MinifyOptions{
		Engine:         EngineEsbuild,
		MangleTopLevel: true,
		DebugExports:   []string{"STATE", "CONFIG"},
	})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	code := string(res.Code)
	for _, name := range []string{"STATE", "CONFIG"} {
		if !strings.Contains(code, name) {
			t.Errorf("expected debug-exported name %q to remain reachable, output:\n%s", name, code)
		}
	}
}

// TestMinifyOptions_ZeroValueIsTdewolff documents the "Default OFF"
// safety property the whole minifier-selection design leans on: a
// MinifyOptions{} zero value (e.g. an uninitialized struct field, or a
// config option nobody set) must minify exactly like today's tdewolff
// path, not silently pick esbuild or top-level mangling.
func TestMinifyOptions_ZeroValueIsTdewolff(t *testing.T) {
	res, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), MinifyOptions{})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	if res.Symbols != nil || res.SourceMap != nil {
		t.Fatalf("zero-value MinifyOptions must never produce a symbol map or source map, got Symbols=%v SourceMap=%v", res.Symbols, res.SourceMap)
	}
}

// TestMinifyWithOptions_EsbuildEmitsRawUTF8NotEscapes is a regression test for a
// bug found only on real hardware: esbuild defaults to ASCII output and escapes
// non-ASCII characters as \uXXXX, but Espruino rejects those outright with
//
//	Uncaught SyntaxError: \uXXXX literals are disallowed
//
// pool-pump.js contains a "✓" (U+2713) and several em dashes (U+2014) in log
// strings, so the whole script failed to parse on a Shelly Plus 1 — while goja
// and the rest of the test suite stayed green, because goja accepts \uXXXX
// happily. Fixed by setting Charset: api.CharsetUTF8 on both api.Transform call
// sites. This affects esbuild output with AND without top-level mangling, so
// both paths are asserted here.
func TestMinifyWithOptions_EsbuildEmitsRawUTF8NotEscapes(t *testing.T) {
	src := []byte(`
var CONFIG_SCHEMA = { tick: { description: "x", key: "tick", default: 1, type: "number" } };
function handleTick() { print("✓ done — really"); }
handleTick();
`)

	escape := regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)

	for _, tc := range []struct {
		name   string
		mangle bool
	}{
		{"no mangling", false},
		{"top-level mangling", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := MinifyOptions{Engine: EngineEsbuild, MangleTopLevel: tc.mangle}
			if tc.mangle {
				opts.PreserveGlobals = []string{"handleTick"}
			}
			res, err := MinifyWithOptions("charset-test.js", src, opts)
			if err != nil {
				t.Fatalf("MinifyWithOptions: %v", err)
			}
			if got := escape.FindAllString(string(res.Code), -1); len(got) > 0 {
				t.Errorf("esbuild emitted %d \\uXXXX escape(s) %v; Espruino rejects these "+
					"(\"\\uXXXX literals are disallowed\") and the script will not parse on device", len(got), got)
			}
			if !utf8.Valid(res.Code) {
				t.Error("minified output is not valid UTF-8")
			}
		})
	}
}
