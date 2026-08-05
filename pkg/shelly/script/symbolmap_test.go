package script

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSymbolMap_MangleDemangleRoundTrip mangles a synthetic script with
// top-level mangling enabled, builds a synthetic Shelly-style crash message
// using the MANGLED names (the way a real device would report it), and
// verifies Demangle recovers the original names -- including a message
// shaped like a real Shelly error, which gives no line/column, only
// function names and a code snippet (see the SymbolMap doc comment for why
// that's the whole reason this table exists).
func TestSymbolMap_MangleDemangleRoundTrip(t *testing.T) {
	res, err := MinifyWithOptions("pool-pump.js", []byte(esbuildTestScript), MinifyOptions{
		Engine:          EngineEsbuild,
		MangleTopLevel:  true,
		PreserveGlobals: []string{"handleMorningStart", "handleEveningStop"},
	})
	if err != nil {
		t.Fatalf("MinifyWithOptions: %v", err)
	}
	if res.Symbols == nil {
		t.Fatal("expected a symbol map")
	}

	// Find the mangled name for "internalHelper" and "STATE" via the map's
	// reverse direction (original -> mangled), the way a developer working
	// backward from the source would.
	mangledOf := func(orig string) string {
		for m, o := range res.Symbols.Symbols {
			if o == orig {
				return m
			}
		}
		t.Fatalf("no mangled name found for original %q in %v", orig, res.Symbols.Symbols)
		return ""
	}
	mangledHelper := mangledOf("internalHelper")
	mangledState := mangledOf("STATE")

	// Shape a synthetic crash message the way Shelly actually reports
	// errors: function name + a code snippet, no line/column at all. See
	// https://github.com/asnowfix/home-automation/issues/266 for a real
	// example of this exact shape.
	crash := `Uncaught Error: Too much recursion
 in function "` + mangledHelper + `" called from ` + mangledHelper + `(` + mangledState + `.count)`

	demangled := res.Symbols.Demangle(crash)

	if !strings.Contains(demangled, "internalHelper") {
		t.Errorf("expected demangled message to contain original name %q, got: %s", "internalHelper", demangled)
	}
	if !strings.Contains(demangled, "STATE") {
		t.Errorf("expected demangled message to contain original name %q, got: %s", "STATE", demangled)
	}
	if strings.Contains(demangled, mangledHelper+"(") {
		// Loose check: the mangled call-site token shouldn't survive.
		t.Errorf("expected mangled name %q to be replaced, got: %s", mangledHelper, demangled)
	}
}

func TestSymbolMap_DemangleRequiresWholeIdentifierMatch(t *testing.T) {
	m := &SymbolMap{
		Script: "test.js",
		Engine: EngineEsbuild,
		Symbols: map[string]string{
			"a": "STATE",
		},
	}
	// "a" must NOT match inside "abc", "a1", or "_a1" -- but a standalone
	// "a" token, wherever it appears (including as a lone function
	// argument), is a legitimate whole-identifier match.
	in := `catch(a){} abc a1 _a1 (a) a.b a,b`
	out := m.Demangle(in)
	want := `catch(STATE){} abc a1 _a1 (STATE) STATE.b STATE,b`
	if out != want {
		t.Fatalf("Demangle() = %q, want %q", out, want)
	}
}

func TestSymbolMap_DemangleNilSafe(t *testing.T) {
	var m *SymbolMap
	if got := m.Demangle("hello a b c"); got != "hello a b c" {
		t.Fatalf("nil SymbolMap.Demangle should be a no-op, got %q", got)
	}

	empty := &SymbolMap{}
	if got := empty.Demangle("hello a b c"); got != "hello a b c" {
		t.Fatalf("empty SymbolMap.Demangle should be a no-op, got %q", got)
	}
}

func TestSymbolMap_WriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "pool-pump.symbols.json")

	m := &SymbolMap{
		Script: "pool-pump.js",
		Engine: EngineEsbuild,
		Symbols: map[string]string{
			"t": "handleMorningStart",
			"a": "handleEveningStop",
			"n": "STATE",
		},
	}

	if err := WriteSymbolMap(path, m); err != nil {
		t.Fatalf("WriteSymbolMap: %v", err)
	}

	got, err := ReadSymbolMap(path)
	if err != nil {
		t.Fatalf("ReadSymbolMap: %v", err)
	}
	if got.Script != m.Script || got.Engine != m.Engine || len(got.Symbols) != len(m.Symbols) {
		t.Fatalf("round-tripped SymbolMap = %+v, want %+v", got, m)
	}
	for k, v := range m.Symbols {
		if got.Symbols[k] != v {
			t.Errorf("Symbols[%q] = %q, want %q", k, got.Symbols[k], v)
		}
	}
}

func TestDecodeMappings_Basic(t *testing.T) {
	// "AAAA" decodes to a single all-zero segment (genCol=0, srcIdx=0,
	// srcLine=0, srcCol=0), the standard smoke-test mapping used across
	// most source-map decoder implementations.
	segs, err := decodeMappings("AAAA")
	if err != nil {
		t.Fatalf("decodeMappings: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d: %+v", len(segs), segs)
	}
	if segs[0].genLine != 0 || segs[0].genCol != 0 || segs[0].nameIdx != -1 {
		t.Fatalf("unexpected segment: %+v", segs[0])
	}
}

func TestDecodeMappings_InvalidByte(t *testing.T) {
	if _, err := decodeMappings("!!!!"); err == nil {
		t.Fatal("expected an error for invalid base64-vlq input")
	}
}
