package ui

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

// eventsFakeRegistry is a DeviceRegistry over a fixed set of devices, matched by id,
// name or MAC like the real one.
type eventsFakeRegistry struct{ devs []*myhome.Device }

func (f *eventsFakeRegistry) GetAllDevices(context.Context) ([]*myhome.Device, error) { return f.devs, nil }

func (f *eventsFakeRegistry) GetDeviceById(ctx context.Context, id string) (*myhome.Device, error) {
	return f.GetDeviceByAny(ctx, id)
}

func (f *eventsFakeRegistry) GetDeviceByAny(_ context.Context, id string) (*myhome.Device, error) {
	for _, d := range f.devs {
		if d.Id() == id || d.Name() == id || (d.Mac() != nil && d.Mac().String() == id) {
			return d, nil
		}
	}
	return nil, errors.New("not found")
}

func eventsTestRegistry() *eventsFakeRegistry {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	named := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyplus1-aabbccddee01").WithName("kitchen").WithMAC(mac)
	unnamed := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyplus1-000000000002").WithName("shellyplus1-000000000002")
	return &eventsFakeRegistry{devs: []*myhome.Device{named, unnamed}}
}

func TestFormatMinutes(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{{0, "00:00"}, {59, "00:59"}, {60, "01:00"}, {18*60 + 5, "18:05"}, {23*60 + 59, "23:59"}}
	for _, tt := range tests {
		if got := formatMinutes(tt.in); got != tt.want {
			t.Errorf("formatMinutes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHTMX_DeviceName(t *testing.T) {
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard(), db: eventsTestRegistry()}
	tests := []struct {
		id, want string
	}{
		{"shellyplus1-aabbccddee01", "kitchen"},
		{"shellyplus1-000000000002", ""}, // the name equals the id: caller shows the raw id
		{"unknown-device", ""},
	}
	for _, tt := range tests {
		if got := h.deviceName(tt.id); got != tt.want {
			t.Errorf("deviceName(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
	if got := h.DeviceNameResolver()("shellyplus1-aabbccddee01"); got != "kitchen" {
		t.Errorf("DeviceNameResolver = %q", got)
	}
}

func TestHTMX_ResolveDeviceFilter(t *testing.T) {
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard(), db: eventsTestRegistry()}
	tests := []struct {
		name, filter string
		want         []string
	}{
		{"empty filter means no restriction", "", nil},
		{"by name: id and MAC both", "kitchen", []string{"shellyplus1-aabbccddee01", "aa:bb:cc:dd:ee:01"}},
		{"device without MAC: id only", "shellyplus1-000000000002", []string{"shellyplus1-000000000002"}},
		{"unknown falls back to the raw filter", "nope", []string{"nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.resolveDeviceFilter(tt.filter); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveDeviceFilter(%q) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

func TestToEventRows_AndNameMap(t *testing.T) {
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard(), db: eventsTestRegistry()}
	evts := []events.Event{
		{DeviceID: "shellyplus1-aabbccddee01", Event: "switch.on", Severity: "info", Ts: 1},
		{DeviceID: "shellyplus1-aabbccddee01", Event: "switch.off", Severity: "info", Ts: 2},
		{DeviceID: "other", Event: "x", Severity: "warn", Ts: 3},
	}
	names := h.deviceNameMap(evts)
	if len(names) != 1 || names["shellyplus1-aabbccddee01"] != "kitchen" {
		t.Fatalf("deviceNameMap = %v", names)
	}
	rows := toEventRows(evts, names)
	if len(rows) != 3 || rows[0].DeviceName != "kitchen" || rows[2].DeviceName != "" || rows[2].Event != "x" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestHTMX_BuildQuery(t *testing.T) {
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard(), db: eventsTestRegistry()}
	r := httptest.NewRequest(http.MethodGet, "/events?device=kitchen&type=switch.on&severity=warn", nil)
	q := h.buildQuery(r, 100)
	if q.EventType != "switch.on" || q.Severity != "warn" || q.Offset != 100 || q.Limit != 50 || q.Since != 24*time.Hour {
		t.Errorf("query = %+v", q)
	}
	if len(q.DeviceIDs) != 2 {
		t.Errorf("DeviceIDs = %v, want id and MAC", q.DeviceIDs)
	}
}

func eventsHandler(t *testing.T, withService bool) *HTMXHandler {
	t.Helper()
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard(), db: eventsTestRegistry()}
	if !withService {
		return h
	}
	store, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(store.Close)
	h.eventsSvc = events.NewService(logr.Discard(), store, nil, nil, 0)
	now := float64(time.Now().Unix())
	for i, name := range []string{"switch.on", "switch.off"} {
		if err := h.eventsSvc.Record(context.Background(), events.Event{
			Ts: now - float64(i), DeviceID: "shellyplus1-aabbccddee01", Component: "switch:0", Event: name, Severity: "info",
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	return h
}

func TestHTMX_EventsTable(t *testing.T) {
	t.Run("without an events service", func(t *testing.T) {
		w := httptest.NewRecorder()
		eventsHandler(t, false).EventsTable(w, httptest.NewRequest(http.MethodGet, "/events", nil))
		if !strings.Contains(w.Body.String(), "Event service not available") {
			t.Errorf("body = %q", w.Body.String())
		}
	})
	t.Run("renders recorded events with the device's friendly name", func(t *testing.T) {
		w := httptest.NewRecorder()
		eventsHandler(t, true).EventsTable(w, httptest.NewRequest(http.MethodGet, "/events", nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, "switch.on") || !strings.Contains(body, "switch.off") || !strings.Contains(body, "kitchen") {
			t.Errorf("status %d, body = %q", w.Code, body)
		}
	})
	t.Run("a filter excludes other event types", func(t *testing.T) {
		w := httptest.NewRecorder()
		eventsHandler(t, true).EventsTable(w, httptest.NewRequest(http.MethodGet, "/events?type=switch.off", nil))
		body := w.Body.String()
		if !strings.Contains(body, "switch.off") || strings.Contains(body, "switch.on") {
			t.Errorf("body = %q", body)
		}
	})
}

func TestHTMX_EventsMore(t *testing.T) {
	t.Run("without an events service nothing is rendered", func(t *testing.T) {
		w := httptest.NewRecorder()
		eventsHandler(t, false).EventsMore(w, httptest.NewRequest(http.MethodGet, "/events/more?offset=50", nil))
		if w.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", w.Body.String())
		}
	})
	t.Run("offset skips the first rows", func(t *testing.T) {
		w := httptest.NewRecorder()
		eventsHandler(t, true).EventsMore(w, httptest.NewRequest(http.MethodGet, "/events/more?offset=1", nil))
		body := w.Body.String()
		if strings.Contains(body, "switch.on") || !strings.Contains(body, "switch.off") {
			t.Errorf("body = %q, want only the second (older) event", body)
		}
	})
	t.Run("a bad offset is treated as 0", func(t *testing.T) {
		w := httptest.NewRecorder()
		eventsHandler(t, true).EventsMore(w, httptest.NewRequest(http.MethodGet, "/events/more?offset=zzz", nil))
		if !strings.Contains(w.Body.String(), "switch.on") {
			t.Errorf("body = %q", w.Body.String())
		}
	})
}
