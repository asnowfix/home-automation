package gen2

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

func newTestEventsService(t *testing.T) *events.Service {
	t.Helper()
	store, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("events.NewStorage: %v", err)
	}
	t.Cleanup(store.Close)
	return events.NewService(logr.Discard(), store, nil, nil, 0)
}

// buildShellyBLUNotifyEvent builds a realistic +/events/rpc payload for a
// shelly-blu event relayed by a Gen2 device running the BLU bridge script.
func buildShellyBLUNotifyEvent(t *testing.T, relaySrc, bluAddress string, ts float64) []byte {
	t.Helper()
	inner := map[string]any{
		"BTHome_version": 2,
		"address":        bluAddress,
		"motion":         1,
		"rssi":           -68,
		"battery":        90,
		"pid":            5,
		"encryption":     false,
	}
	payload := map[string]any{
		"src":    relaySrc,
		"method": "NotifyEvent",
		"params": map[string]any{
			"ts": ts,
			"events": []any{
				map[string]any{
					"component": "script:3",
					"id":        3,
					"event":     "shelly-blu",
					"ts":        0, // per-event ts absent; falls back to params.ts
					"data":      inner,
				},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// TestHandleNotifyEvent_ShellyBLU verifies that a relayed shelly-blu event is
// stored under the BLU sensor's address (not the relay device's id), and that
// the relay device id is preserved in the Data field.
func TestHandleNotifyEvent_ShellyBLU(t *testing.T) {
	const relaySrc = "shelly1minig3-543204641d24"
	const bluMAC = "e8:e0:7e:d0:f9:89"
	const frameTs = 1.781600000e+09 // plausible recent unix ts

	svc := newTestEventsService(t)
	l := NewListener(logr.Discard(), nil, svc, nil)
	payload := buildShellyBLUNotifyEvent(t, relaySrc, bluMAC, frameTs)

	if err := l.handleNotifyEvent(context.Background(), payload); err != nil {
		t.Fatalf("handleNotifyEvent: %v", err)
	}

	evts, err := svc.Store().Query(context.Background(), events.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	e := evts[0]

	// DeviceID must be the BLU sensor, not the relay Shelly
	if e.DeviceID != bluMAC {
		t.Errorf("DeviceID=%q, want BLU MAC %q", e.DeviceID, bluMAC)
	}
	if e.DeviceID == relaySrc {
		t.Errorf("DeviceID must not be the relay device %q", relaySrc)
	}

	// Data must be populated and contain the relay device id
	if e.Data == nil {
		t.Fatal("Data must not be nil")
	}
	if !strings.Contains(*e.Data, relaySrc) {
		t.Errorf("Data %q should contain relay device id %q", *e.Data, relaySrc)
	}
}
