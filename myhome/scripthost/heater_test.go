package scripthost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// TestHeaterWorkflow runs the real heater-myhome.js on the script host with a
// fake device backend and exercises the heater.getconfig / heater.setconfig
// verbs end-to-end (async respond() completion across chained device calls).
// No t.Parallel(): registers handlers in the shared myhome methods map.
func TestHeaterWorkflow(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "shelly", "scripts", "heater-myhome.js"))
	if err != nil {
		t.Fatalf("read heater-myhome.js: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := testr.New(t)
	ctx = logr.NewContext(ctx, log)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	// Fake device manager lookup (normally registered by devices/impl)
	myhome.RegisterMethodHandler(myhome.DeviceLookup, func(ctx context.Context, in any) (any, error) {
		return []map[string]any{{"id": "shellyplus1-test", "name": "radiator-test"}}, nil
	})

	// Fake device RPC backend
	var mu sync.Mutex
	kvsSets := make(map[string]string)
	var uploads []string
	fakeDeviceCall := func(ctx context.Context, identifier, method string, params any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "Script.List":
			return map[string]any{"scripts": []any{
				map[string]any{"id": 1, "name": "heater.js", "running": true},
			}}, nil
		case "KVS.GetMany":
			return map[string]any{"items": map[string]any{
				// object form (etag+value) and plain form must both parse
				"script/heater/cheap-start-hour": map[string]any{"etag": "x", "value": "23"},
				"script/heater/enable-logging":   "true",
				"script/heater/preheat-hours":    map[string]any{"value": "3"},
			}}, nil
		case "KVS.Get":
			p := params.(map[string]any)
			switch p["key"] {
			case "room-id":
				return map[string]any{"value": "office"}, nil
			default:
				return nil, fmt.Errorf("key not found")
			}
		case "KVS.Set":
			p := params.(map[string]any)
			kvsSets[p["key"].(string)] = p["value"].(string)
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("unexpected device call %s", method)
	}
	fakeUpload := func(ctx context.Context, identifier, scriptName string) error {
		mu.Lock()
		defer mu.Unlock()
		uploads = append(uploads, identifier+":"+scriptName)
		return nil
	}

	svc := NewService(log, Config{
		Enabled:  true,
		Run:      []string{"heater-myhome"},
		StateDir: t.TempDir(),
	}, fstest.MapFS{"heater-myhome.js": &fstest.MapFile{Data: src}}, nil, "test-instance").
		WithDeviceCaller(fakeDeviceCall, fakeUpload)
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// heater.getconfig — retried until the script engine is up
	var out any
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err = myhome.CallLocalE(ctx, myhome.HeaterGetConfig, json.RawMessage(`{"identifier":"shellyplus1-test"}`))
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heater.getconfig never succeeded: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T (%v)", out, out)
	}
	if res["device_id"] != "shellyplus1-test" || res["device_name"] != "radiator-test" {
		t.Errorf("device identity = %v / %v", res["device_id"], res["device_name"])
	}
	if res["has_script"] != true {
		t.Fatalf("has_script = %v, want true", res["has_script"])
	}
	config, ok := res["config"].(map[string]any)
	if !ok {
		t.Fatalf("missing config in %v", res)
	}
	if config["cheap_start_hour"] != int64(23) && config["cheap_start_hour"] != 23.0 {
		t.Errorf("cheap_start_hour = %v (%T), want 23", config["cheap_start_hour"], config["cheap_start_hour"])
	}
	if config["enable_logging"] != true {
		t.Errorf("enable_logging = %v, want true", config["enable_logging"])
	}
	if config["preheat_hours"] != int64(3) && config["preheat_hours"] != 3.0 {
		t.Errorf("preheat_hours = %v, want 3", config["preheat_hours"])
	}
	if config["room_id"] != "office" {
		t.Errorf("room_id = %v, want office", config["room_id"])
	}
	if config["normally_closed"] != false {
		t.Errorf("normally_closed = %v, want false", config["normally_closed"])
	}

	// heater.setconfig — sequential KVS.Set chain + script upload
	out, err = myhome.CallLocalE(ctx, myhome.HeaterSetConfig,
		json.RawMessage(`{"identifier":"shellyplus1-test","cheap_start_hour":22,"enable_logging":true,"room_id":"salon"}`))
	if err != nil {
		t.Fatalf("heater.setconfig: %v", err)
	}
	res, ok = out.(map[string]any)
	if !ok || res["success"] != true {
		t.Fatalf("setconfig result = %v (%T), want success", out, out)
	}
	mu.Lock()
	defer mu.Unlock()
	if kvsSets["script/heater/cheap-start-hour"] != "22" {
		t.Errorf("cheap-start-hour set to %q, want 22", kvsSets["script/heater/cheap-start-hour"])
	}
	if kvsSets["script/heater/enable-logging"] != "true" {
		t.Errorf("enable-logging set to %q, want true", kvsSets["script/heater/enable-logging"])
	}
	if kvsSets["room-id"] != "salon" {
		t.Errorf("room-id set to %q, want salon", kvsSets["room-id"])
	}
	if len(uploads) != 1 || uploads[0] != "shellyplus1-test:heater.js" {
		t.Errorf("uploads = %v, want [shellyplus1-test:heater.js]", uploads)
	}
}
