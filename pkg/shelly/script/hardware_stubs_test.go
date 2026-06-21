package script

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// TestHardwareStubsThrowCatchableException verifies that every hardware-only
// global (HTTPServer, BLE, BTHome, UART) is registered but throws a
// descriptive, try/catch-able exception instead of leaving scripts to hit an
// unrelated ReferenceError or silently misbehave.
func TestHardwareStubsThrowCatchableException(t *testing.T) {
	tests := []struct {
		name string
		call string
	}{
		{"HTTPServer.registerEndpoint", `HTTPServer.registerEndpoint("foo", function() {})`},
		{"BLE.advertiseOnce", `BLE.advertiseOnce({})`},
		{"BLE.Scanner.start", `BLE.Scanner.start({})`},
		{"BLE.GAP.parseName", `BLE.GAP.parseName(null)`},
		{"BLE.AdvBuilder.build", `BLE.AdvBuilder.build()`},
		{"BTHome.parseData", `BTHome.parseData(null)`},
		{"BTHome.DataBuilder.build", `BTHome.DataBuilder.build()`},
		{"UART.get", `UART.get(0)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := goja.New()
			installHardwareStubs(vm)

			// Caught from script-side try/catch: the script must observe the
			// error, not have the host process crash.
			result, err := vm.RunString(`
				(function() {
					try {
						` + tt.call + `;
						return "no error thrown";
					} catch (e) {
						return String(e);
					}
				})();
			`)
			if err != nil {
				t.Fatalf("RunString returned a Go error (exception escaped the VM): %v", err)
			}
			msg := result.String()
			if !strings.Contains(msg, "not implemented") {
				t.Errorf("%s: message = %q, want it to mention 'not implemented'", tt.name, msg)
			}
		})
	}
}
