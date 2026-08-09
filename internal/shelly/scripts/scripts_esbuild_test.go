package scripts

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// esbuildES5DownlevelUnsupported is esbuild's fixed error phrasing when
// asked to target ES5 for syntax it cannot downlevel to ES5 at all (notably
// let/const/classes -- as opposed to syntax it simply won't ever emit, like
// arrow functions, which it CAN downlevel). This is a real, current
// limitation of esbuild itself, not a bug in this package's wrapping code:
// a handful of this repo's existing scripts already use let/const directly
// in source (and apparently run fine on real Shelly hardware -- Espruino's
// "limited ES6" support evidently covers them), which the tdewolff path
// left untouched because it doesn't transpile syntax either. Until those
// scripts are rewritten to `var`-only (a separate, larger cleanup, out of
// scope here) or esbuild adds ES5 let/const downleveling, they simply
// cannot go through the esbuild path -- MinifyWithOptions fails loudly
// with this error rather than risking silently-wrong output, which is the
// safe failure mode. Scripts that hit this are logged and skipped rather
// than failing the suite.
const esbuildES5DownlevelUnsupported = "is not supported yet"

// TestSmokeAllScriptsEsbuild extends TestSmokeAllScripts (tdewolff path) to
// the new esbuild engine, for every embedded script, in two modes:
//
//  1. esbuild, no top-level mangling (local-identifier mangling only, same
//     scope as today's tdewolff path).
//  2. esbuild, top-level mangling ON, using this script's registered
//     PreserveGlobals (see MinifyOptionsFor) -- verifies the preserved
//     names are still present in the output as literal tokens, i.e. still
//     reachable by Shelly Schedule jobs that eval them by name.
//
// Every mode is checked for ES5-only syntax and run through the goja
// emulator harness, exactly like the tdewolff smoke test.
func TestSmokeAllScriptsEsbuild(t *testing.T) {
	minifyOnly := map[string]string{
		"universal-blu-to-mqtt.js": "uses BLE.Scanner (hardware-only API)",
	}
	perScriptState := map[string]*script.DeviceState{}

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	entries, err := fs.ReadDir(GetFS(), ".")
	if err != nil {
		t.Fatalf("failed to read embedded script FS: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		name := entry.Name()

		t.Run(name, func(t *testing.T) {
			rawBuf, err := fs.ReadFile(GetFS(), name)
			if err != nil {
				t.Fatalf("read embedded script: %v", err)
			}

			modes := []struct {
				label string
				opts  script.MinifyOptions
			}{
				{"no top-level mangling", MinifyOptionsFor(name, script.EngineEsbuild, false, false)},
				{"top-level mangling", MinifyOptionsFor(name, script.EngineEsbuild, true, false)},
			}

			for _, mode := range modes {
				t.Run(mode.label, func(t *testing.T) {
					res, err := script.MinifyWithOptions(name, rawBuf, mode.opts)
					if err != nil {
						if strings.Contains(err.Error(), esbuildES5DownlevelUnsupported) {
							t.Skipf("script uses syntax esbuild cannot downlevel to ES5 (e.g. let/const) -- skipping, see esbuildES5DownlevelUnsupported doc comment: %v", err)
						}
						t.Fatalf("esbuild minify (%s) failed: %v", mode.label, err)
					}
					if found := script.FindES6Syntax(res.Code); len(found) > 0 {
						t.Errorf("esbuild output contains ES6-only marker(s) %v", found)
					}

					// Every registered PreserveGlobals name must remain a
					// literal token in the output -- the whole point of
					// the preserve-list.
					for _, preserved := range mode.opts.PreserveGlobals {
						if !strings.Contains(string(res.Code), preserved) {
							t.Errorf("preserved global %q not found in esbuild output (%s)", preserved, mode.label)
						}
					}

					if reason, ok := minifyOnly[name]; ok {
						t.Logf("minify-only (skipping goja run): %s", reason)
						return
					}

					ctx, cancel := context.WithTimeout(
						logr.NewContext(context.Background(), testr.New(t)),
						smokeTimeout,
					)
					defer cancel()

					deviceState, ok := perScriptState[name]
					if !ok {
						deviceState = &script.DeviceState{
							KVS:             make(map[string]interface{}),
							Storage:         make(map[string]interface{}),
							ComponentStatus: genericComponentStatus(),
						}
					}

					runErr := script.RunWithDeviceState(ctx, name, res.Code, false, deviceState)
					if runErr != nil &&
						!errors.Is(runErr, context.Canceled) &&
						!errors.Is(runErr, context.DeadlineExceeded) {
						t.Fatalf("script failed to initialise under esbuild (%s): %v", mode.label, runErr)
					}
				})
			}
		})
	}
}

// TestSmokeAllScriptsEsbuild_SymbolMapCoversPreservedNames verifies, for
// every script with a registered preserve/debug-export list, that the
// symbol map produced alongside top-level-mangled output actually contains
// entries -- i.e. VLQ-decoding the source map against these real,
// much-larger-than-the-synthetic-test-fixture scripts (pool-pump.js is
// ~75KB) works and yields a non-trivial mapping, and that every
// preserved/debug-exported ORIGINAL name has at least one internal mangled
// call site recorded.
//
// Note: a preserved/debug-exported name being reachable as a literal
// top-level global (via the outer re-export assignment) does NOT mean it
// has no mangled form -- quite the opposite. Every reference to it INSIDE
// the wrapped closure (e.g. "STATE" read/written ~160 times throughout
// pool-pump.js) still resolves to the local declaration and gets mangled
// like any other local reference; only the one, extra, outer re-export
// statement keeps the literal name callable/inspectable from outside. So
// this test asserts each preserved/debug name DOES appear as an original
// name in the symbol map (the internal savings are still being captured),
// not that it's absent.
func TestSmokeAllScriptsEsbuild_SymbolMapCoversPreservedNames(t *testing.T) {
	for name, overrides := range scriptMinifyOverrides {
		name, overrides := name, overrides
		t.Run(name, func(t *testing.T) {
			rawBuf, err := fs.ReadFile(GetFS(), name)
			if err != nil {
				t.Fatalf("read embedded script: %v", err)
			}

			opts := MinifyOptionsFor(name, script.EngineEsbuild, true, true) // include debug exports too
			res, err := script.MinifyWithOptions(name, rawBuf, opts)
			if err != nil {
				t.Fatalf("esbuild minify failed: %v", err)
			}
			if res.Symbols == nil || len(res.Symbols.Symbols) == 0 {
				t.Fatal("expected a non-empty symbol map")
			}

			originals := make(map[string]bool, len(res.Symbols.Symbols))
			for _, o := range res.Symbols.Symbols {
				originals[o] = true
			}

			for _, name := range append(append([]string{}, overrides.PreserveGlobals...), overrides.DebugExports...) {
				if !originals[name] {
					t.Errorf("expected preserved/debug-exported name %q to still have an internal mangled call site recorded in the symbol map", name)
				}
			}

			t.Logf("%s: %d top-level symbols mapped", name, len(res.Symbols.Symbols))
		})
	}
}
