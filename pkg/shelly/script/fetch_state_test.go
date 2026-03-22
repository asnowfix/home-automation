// fetch_state_test.go — tests for FetchDeviceState and the ComponentStatus
// round-trip introduced by changing json:"-" to json:"component_status,omitempty".
//
// Approach A — unit tests: fakeDevice returns canned RPC responses; results are
// asserted directly.
//
// Approach B — golden file: a fixed fixture is run through FetchDeviceState and
// the marshaled output is compared to testdata/fetch_state.golden.json.
// Regenerate the golden file with:
//
//	go test -run TestFetchDeviceState_Golden -update
package script

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
)

var update = flag.Bool("update", false, "write golden files instead of comparing them")

// ---------------------------------------------------------------------------
// fakeDevice — minimal types.Device for unit tests.
// callFn handles all RPC dispatching; it can inspect method and params to
// return key-specific responses (important for KVS.Get which is called once
// per key with the key name in params).
// ---------------------------------------------------------------------------

// Compile-time check that fakeDevice satisfies the interface.
var _ types.Device = (*fakeDevice)(nil)

type fakeDevice struct {
	callFn func(method string, params any) (any, error)
}

func (f *fakeDevice) CallE(_ context.Context, _ types.Channel, method string, params any) (any, error) {
	return f.callFn(method, params)
}

func (f *fakeDevice) String() string                            { return "fake" }
func (f *fakeDevice) Name() string                             { return "fake" }
func (f *fakeDevice) Host() string                             { return "fake" }
func (f *fakeDevice) Manufacturer() string                     { return "fake" }
func (f *fakeDevice) Id() string                               { return "fake" }
func (f *fakeDevice) Mac() net.HardwareAddr                    { return nil }
func (f *fakeDevice) ReplyTo() string                          { return "" }
func (f *fakeDevice) To() chan<- []byte                        { return nil }
func (f *fakeDevice) From() <-chan []byte                      { return nil }
func (f *fakeDevice) StartDialog(_ context.Context) uint32     { return 0 }
func (f *fakeDevice) StopDialog(_ context.Context, _ uint32)  {}
func (f *fakeDevice) IsHttpReady() bool                        { return false }
func (f *fakeDevice) IsMqttReady() bool                        { return true }
func (f *fakeDevice) Channel(via types.Channel) types.Channel  { return via }
func (f *fakeDevice) UpdateName(_ string)                      {}
func (f *fakeDevice) UpdateHost(_ string)                      {}
func (f *fakeDevice) ClearHost()                               {}
func (f *fakeDevice) UpdateMac(_ string)                       {}
func (f *fakeDevice) UpdateId(_ string)                        {}
func (f *fakeDevice) IsModified() bool                         { return false }
func (f *fakeDevice) ResetModified()                           {}

// kvsListResp builds a KVS.List response from a store map.
func kvsListResp(store map[string]string) *kvs.ListResponse {
	keys := make(map[string]kvs.Status, len(store))
	for k := range store {
		keys[k] = kvs.Status{}
	}
	return &kvs.ListResponse{Keys: keys}
}

// makeCallFn builds a callFn that serves KVS.List, KVS.Get, and
// Shelly.GetStatus from the provided stores, failing on anything else.
func makeCallFn(store map[string]string, status any) func(string, any) (any, error) {
	return func(method string, params any) (any, error) {
		switch method {
		case "KVS.List":
			return kvsListResp(store), nil
		case "KVS.Get":
			req := params.(*kvs.GetRequest)
			v, ok := store[req.Key]
			if !ok {
				return nil, fmt.Errorf("fakeDevice: unknown key %q", req.Key)
			}
			return &kvs.GetResponse{Value: v}, nil
		case "Shelly.GetStatus":
			return status, nil
		default:
			return nil, fmt.Errorf("fakeDevice: unexpected method %q", method)
		}
	}
}

// ---------------------------------------------------------------------------
// Approach A — unit tests
// ---------------------------------------------------------------------------

// TestFetchDeviceState_HappyPath verifies KVS and ComponentStatus are
// populated, and Storage is always empty.
func TestFetchDeviceState_HappyPath(t *testing.T) {
	store := map[string]string{
		"script/pool-pump/active-output": "-1",
		"script/pool-pump/device-role":   "controller",
	}
	d := &fakeDevice{callFn: makeCallFn(store, map[string]interface{}{
		"switch:0": map[string]interface{}{"id": 0, "output": false},
		"sys":      map[string]interface{}{"device_id": "test-device"},
	})}

	state, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.KVS) != 2 {
		t.Errorf("KVS: got %d keys, want 2", len(state.KVS))
	}
	if state.KVS["script/pool-pump/active-output"] != "-1" {
		t.Errorf("KVS active-output: got %v, want -1", state.KVS["script/pool-pump/active-output"])
	}
	if state.KVS["script/pool-pump/device-role"] != "controller" {
		t.Errorf("KVS device-role: got %v, want controller", state.KVS["script/pool-pump/device-role"])
	}
	if len(state.ComponentStatus) != 2 {
		t.Errorf("ComponentStatus: got %d keys, want 2", len(state.ComponentStatus))
	}
	if len(state.Storage) != 0 {
		t.Errorf("Storage: got %d keys, want 0 (Script.storage is not fetchable via RPC)", len(state.Storage))
	}
}

// TestFetchDeviceState_Empty verifies that a device with no KVS entries and
// a nil status response does not produce an error.
func TestFetchDeviceState_Empty(t *testing.T) {
	d := &fakeDevice{callFn: makeCallFn(map[string]string{}, nil)}

	state, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.KVS) != 0 {
		t.Errorf("KVS: got %d keys, want 0", len(state.KVS))
	}
	if len(state.ComponentStatus) != 0 {
		t.Errorf("ComponentStatus: got %d keys, want 0", len(state.ComponentStatus))
	}
}

// TestFetchDeviceState_MultipleKVSKeys verifies that all keys returned by
// KVS.List are fetched individually and merged into the result.
func TestFetchDeviceState_MultipleKVSKeys(t *testing.T) {
	store := map[string]string{
		"key-a": "value-a",
		"key-b": "value-b",
		"key-c": "value-c",
	}
	d := &fakeDevice{callFn: makeCallFn(store, nil)}

	state, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.KVS) != 3 {
		t.Errorf("KVS: got %d keys, want 3", len(state.KVS))
	}
	for k, want := range store {
		if state.KVS[k] != want {
			t.Errorf("KVS[%q]: got %v, want %v", k, state.KVS[k], want)
		}
	}
}

// TestFetchDeviceState_KVSListError verifies that a KVS.List failure is
// propagated with a meaningful error message.
func TestFetchDeviceState_KVSListError(t *testing.T) {
	d := &fakeDevice{callFn: func(method string, _ any) (any, error) {
		if method == "KVS.List" {
			return nil, fmt.Errorf("connection refused")
		}
		return nil, fmt.Errorf("unexpected: %s", method)
	}}

	_, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetching KVS") {
		t.Errorf("error %q should mention 'fetching KVS'", err)
	}
}

// TestFetchDeviceState_KVSGetError verifies that a per-key KVS.Get failure is
// propagated with the key name in the error message.
func TestFetchDeviceState_KVSGetError(t *testing.T) {
	d := &fakeDevice{callFn: func(method string, params any) (any, error) {
		switch method {
		case "KVS.List":
			return &kvs.ListResponse{Keys: map[string]kvs.Status{"my-key": {}}}, nil
		case "KVS.Get":
			return nil, fmt.Errorf("timeout")
		default:
			return nil, fmt.Errorf("unexpected: %s", method)
		}
	}}

	_, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "KVS.Get") {
		t.Errorf("error %q should mention 'KVS.Get'", err)
	}
}

// TestFetchDeviceState_StatusError verifies that a Shelly.GetStatus failure is
// propagated with a meaningful error message.
func TestFetchDeviceState_StatusError(t *testing.T) {
	d := &fakeDevice{callFn: func(method string, _ any) (any, error) {
		switch method {
		case "KVS.List":
			return &kvs.ListResponse{Keys: map[string]kvs.Status{}}, nil
		case "Shelly.GetStatus":
			return nil, fmt.Errorf("device unavailable")
		default:
			return nil, fmt.Errorf("unexpected: %s", method)
		}
	}}

	_, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetching component status") {
		t.Errorf("error %q should mention 'fetching component status'", err)
	}
}

// ---------------------------------------------------------------------------
// Approach B — golden file test
// ---------------------------------------------------------------------------

// TestFetchDeviceState_Golden runs FetchDeviceState against a fixed fixture and
// compares the marshaled DeviceState to testdata/fetch_state.golden.json.
//
// To regenerate the golden file after an intentional output change:
//
//	go test -run TestFetchDeviceState_Golden -update
func TestFetchDeviceState_Golden(t *testing.T) {
	store := map[string]string{
		"script/pool-pump/active-output": "-1",
		"script/pool-pump/device-role":   "controller",
	}
	d := &fakeDevice{callFn: makeCallFn(store, map[string]interface{}{
		"switch:0": map[string]interface{}{"id": 0, "output": false},
		"sys":      map[string]interface{}{"device_id": "test-device"},
	})}

	state, err := FetchDeviceState(context.Background(), types.ChannelDefault, d)
	if err != nil {
		t.Fatalf("FetchDeviceState: %v", err)
	}
	got, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent: %v", err)
	}

	goldenPath := filepath.Join("testdata", "fetch_state.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden file updated: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v\n(run with -update to create it)", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output mismatch with %s\n\ngot:\n%s\n\nwant:\n%s", goldenPath, got, want)
	}
}

// ---------------------------------------------------------------------------
// Regression guard for json:"-" → json:"component_status,omitempty" change
// ---------------------------------------------------------------------------

// TestDeviceState_ComponentStatusRoundTrip verifies that ComponentStatus
// survives a SaveDeviceState / LoadDeviceState cycle. This is the primary
// regression guard for the json tag change on that field.
func TestDeviceState_ComponentStatusRoundTrip(t *testing.T) {
	original := &DeviceState{
		KVS:     map[string]interface{}{"kvs-key": "kvs-val"},
		Storage: map[string]interface{}{"storage-key": "storage-val"},
		ComponentStatus: map[string]interface{}{
			"switch:0": map[string]interface{}{"id": 0, "output": true},
			"sys":      map[string]interface{}{"device_id": "my-device"},
		},
	}

	tmp := filepath.Join(t.TempDir(), "state.json")
	log := logr.Discard()

	if err := SaveDeviceState(log, tmp, original); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	// Confirm the JSON file contains the component_status key.
	raw, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), `"component_status"`) {
		t.Errorf("saved JSON does not contain 'component_status' key:\n%s", raw)
	}

	loaded, err := LoadDeviceState(log, tmp)
	if err != nil {
		t.Fatalf("LoadDeviceState: %v", err)
	}

	if len(loaded.ComponentStatus) != 2 {
		t.Errorf("ComponentStatus: got %d entries, want 2", len(loaded.ComponentStatus))
	}
	sw0, ok := loaded.ComponentStatus["switch:0"].(map[string]interface{})
	if !ok {
		t.Fatalf("ComponentStatus[switch:0]: expected map[string]interface{}, got %T",
			loaded.ComponentStatus["switch:0"])
	}
	// JSON unmarshal converts numbers to float64; bool stays bool.
	if sw0["output"] != true {
		t.Errorf("switch:0 output: got %v, want true", sw0["output"])
	}

	// Also verify KVS and Storage round-tripped correctly.
	if loaded.KVS["kvs-key"] != "kvs-val" {
		t.Errorf("KVS[kvs-key]: got %v, want kvs-val", loaded.KVS["kvs-key"])
	}
	if loaded.Storage["storage-key"] != "storage-val" {
		t.Errorf("Storage[storage-key]: got %v, want storage-val", loaded.Storage["storage-key"])
	}
}
