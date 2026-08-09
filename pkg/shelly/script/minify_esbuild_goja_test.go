package script

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// TestEsbuildOutput_RunsInGojaHarness verifies the goja emulator harness
// (Run/RunWithDeviceState) accepts and executes a script minified by the
// new esbuild path -- both without and with top-level mangling -- just as
// it already does for tdewolff output (see
// internal/shelly/scripts.TestSmokeAllScripts). Reaching the timeout is the
// healthy outcome: it means the script initialised without a JS-level
// exception and was left running/waiting.
func TestEsbuildOutput_RunsInGojaHarness(t *testing.T) {
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	cases := []struct {
		name string
		opts MinifyOptions
	}{
		{"no top-level mangling", MinifyOptions{Engine: EngineEsbuild}},
		{"top-level mangling", MinifyOptions{
			Engine:          EngineEsbuild,
			MangleTopLevel:  true,
			PreserveGlobals: []string{"handleMorningStart", "handleEveningStop"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := MinifyWithOptions("test.js", []byte(esbuildTestScript), tc.opts)
			if err != nil {
				t.Fatalf("MinifyWithOptions: %v", err)
			}

			ctx, cancel := context.WithTimeout(
				logr.NewContext(context.Background(), testr.New(t)),
				2*time.Second,
			)
			defer cancel()

			deviceState := &DeviceState{
				KVS:     make(map[string]interface{}),
				Storage: make(map[string]interface{}),
			}
			runErr := RunWithDeviceState(ctx, "test.js", res.Code, false, deviceState)
			if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
				t.Fatalf("script failed to run under goja: %v\ncode:\n%s", runErr, res.Code)
			}
		})
	}
}
