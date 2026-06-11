package scripthost

import (
	"context"
	"encoding/json"
	"testing"
	"testing/fstest"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// newTestService starts a script host with the given embedded scripts.
// Tests must NOT call t.Parallel(): the host registers RPC method handlers in
// the shared package-level myhome methods map.
func newTestService(t *testing.T, scripts fstest.MapFS, run []string) (context.Context, *Service) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := testr.New(t)
	ctx = logr.NewContext(ctx, log)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	svc := NewService(log, Config{
		Enabled:  true,
		Run:      run,
		StateDir: t.TempDir(),
	}, scripts, nil, "test-instance")

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return ctx, svc
}

// invokeEventually retries script.invoke until the script engine is up.
func invokeEventually(t *testing.T, ctx context.Context, svc *Service, script, name string, params any) *myhome.ScriptInvokeResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		res, err := svc.Invoke(ctx, script, name, params)
		if err == nil {
			return res
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Invoke(%s, %s) never succeeded: %v", script, name, lastErr)
	return nil
}

func TestInvokeHandler(t *testing.T) {
	scripts := fstest.MapFS{
		"hello.js": &fstest.MapFile{Data: []byte(`
			var count = 0;
			MyHome.on("ping", function(params) {
				count++;
				return { pong: params.value, count: count, instance: MyHome.instance() };
			});
		`)},
	}
	ctx, svc := newTestService(t, scripts, []string{"hello"})

	res := invokeEventually(t, ctx, svc, "hello", "ping", map[string]any{"value": 42.0})
	m, ok := res.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T (%v)", res.Result, res.Result)
	}
	if m["pong"] != int64(42) && m["pong"] != 42.0 {
		t.Errorf("pong = %v (%T), want 42", m["pong"], m["pong"])
	}
	if m["instance"] != "test-instance" {
		t.Errorf("instance = %v, want test-instance", m["instance"])
	}

	// Second invocation: same VM, state preserved
	res = invokeEventually(t, ctx, svc, "hello", "ping", map[string]any{"value": 1.0})
	m = res.Result.(map[string]any)
	if m["count"] != int64(2) && m["count"] != 2.0 {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestInvokeViaRPCDispatch(t *testing.T) {
	scripts := fstest.MapFS{
		"echo.js": &fstest.MapFile{Data: []byte(`
			MyHome.on("echo", function(params) { return params; });
		`)},
	}
	ctx, svc := newTestService(t, scripts, []string{"echo.js"})

	// Wait for the engine, then go through the same path as a device request:
	// CallLocalE decodes raw JSON per the script.invoke signature.
	invokeEventually(t, ctx, svc, "echo", "echo", nil)

	raw := json.RawMessage(`{"script":"echo","name":"echo","params":{"a":"b"}}`)
	out, err := myhome.CallLocalE(ctx, myhome.ScriptInvoke, raw)
	if err != nil {
		t.Fatalf("CallLocalE: %v", err)
	}
	res, ok := out.(*myhome.ScriptInvokeResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", out)
	}
	m, ok := res.Result.(map[string]any)
	if !ok || m["a"] != "b" {
		t.Fatalf("echo result = %v, want {a:b}", res.Result)
	}
}

func TestRegisterVerb(t *testing.T) {
	scripts := fstest.MapFS{
		"verbs.js": &fstest.MapFile{Data: []byte(`
			MyHome.registerVerb("heater.getconfig", function(params) {
				return { device_id: params.identifier, device_name: "js", has_script: false };
			});
			MyHome.on("ready", function() { return true; });
		`)},
	}
	ctx, svc := newTestService(t, scripts, []string{"verbs"})
	invokeEventually(t, ctx, svc, "verbs", "ready", nil)

	out, err := myhome.CallLocalE(ctx, myhome.HeaterGetConfig, json.RawMessage(`{"identifier":"dev1"}`))
	if err != nil {
		t.Fatalf("CallLocalE(heater.getconfig): %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T (%v)", out, out)
	}
	if m["device_id"] != "dev1" || m["device_name"] != "js" {
		t.Fatalf("unexpected result: %v", m)
	}
}

func TestInvokeUnknownScript(t *testing.T) {
	ctx, svc := newTestService(t, fstest.MapFS{}, nil)
	if _, err := svc.Invoke(ctx, "ghost", "x", nil); err == nil {
		t.Fatal("expected error for unknown script")
	}
}
