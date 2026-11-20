package blu

import (
	"context"
	"encoding/json"
	"io/fs"
	"testing"
	"time"

	scripts "github.com/asnowfix/home-automation/internal/shelly/scripts"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

const bluPublisherScriptPath = "blu-publisher.js"

// runBluPublisher runs blu-publisher.js against a "ble" component config and
// reports how many times the script started the BLE scanner.
func runBluPublisher(t *testing.T, bleConfig map[string]interface{}) int {
	t.Helper()
	buf, err := fs.ReadFile(scripts.GetFS(), bluPublisherScriptPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", bluPublisherScriptPath, err)
	}
	state := &script.DeviceState{
		KVS:             map[string]interface{}{},
		Storage:         make(map[string]interface{}),
		ComponentConfig: map[string]interface{}{"ble": bleConfig},
	}

	ctx, cancel := context.WithTimeout(
		mqtt.NewContextWithClient(logr.NewContext(context.Background(), testr.New(t)), mqtt.NewMockClient()),
		5*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, bluPublisherScriptPath, buf, false, state)
	}()

	// Init is synchronous apart from the KVS.List chain; give it time to settle
	// so "never started" is distinguished from "not started yet".
	waitFor(2*time.Second, 50*time.Millisecond, func() bool { return state.BLEScannerStarts() > 0 })
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	return state.BLEScannerStarts()
}

// TestBluPublisher_StartsScannerWhenBLEConfigHasNoEnableKey is the regression
// test for firmware >= 2.0.0, which removed ble.config.enable (scanning is
// automatic): the script used to read the missing key as "Bluetooth disabled"
// and exit at init without starting the scanner. This is the config measured
// on volets-gym-1/-2 (S2PMG3, fw 2.1.0) on 2026-10-20.
func TestBluPublisher_StartsScannerWhenBLEConfigHasNoEnableKey(t *testing.T) {
	cfg := map[string]interface{}{"rpc": map[string]interface{}{"enable": false}}
	if got := runBluPublisher(t, cfg); got != 1 {
		t.Fatalf("BLE.Scanner.Start called %d times, want 1 (config without \"enable\" must not mean disabled)", got)
	}
}

// TestBluPublisher_StartsScannerWhenBLEEnabled covers firmware < 2.0.0 with
// Bluetooth switched on: behaviour must be unchanged.
func TestBluPublisher_StartsScannerWhenBLEEnabled(t *testing.T) {
	cfg := map[string]interface{}{"enable": true, "rpc": map[string]interface{}{"enable": false}}
	if got := runBluPublisher(t, cfg); got != 1 {
		t.Fatalf("BLE.Scanner.Start called %d times, want 1", got)
	}
}

// TestBluPublisher_PayloadCarriesRelayID (#850): every published payload
// names the publishing device ("relay"), read once at script start from
// Shelly.getDeviceInfo().id, so the daemon can tell which relays heard one
// advertisement. The emulator's device id is a fixed synthetic one.
func TestBluPublisher_PayloadCarriesRelayID(t *testing.T) {
	const mac = "aa:bb:cc:00:11:22"
	const wantRelay = "shellyplus1-b8d61a85a970" // emulator's Shelly.getDeviceInfo().id
	buf, err := fs.ReadFile(scripts.GetFS(), bluPublisherScriptPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", bluPublisherScriptPath, err)
	}
	state := &script.DeviceState{
		KVS:                  map[string]interface{}{},
		Storage:              make(map[string]interface{}),
		ComponentConfig:      map[string]interface{}{"ble": map[string]interface{}{"enable": true}},
		ScheduleEvalInjector: make(chan []byte, 4),
	}
	mc := mqtt.NewMockClient()
	ctx, cancel := context.WithTimeout(
		mqtt.NewContextWithClient(logr.NewContext(context.Background(), testr.New(t)), mc),
		10*time.Second,
	)
	defer cancel()

	published, err := mc.Subscribe(ctx, "shelly-blu/events/"+mac, 4, "test")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, bluPublisherScriptPath, buf, false, state)
	}()
	waitFor(2*time.Second, 50*time.Millisecond, func() bool { return state.BLEScannerStarts() > 0 })

	req, _ := json.Marshal(map[string]string{
		"code": `emitData({address:"` + mac + `",rssi:-70,pid:3,motion:1})`,
	})
	state.ScheduleEvalInjector <- req

	select {
	case msg := <-published:
		var got map[string]interface{}
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("payload %q is not JSON: %v", msg, err)
		}
		if got["relay"] != wantRelay {
			t.Errorf("relay = %v, want %q (payload %s)", got["relay"], wantRelay, msg)
		}
		if got["address"] != mac || got["pid"] != float64(3) {
			t.Errorf("existing payload keys changed: %s", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no MQTT message published")
	}
	cancel()
	<-done
}

// TestBluPublisher_DoesNotStartScannerWhenBLEExplicitlyDisabled covers
// firmware < 2.0.0 with Bluetooth switched off: an explicit enable:false must
// still keep the scanner off.
func TestBluPublisher_DoesNotStartScannerWhenBLEExplicitlyDisabled(t *testing.T) {
	cfg := map[string]interface{}{"enable": false, "rpc": map[string]interface{}{"enable": false}}
	if got := runBluPublisher(t, cfg); got != 0 {
		t.Fatalf("BLE.Scanner.Start called %d times, want 0 (explicit enable:false must disable)", got)
	}
}
