package storage

import (
	"context"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/go-logr/logr/testr"
)

// newTestStorage opens a fresh in-memory SQLite database for each test.
// t.Cleanup ensures the connection is closed when the test ends.
func newTestStorage(t *testing.T) *DeviceStorage {
	t.Helper()
	s, err := NewDeviceStorage(testr.New(t), ":memory:")
	if err != nil {
		t.Fatalf("NewDeviceStorage: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// makeDevice constructs a minimal *myhome.Device for use in tests.
// Info and Config are intentionally left nil — the storage layer handles nil
// by marshalling them as JSON "null" and unmarshalling back to nil.
func makeDevice(manufacturer, id, mac, name, host string) *myhome.Device {
	return &myhome.Device{
		DeviceSummary: myhome.DeviceSummary{
			DeviceIdentifier: myhome.DeviceIdentifier{
				Manufacturer_: manufacturer,
				Id_:           id,
			},
			MAC:   mac,
			Name_: name,
			Host_: host,
		},
	}
}

// ── schema ────────────────────────────────────────────────────────────────────

// TestNewDeviceStorage_CreatesSchema verifies that the devices table exists
// after NewDeviceStorage returns.
func TestNewDeviceStorage_CreatesSchema(t *testing.T) {
	s := newTestStorage(t)
	var count int
	if err := s.db.Get(&count, "SELECT COUNT(*) FROM devices"); err != nil {
		t.Fatalf("devices table missing after schema creation: %v", err)
	}
}

// ── SetDevice ─────────────────────────────────────────────────────────────────

// TestSetDevice_InsertReportsChange confirms that the first insert of a device
// returns changed=true.
func TestSetDevice_InsertReportsChange(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-001", "aa:bb:cc:dd:ee:01", "my-light", "192.168.1.101")
	changed, err := s.SetDevice(ctx, d, false)
	if err != nil {
		t.Fatalf("SetDevice: %v", err)
	}
	if !changed {
		t.Error("expected true on first insert; got false")
	}
}

// TestSetDevice_SameDataNoChange confirms that upserting an identical device
// returns changed=false (the WHERE clause in the ON CONFLICT block filters it).
// A MAC-less device is used deliberately: when a MAC is present SetDevice runs
// a secondary unconditional UPDATE by MAC which always reports RowsAffected=1,
// masking the no-change detection.  That secondary path is a known limitation
// and is tested separately in TestSetDevice_MACPathAlwaysReportsChange.
func TestSetDevice_SameDataNoChange(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-002", "", "my-plug", "192.168.1.102")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("first SetDevice: %v", err)
	}

	changed, err := s.SetDevice(ctx, d, true)
	if err != nil {
		t.Fatalf("second SetDevice: %v", err)
	}
	if changed {
		t.Error("expected false when upserting identical data; got true")
	}
}

// TestSetDevice_MACPathAlwaysReportsChange documents the current behaviour: when
// a device has a MAC the secondary UPDATE-by-MAC path fires even for identical
// data, so changed=true is always returned.  This is a known limitation tracked
// in the project issue tracker.
func TestSetDevice_MACPathAlwaysReportsChange(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-002b", "aa:bb:cc:dd:ee:02", "my-plug-mac", "192.168.1.102")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("first SetDevice: %v", err)
	}

	changed, err := s.SetDevice(ctx, d, true)
	if err != nil {
		t.Fatalf("second SetDevice: %v", err)
	}
	// The secondary MAC-based UPDATE always touches the row, so changed=true
	// even though no field actually changed.
	if !changed {
		t.Error("known limitation: expected true from MAC path; got false")
	}
}

// TestSetDevice_UpdatedFieldReportsChange verifies that changing a field on an
// existing device is detected and returns changed=true.
func TestSetDevice_UpdatedFieldReportsChange(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-003", "aa:bb:cc:dd:ee:03", "old-name", "192.168.1.103")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("first SetDevice: %v", err)
	}

	d.Name_ = "new-name"
	changed, err := s.SetDevice(ctx, d, true)
	if err != nil {
		t.Fatalf("second SetDevice: %v", err)
	}
	if !changed {
		t.Error("expected true after changing device name; got false")
	}
}

// ── GetDeviceById ─────────────────────────────────────────────────────────────

func TestGetDeviceById(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-010", "aa:bb:cc:dd:ee:10", "my-switch", "192.168.1.110")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := s.GetDeviceById(ctx, "shellytest-010")
	if err != nil {
		t.Fatalf("GetDeviceById: %v", err)
	}
	if got.Id() != "shellytest-010" {
		t.Errorf("id: got %q, want %q", got.Id(), "shellytest-010")
	}
	if got.Name() != "my-switch" {
		t.Errorf("name: got %q, want %q", got.Name(), "my-switch")
	}
}

func TestGetDeviceById_NotFound(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.GetDeviceById(context.Background(), "does-not-exist")
	if err == nil {
		t.Error("expected error for missing device; got nil")
	}
}

// ── GetDeviceByMAC ────────────────────────────────────────────────────────────

func TestGetDeviceByMAC(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-020", "aa:bb:cc:dd:ee:20", "my-sensor", "192.168.1.120")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := s.GetDeviceByMAC(ctx, "aa:bb:cc:dd:ee:20")
	if err != nil {
		t.Fatalf("GetDeviceByMAC: %v", err)
	}
	if got.MAC != "aa:bb:cc:dd:ee:20" {
		t.Errorf("mac: got %q, want %q", got.MAC, "aa:bb:cc:dd:ee:20")
	}
}

// TestGetDeviceByMAC_EmptyReturnsError verifies the guard against empty MACs.
func TestGetDeviceByMAC_EmptyReturnsError(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.GetDeviceByMAC(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty MAC; got nil")
	}
}

// ── GetDeviceByName ───────────────────────────────────────────────────────────

func TestGetDeviceByName(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-030", "aa:bb:cc:dd:ee:30", "lounge-bulb", "192.168.1.130")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := s.GetDeviceByName(ctx, "lounge-bulb")
	if err != nil {
		t.Fatalf("GetDeviceByName: %v", err)
	}
	if got.Name() != "lounge-bulb" {
		t.Errorf("name: got %q, want %q", got.Name(), "lounge-bulb")
	}
}

// ── GetDeviceByHost ───────────────────────────────────────────────────────────

func TestGetDeviceByHost(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-040", "aa:bb:cc:dd:ee:40", "garage-relay", "192.168.1.140")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := s.GetDeviceByHost(ctx, "192.168.1.140")
	if err != nil {
		t.Fatalf("GetDeviceByHost: %v", err)
	}
	if got.Host() != "192.168.1.140" {
		t.Errorf("host: got %q, want %q", got.Host(), "192.168.1.140")
	}
}

// ── GetDeviceByAny ────────────────────────────────────────────────────────────

func TestGetDeviceByAny_ByName(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-050", "aa:bb:cc:dd:ee:50", "hall-motion", "192.168.1.150")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := s.GetDeviceByAny(ctx, "hall-motion")
	if err != nil {
		t.Fatalf("GetDeviceByAny(name): %v", err)
	}
	if got.Id() != "shellytest-050" {
		t.Errorf("id: got %q, want %q", got.Id(), "shellytest-050")
	}
}

func TestGetDeviceByAny_ById(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-051", "aa:bb:cc:dd:ee:51", "hall-temp", "192.168.1.151")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := s.GetDeviceByAny(ctx, "shellytest-051")
	if err != nil {
		t.Fatalf("GetDeviceByAny(id): %v", err)
	}
	if got.Id() != "shellytest-051" {
		t.Errorf("id: got %q, want %q", got.Id(), "shellytest-051")
	}
}

// ── GetAllDevices ─────────────────────────────────────────────────────────────

func TestGetAllDevices(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	want := []*myhome.Device{
		makeDevice("Shelly", "shellytest-060", "aa:bb:cc:dd:ee:60", "light-a", "192.168.1.160"),
		makeDevice("Shelly", "shellytest-061", "aa:bb:cc:dd:ee:61", "light-b", "192.168.1.161"),
		makeDevice("Shelly", "shellytest-062", "aa:bb:cc:dd:ee:62", "light-c", "192.168.1.162"),
	}
	for _, d := range want {
		if _, err := s.SetDevice(ctx, d, false); err != nil {
			t.Fatalf("SetDevice(%s): %v", d.Id(), err)
		}
	}

	all, err := s.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("GetAllDevices: %v", err)
	}
	if len(all) != len(want) {
		t.Errorf("count: got %d, want %d", len(all), len(want))
	}
}

// ── GetDevicesMatchingAny ─────────────────────────────────────────────────────

// TestGetDevicesMatchingAny verifies partial-name substring matching.
func TestGetDevicesMatchingAny(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	devs := []*myhome.Device{
		makeDevice("Shelly", "shellytest-070", "aa:bb:cc:dd:ee:70", "bedroom-light", "192.168.1.170"),
		makeDevice("Shelly", "shellytest-071", "aa:bb:cc:dd:ee:71", "kitchen-light", "192.168.1.171"),
		makeDevice("Shelly", "shellytest-072", "aa:bb:cc:dd:ee:72", "garage-sensor", "192.168.1.172"),
	}
	for _, d := range devs {
		if _, err := s.SetDevice(ctx, d, false); err != nil {
			t.Fatalf("SetDevice: %v", err)
		}
	}

	matches, err := s.GetDevicesMatchingAny(ctx, "light")
	if err != nil {
		t.Fatalf("GetDevicesMatchingAny: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("count: got %d, want 2", len(matches))
	}
}

// ── ForgetDevice ──────────────────────────────────────────────────────────────

func TestForgetDevice(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-080", "aa:bb:cc:dd:ee:80", "temp-sensor", "192.168.1.180")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	if err := s.ForgetDevice(ctx, "shellytest-080"); err != nil {
		t.Fatalf("ForgetDevice: %v", err)
	}

	_, err := s.GetDeviceById(ctx, "shellytest-080")
	if err == nil {
		t.Error("expected error after ForgetDevice; device still exists")
	}
}

// ── Room management ───────────────────────────────────────────────────────────

func TestSetAndGetDeviceRoom(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	d := makeDevice("Shelly", "shellytest-090", "aa:bb:cc:dd:ee:90", "office-heater", "192.168.1.190")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	changed, err := s.SetDeviceRoom(ctx, "shellytest-090", "office")
	if err != nil {
		t.Fatalf("SetDeviceRoom: %v", err)
	}
	if !changed {
		t.Error("expected SetDeviceRoom to report a change")
	}

	in, err := s.GetDevicesByRoom(ctx, "office")
	if err != nil {
		t.Fatalf("GetDevicesByRoom: %v", err)
	}
	if len(in) != 1 {
		t.Fatalf("GetDevicesByRoom count: got %d, want 1", len(in))
	}
	if in[0].RoomId != "office" {
		t.Errorf("room_id: got %q, want %q", in[0].RoomId, "office")
	}
}

func TestGetDevicesByRoom_Empty(t *testing.T) {
	s := newTestStorage(t)
	in, err := s.GetDevicesByRoom(context.Background(), "no-such-room")
	if err != nil {
		t.Fatalf("GetDevicesByRoom: %v", err)
	}
	if len(in) != 0 {
		t.Errorf("expected empty slice; got %d devices", len(in))
	}
}
