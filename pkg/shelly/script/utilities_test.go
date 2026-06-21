package script

import (
	"testing"

	"github.com/dop251/goja"
)

func TestUtilitiesKnownVectors(t *testing.T) {
	vm := goja.New()
	installUtilities(vm)

	tests := []struct {
		name string
		expr string
		want string
	}{
		{"btoa", `btoa("hello")`, "aGVsbG8="},
		{"atob", `atob("aGVsbG8=")`, "hello"},
		{"btoh", `btoh("hi")`, "6869"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := vm.RunString(tt.expr)
			if err != nil {
				t.Fatalf("%s: %v", tt.expr, err)
			}
			if got := result.String(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestUtilitiesRoundTrip(t *testing.T) {
	vm := goja.New()
	installUtilities(vm)

	result, err := vm.RunString(`atob(btoa("round trip \x00\xff test"))`)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	// \xff in a Go string literal is the raw byte 0xFF (invalid UTF-8 alone);
	// the JS round trip instead produces the Unicode code point U+00FF, whose
	// Go literal is ÿ (a valid 2-byte UTF-8 sequence). Same byte value
	// 0xFF, different Go source notation.
	want := "round trip \x00ÿ test"
	if got := result.String(); got != want {
		t.Errorf("round trip = %q, want %q", got, want)
	}
}

func TestBtoaRejectsOutOfRangeChar(t *testing.T) {
	vm := goja.New()
	installUtilities(vm)

	result, err := vm.RunString(`
		(function() {
			try {
				btoa("ሴ");
				return "no error thrown";
			} catch (e) {
				return String(e);
			}
		})();
	`)
	if err != nil {
		t.Fatalf("RunString returned a Go error (exception escaped the VM): %v", err)
	}
	if result.String() == "no error thrown" {
		t.Error("btoa accepted a character outside the byte range, want a thrown exception")
	}
}
