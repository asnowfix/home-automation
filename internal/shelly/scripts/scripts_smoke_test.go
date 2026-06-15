package scripts

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// smokeTimeout is how long each script may run before the test cancels it.
// Reaching the deadline is healthy: the script was running and waiting for
// events. Only a JS-level exception or a non-context error causes failure.
const smokeTimeout = 5 * time.Second

// genericComponentStatus returns a minimal Shelly component-status map that is
// sufficient for most scripts to initialize without crashing. Scripts that need
// specific hardware state to init (e.g. a particular KVS key) degrade
// gracefully (log an error, return early from init) rather than throwing.
func genericComponentStatus() map[string]interface{} {
	return map[string]interface{}{
		"switch:0": map[string]interface{}{"id": 0, "output": false},
		"switch:1": map[string]interface{}{"id": 1, "output": false},
		"switch:2": map[string]interface{}{"id": 2, "output": false},
		"switch:3": map[string]interface{}{"id": 3, "output": false},
		"input:0":  map[string]interface{}{"id": 0, "state": false},
		"input:1":  map[string]interface{}{"id": 1, "state": false},
		"input:2":  map[string]interface{}{"id": 2, "state": false},
		"sys":      map[string]interface{}{"device_id": "shellytest-aabbccddeeff"},
		"mqtt":     map[string]interface{}{"connected": true},
		"wifi":     map[string]interface{}{"status": "got ip", "sta_ip": "192.168.1.200"},
		"eth":      map[string]interface{}{"status": "disconnected"},
	}
}

// TestSmokeAllScripts verifies that every embedded Shelly script:
//  1. Minifies without error — catching catch{} and other minify-safety bugs
//     (the Espruino constraint that `catch (e) {}` must never be written because
//     the minifier drops the `e` binding, producing invalid `catch{}` syntax).
//  2. Loads and initialises in the goja harness without a JS-level exception.
//
// The gate enumerates the embedded FS automatically: adding a new .js file
// means it must pass the gate without any list to maintain.
//
// If a script legitimately requires specific KVS or device state to init,
// add an override in perScriptState below. The default is an empty KVS with a
// generic component status — scripts must degrade gracefully when config is
// absent (log an error, return early) rather than throw.
//
// If a script relies on hardware-only APIs (e.g. BLE.Scanner) that cannot be
// stubbed in the goja harness, list it in minifyOnly. It will still be checked
// for minify-safety but will not be run through the harness.
func TestSmokeAllScripts(t *testing.T) {
	// Scripts that depend on hardware-only APIs unavailable in the goja harness.
	// These are minify-checked only (no goja run). Keep this list minimal.
	minifyOnly := map[string]string{
		// BLE.Scanner is a hardware-only Shelly API; no harness stub exists.
		"universal-blu-to-mqtt.js": "uses BLE.Scanner (hardware-only API)",
	}

	// Per-script DeviceState overrides. Keyed by filename (e.g. "pool-pump.js").
	// Leave empty to use the generic state below.
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

			// Step 1 — minify: validates no catch{} or other minify-safety violations.
			minified, err := script.Minify(rawBuf)
			if err != nil {
				t.Fatalf("minify failed — check catch blocks (must be `catch(e){if(e&&false){}}`, not `catch(e){}`): %v", err)
			}

			// Scripts that use hardware-only APIs (see minifyOnly above) are
			// checked for minify-safety only; the goja run is skipped.
			if reason, ok := minifyOnly[name]; ok {
				t.Logf("minify-only (skipping goja run): %s", reason)
				return
			}

			// Step 2 — run the minified source through goja.
			// Reaching the timeout is not a failure; it means the script was healthy
			// and waiting for events. Only genuine JS exceptions or non-context
			// errors cause the subtest to fail.
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

			runErr := script.RunWithDeviceState(ctx, name, minified, false, deviceState)
			if runErr != nil &&
				!errors.Is(runErr, context.Canceled) &&
				!errors.Is(runErr, context.DeadlineExceeded) {
				t.Fatalf("script failed to initialise: %v", runErr)
			}
		})
	}
}
