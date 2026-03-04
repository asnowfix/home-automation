// Package script — Shelly JavaScript engine compatibility tests.
//
// These tests document the constraints of the Shelly Espruino-based JavaScript
// engine by running equivalent code in goja (a standards-compliant ES5.1+
// interpreter). Where goja and Shelly diverge, the test documents which pattern
// is safe and why.
//
// Key constraints (from AGENTS.md):
//   - No hoisting of function expressions (declarations ARE hoisted in standard JS)
//   - catch (e) {} is required; catch {} (optional catch binding) may not work
//   - Array.prototype.shift/unshift are NOT supported on Shelly
//   - Max 2-3 levels of nested anonymous callbacks
//   - "key" in obj is safer than obj.key !== undefined after minification
//   - var is safer than let/const for maximum compatibility
//   - Function.prototype.bind() works fine
//   - ES5 array methods (map, filter, forEach, reduce, indexOf) all work
package script

import (
	"testing"

	"github.com/dop251/goja"
)

// runJS is a test helper that runs JS in a new goja VM and returns the result.
func runJS(t *testing.T, src string) goja.Value {
	t.Helper()
	vm := goja.New()
	v, err := vm.RunString(src)
	if err != nil {
		t.Fatalf("runJS error: %v\nsrc:\n%s", err, src)
	}
	return v
}

// runJSExpectError asserts that the given JS produces a runtime or compile error.
func runJSExpectError(t *testing.T, src string) {
	t.Helper()
	vm := goja.New()
	_, err := vm.RunString(src)
	if err == nil {
		t.Fatalf("expected an error, but JS ran successfully\nsrc:\n%s", src)
	}
}

// TestScript_FunctionExpressionNoHoisting documents that function expressions
// (var f = function(){}) are NOT hoisted in standard JS, and therefore cannot
// be called before their assignment. Shelly enforces this strictly.
// Always define functions before calling them.
func TestScript_FunctionExpressionNoHoisting(t *testing.T) {
	// Calling a function expression before its definition should fail.
	runJSExpectError(t, `
		greet(); // called before assignment
		var greet = function() { return "hello"; };
	`)
}

// TestScript_FunctionDeclarationIsHoisted documents that function declarations
// ARE hoisted in goja (standard ES5). This works in standard engines but may
// not on Shelly. Use explicit ordering to be safe on both.
func TestScript_FunctionDeclarationIsHoisted(t *testing.T) {
	// NOTE: This passes in goja (standard ES5) but may fail on Shelly.
	// Best practice: define all functions before calling them regardless.
	vm := goja.New()
	v, err := vm.RunString(`
		var result = add(1, 2);
		function add(a, b) { return a + b; }
		result;
	`)
	if err != nil {
		// Document this as a known goja behaviour (hoisting works).
		t.Logf("goja does NOT hoist function declarations: %v", err)
	} else {
		if v.ToInteger() != 3 {
			t.Errorf("expected 3, got %v", v)
		}
		t.Log("goja hoists function declarations; Shelly does NOT — always order functions before callers")
	}
}

// TestScript_CatchParameterRequired verifies that catch (e) {} with an explicit
// parameter always works. The optional-catch-binding form (catch {}) is ES2019
// and may not be available on Shelly's Espruino engine.
func TestScript_CatchParameterRequired(t *testing.T) {
	v := runJS(t, `
		var result = "none";
		try {
			throw new Error("boom");
		} catch (e) {
			result = "caught: " + e.message;
		}
		result;
	`)
	if v.String() != "caught: boom" {
		t.Errorf("expected \"caught: boom\", got %q", v.String())
	}
}

// TestScript_MinifierSafeCatchPattern documents the recommended empty-catch
// pattern that prevents the minifier from removing the catch parameter.
// Using `if (e && false) {}` keeps the parameter referenced.
func TestScript_MinifierSafeCatchPattern(t *testing.T) {
	v := runJS(t, `
		var parsed = null;
		try {
			parsed = JSON.parse("{\"ok\":true}");
		} catch (e) {
			if (e && false) {} // prevents minifier from stripping the parameter
		}
		parsed !== null;
	`)
	if !v.ToBoolean() {
		t.Error("expected parse to succeed and parsed to be non-null")
	}
}

// TestScript_ArrayShiftUnsupported documents that Array.prototype.shift() and
// unshift() are NOT available on Shelly. In goja they work (standard ES5);
// this test documents the safe alternative (manual loop).
func TestScript_ArrayShiftUnsupported(t *testing.T) {
	// In goja, shift() works. On Shelly it does NOT.
	// Safe pattern: manual loop to remove the first element.
	v := runJS(t, `
		var arr = [1, 2, 3];
		// Shelly-safe: manual "shift"
		var first = arr[0];
		var newArr = [];
		for (var i = 1; i < arr.length; i++) {
			newArr.push(arr[i]);
		}
		arr = newArr;
		first === 1 && arr.length === 2 && arr[0] === 2;
	`)
	if !v.ToBoolean() {
		t.Error("manual shift pattern should produce correct result")
	}
}

// TestScript_ArrayShiftWorksInGoja documents that shift() DOES work in goja
// (i.e. standard ES5), but must not be relied on for Shelly.
func TestScript_ArrayShiftWorksInGoja(t *testing.T) {
	v := runJS(t, `
		var arr = [10, 20, 30];
		var first = arr.shift();
		first === 10 && arr.length === 2;
	`)
	if !v.ToBoolean() {
		t.Error("shift() should work in goja (but NOT on Shelly — use the manual pattern)")
	}
}

// TestScript_InOperatorMinifierSafe verifies that the `in` operator correctly
// checks for property existence and is not altered by the minifier (unlike
// `obj.prop !== undefined` which may be rewritten unsafely).
func TestScript_InOperatorMinifierSafe(t *testing.T) {
	v := runJS(t, `
		var obj = { illuminance_min: 0, name: "sensor" };
		var hasMin   = ("illuminance_min" in obj);   // ← minifier-safe
		var hasMissing = ("nonexistent" in obj);
		hasMin && !hasMissing;
	`)
	if !v.ToBoolean() {
		t.Error("'in' operator should correctly detect present and absent properties")
	}
}

// TestScript_UndefinedCheckRisk documents why `!== undefined` is risky: the
// minifier can rewrite `obj.prop !== undefined` to `obj.prop` (relying on
// truthiness), which gives wrong results for zero/false/null values.
func TestScript_UndefinedCheckRisk(t *testing.T) {
	// This pattern is unsafe on Shelly because minifiers rewrite it.
	v := runJS(t, `
		var obj = { count: 0 };
		// RISKY: obj.count !== undefined is true here (0 !== undefined)
		// but if minified to just obj.count it becomes 0 (falsy).
		var safeCheck   = ("count" in obj);      // always correct
		var riskyCheck  = (obj.count !== undefined); // correct value, risky syntax
		safeCheck && riskyCheck; // both true for 0
	`)
	if !v.ToBoolean() {
		t.Error("both checks should return true for count=0")
	}
	t.Log("Use 'key' in obj instead of obj.key !== undefined to be minifier-safe")
}

// TestScript_VarOverLet verifies that `var` declarations work correctly, and
// documents that `let`/`const` may not be available on all Shelly firmware.
func TestScript_VarOverLet(t *testing.T) {
	v := runJS(t, `
		var x = 42;
		var y = x * 2;
		y;
	`)
	if v.ToInteger() != 84 {
		t.Errorf("expected 84, got %v", v)
	}
}

// TestScript_LetWorksInGoja documents that let/const work in goja but may fail
// on older Shelly firmware. Use var for maximum compatibility.
func TestScript_LetWorksInGoja(t *testing.T) {
	v := runJS(t, `
		let a = 10;
		const b = 20;
		a + b;
	`)
	if v.ToInteger() != 30 {
		t.Errorf("expected 30, got %v", v)
	}
	t.Log("let/const work in goja; use var for Shelly compatibility (firmware < Espruino v2.14)")
}

// TestScript_BindSupported verifies that Function.prototype.bind() works.
// This is explicitly listed as supported in Shelly's documentation.
func TestScript_BindSupported(t *testing.T) {
	v := runJS(t, `
		function greet(greeting) {
			return greeting + ", " + this.name;
		}
		var obj = { name: "World" };
		var boundGreet = greet.bind(obj, "Hello");
		boundGreet();
	`)
	if v.String() != "Hello, World" {
		t.Errorf("expected \"Hello, World\", got %q", v.String())
	}
}

// TestScript_ES5ArrayMethods verifies that ES5 array methods supported by
// Shelly all work correctly.
func TestScript_ES5ArrayMethods(t *testing.T) {
	tests := []struct {
		name string
		js   string
		want string
	}{
		{
			name: "map",
			js:   `[1,2,3].map(function(x){ return x*2; }).join(",");`,
			want: "2,4,6",
		},
		{
			name: "filter",
			js:   `[1,2,3,4].filter(function(x){ return x%2===0; }).join(",");`,
			want: "2,4",
		},
		{
			name: "forEach",
			js: `
				var sum = 0;
				[1,2,3].forEach(function(x){ sum += x; });
				String(sum);
			`,
			want: "6",
		},
		{
			name: "reduce",
			js:   `String([1,2,3,4].reduce(function(acc,x){ return acc+x; }, 0));`,
			want: "10",
		},
		{
			name: "indexOf",
			js:   `String([10,20,30].indexOf(20));`,
			want: "1",
		},
		{
			name: "isArray",
			js:   `String(Array.isArray([]) && !Array.isArray({}));`,
			want: "true",
		},
		{
			name: "push and pop",
			js: `
				var a = [1,2];
				a.push(3);
				var last = a.pop();
				String(last) + "," + String(a.length);
			`,
			want: "3,2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := runJS(t, tc.js)
			if v.String() != tc.want {
				t.Errorf("got %q, want %q", v.String(), tc.want)
			}
		})
	}
}

// TestScript_CallbackDepthLimit documents that deeply nested anonymous function
// callbacks will crash Shelly's JavaScript engine (>2-3 levels). This test
// verifies the safe pattern: named top-level functions.
func TestScript_CallbackDepthLimit_SafePattern(t *testing.T) {
	// Safe: named functions, not deeply nested.
	v := runJS(t, `
		function processItem(item, done) {
			done(item * 2);
		}

		function handleResult(result) {
			return result;
		}

		var output = [];
		[1, 2, 3].forEach(function(item) {
			processItem(item, function(r) {
				output.push(handleResult(r));
			});
		});
		output.join(",");
	`)
	if v.String() != "2,4,6" {
		t.Errorf("expected \"2,4,6\", got %q", v.String())
	}
	t.Log("Keep callback nesting ≤ 2-3 levels to avoid 'Too many calls in progress' on Shelly")
}

// TestScript_ObjectAssign verifies that Object.assign works (ES6, but
// supported on Shelly per documentation).
func TestScript_ObjectAssign(t *testing.T) {
	v := runJS(t, `
		var target = {a: 1};
		var source = {b: 2, c: 3};
		Object.assign(target, source);
		target.a + target.b + target.c;
	`)
	if v.ToInteger() != 6 {
		t.Errorf("expected 6, got %v", v)
	}
}

// TestScript_ObjectKeys verifies that Object.keys works.
func TestScript_ObjectKeys(t *testing.T) {
	v := runJS(t, `
		var obj = {x: 1, y: 2, z: 3};
		Object.keys(obj).length;
	`)
	if v.ToInteger() != 3 {
		t.Errorf("expected 3, got %v", v)
	}
}

// TestScript_JSONRoundTrip verifies JSON.parse and JSON.stringify work
// correctly (required for KVS communication on Shelly).
func TestScript_JSONRoundTrip(t *testing.T) {
	v := runJS(t, `
		var original = {name: "sensor", value: 42, active: true};
		var serialised = JSON.stringify(original);
		var restored = JSON.parse(serialised);
		restored.name === "sensor" && restored.value === 42 && restored.active === true;
	`)
	if !v.ToBoolean() {
		t.Error("JSON round-trip should produce identical values")
	}
}
