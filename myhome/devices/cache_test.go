package devices

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"myhome"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// testCtx returns a context with a test-aware logger.
// NewCache panics if the context has no logger, so every test needs this.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	return logr.NewContext(context.Background(), testr.New(t))
}

// fakeDevice constructs a minimal *myhome.Device for use in tests.
func fakeDevice(id, name, mac, host string) *myhome.Device {
	return &myhome.Device{
		DeviceSummary: myhome.DeviceSummary{
			DeviceIdentifier: myhome.DeviceIdentifier{
				Manufacturer_: "Shelly",
				Id_:           id,
			},
			MAC:   mac,
			Name_: name,
			Host_: host,
		},
	}
}

// ── fakeRegistry ─────────────────────────────────────────────────────────────
//
// fakeRegistry is a simple in-memory implementation of DeviceRegistry.
// It tracks how many times each method was called so tests can assert
// whether the cache hit or missed.

type fakeRegistry struct {
	mu      sync.Mutex
	devices map[string]*myhome.Device // keyed by device id
	rooms   map[string]string         // device id → room id
	calls   map[string]int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{
		devices: make(map[string]*myhome.Device),
		rooms:   make(map[string]string),
		calls:   make(map[string]int),
	}
}

var errNotFound = errors.New("not found")

func (f *fakeRegistry) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Flush"]++
	f.devices = make(map[string]*myhome.Device)
	f.rooms = make(map[string]string)
	return nil
}

func (f *fakeRegistry) SetDevice(_ context.Context, d *myhome.Device, overwrite bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["SetDevice"]++
	if _, ok := f.devices[d.Id()]; ok && !overwrite {
		return false, errors.New("device already exists")
	}
	f.devices[d.Id()] = d
	return true, nil
}

func (f *fakeRegistry) GetDeviceById(_ context.Context, id string) (*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDeviceById"]++
	if d, ok := f.devices[id]; ok {
		return d, nil
	}
	return nil, errNotFound
}

func (f *fakeRegistry) GetDeviceByMAC(_ context.Context, mac string) (*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDeviceByMAC"]++
	for _, d := range f.devices {
		if d.MAC == mac {
			return d, nil
		}
	}
	return nil, errNotFound
}

func (f *fakeRegistry) GetDeviceByName(_ context.Context, name string) (*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDeviceByName"]++
	for _, d := range f.devices {
		if d.Name() == name {
			return d, nil
		}
	}
	return nil, errNotFound
}

func (f *fakeRegistry) GetDeviceByHost(_ context.Context, host string) (*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDeviceByHost"]++
	for _, d := range f.devices {
		if d.Host() == host {
			return d, nil
		}
	}
	return nil, errNotFound
}

func (f *fakeRegistry) GetDeviceByAny(_ context.Context, key string) (*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDeviceByAny"]++
	for _, d := range f.devices {
		if d.Id() == key || d.MAC == key || d.Name() == key || d.Host() == key {
			return d, nil
		}
	}
	return nil, errNotFound
}

func (f *fakeRegistry) GetAllDevices(_ context.Context) ([]*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetAllDevices"]++
	out := make([]*myhome.Device, 0, len(f.devices))
	for _, d := range f.devices {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeRegistry) GetDevicesMatchingAny(_ context.Context, _ string) ([]*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDevicesMatchingAny"]++
	return nil, nil
}

func (f *fakeRegistry) ForgetDevice(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["ForgetDevice"]++
	delete(f.devices, id)
	return nil
}

func (f *fakeRegistry) SetDeviceRoom(_ context.Context, identifier string, roomId string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["SetDeviceRoom"]++
	if _, ok := f.devices[identifier]; ok {
		f.rooms[identifier] = roomId
		return true, nil
	}
	return false, nil
}

func (f *fakeRegistry) GetDevicesByRoom(_ context.Context, roomId string) ([]*myhome.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetDevicesByRoom"]++
	var out []*myhome.Device
	for id, r := range f.rooms {
		if r == roomId {
			out = append(out, f.devices[id])
		}
	}
	return out, nil
}

// ── tests: in-memory maps ─────────────────────────────────────────────────────

// TestCache_SetDevice_PopulatesAllMaps verifies that SetDevice writes the device
// into all four internal lookup maps (by id, MAC, host, and name).
func TestCache_SetDevice_PopulatesAllMaps(t *testing.T) {
	c := NewCache(testCtx(t), newFakeRegistry())
	d := fakeDevice("shelly-001", "kitchen-light", "aa:bb:cc:dd:ee:01", "192.168.1.1")

	if _, err := c.SetDevice(context.Background(), d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	if c.devicesById["shelly-001"] == nil {
		t.Error("devicesById: device missing")
	}
	if c.devicesByMAC["aa:bb:cc:dd:ee:01"] == nil {
		t.Error("devicesByMAC: device missing")
	}
	if c.devicesByHost["192.168.1.1"] == nil {
		t.Error("devicesByHost: device missing")
	}
	if c.devicesByName["kitchen-light"] == nil {
		t.Error("devicesByName: device missing")
	}
}

// ── tests: cache hits (registry must NOT be called) ───────────────────────────

func TestCache_GetDeviceById_CacheHit(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-002", "hall-light", "aa:bb:cc:dd:ee:02", "192.168.1.2")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	before := reg.calls["GetDeviceById"]
	got, err := c.GetDeviceById(ctx, "shelly-002")
	if err != nil {
		t.Fatalf("GetDeviceById: %v", err)
	}
	if got.Id() != "shelly-002" {
		t.Errorf("id: got %q, want %q", got.Id(), "shelly-002")
	}
	if reg.calls["GetDeviceById"] != before {
		t.Error("cache miss: registry was called for a device that should have been cached")
	}
}

func TestCache_GetDeviceByMAC_CacheHit(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-003", "lounge-plug", "aa:bb:cc:dd:ee:03", "192.168.1.3")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := c.GetDeviceByMAC(ctx, "aa:bb:cc:dd:ee:03")
	if err != nil {
		t.Fatalf("GetDeviceByMAC: %v", err)
	}
	if got.MAC != "aa:bb:cc:dd:ee:03" {
		t.Errorf("mac: got %q, want %q", got.MAC, "aa:bb:cc:dd:ee:03")
	}
	if reg.calls["GetDeviceByMAC"] != 0 {
		t.Errorf("registry called %d times on MAC cache hit", reg.calls["GetDeviceByMAC"])
	}
}

func TestCache_GetDeviceByHost_CacheHit(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-004", "office-fan", "aa:bb:cc:dd:ee:04", "192.168.1.4")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := c.GetDeviceByHost(ctx, "192.168.1.4")
	if err != nil {
		t.Fatalf("GetDeviceByHost: %v", err)
	}
	if got.Host() != "192.168.1.4" {
		t.Errorf("host: got %q, want %q", got.Host(), "192.168.1.4")
	}
	if reg.calls["GetDeviceByHost"] != 0 {
		t.Errorf("registry called %d times on host cache hit", reg.calls["GetDeviceByHost"])
	}
}

func TestCache_GetDeviceByName_CacheHit(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-005", "bedroom-heater", "aa:bb:cc:dd:ee:05", "192.168.1.5")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	got, err := c.GetDeviceByName(ctx, "bedroom-heater")
	if err != nil {
		t.Fatalf("GetDeviceByName: %v", err)
	}
	if got.Name() != "bedroom-heater" {
		t.Errorf("name: got %q, want %q", got.Name(), "bedroom-heater")
	}
	if reg.calls["GetDeviceByName"] != 0 {
		t.Errorf("registry called %d times on name cache hit", reg.calls["GetDeviceByName"])
	}
}

// TestCache_GetDeviceByAny_HitsCache verifies that GetDeviceByAny finds a
// cached device via all four key variants (id, MAC, host, name) without
// touching the registry.
func TestCache_GetDeviceByAny_HitsCache(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-006", "front-door", "aa:bb:cc:dd:ee:06", "192.168.1.6")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	for _, key := range []string{"shelly-006", "aa:bb:cc:dd:ee:06", "192.168.1.6", "front-door"} {
		got, err := c.GetDeviceByAny(ctx, key)
		if err != nil {
			t.Fatalf("GetDeviceByAny(%q): %v", key, err)
		}
		if got.Id() != "shelly-006" {
			t.Errorf("key %q: got id %q, want %q", key, got.Id(), "shelly-006")
		}
	}
	if reg.calls["GetDeviceByAny"] != 0 {
		t.Errorf("registry called %d times for cache hits", reg.calls["GetDeviceByAny"])
	}
}

// ── tests: cache misses (registry MUST be called) ─────────────────────────────

// TestCache_GetDeviceById_CacheMiss_FallsThrough puts a device directly in the
// registry (bypassing the cache) and confirms the cache falls through to it.
func TestCache_GetDeviceById_CacheMiss_FallsThrough(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	// Bypass the cache by writing directly to the registry.
	reg.devices["shelly-007"] = fakeDevice("shelly-007", "garage-sensor", "aa:bb:cc:dd:ee:07", "192.168.1.7")

	got, err := c.GetDeviceById(ctx, "shelly-007")
	if err != nil {
		t.Fatalf("GetDeviceById: %v", err)
	}
	if got.Id() != "shelly-007" {
		t.Errorf("id: got %q, want %q", got.Id(), "shelly-007")
	}
	if reg.calls["GetDeviceById"] == 0 {
		t.Error("expected registry to be called on cache miss")
	}
}

// ── tests: overwrite / duplicate handling ─────────────────────────────────────

func TestCache_SetDevice_Overwrite(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-010", "old-name", "aa:bb:cc:dd:ee:10", "192.168.1.10")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice (insert): %v", err)
	}

	d2 := fakeDevice("shelly-010", "new-name", "aa:bb:cc:dd:ee:10", "192.168.1.10")
	if _, err := c.SetDevice(ctx, d2, true); err != nil {
		t.Fatalf("SetDevice (overwrite): %v", err)
	}

	got, err := c.GetDeviceByName(ctx, "new-name")
	if err != nil {
		t.Fatalf("GetDeviceByName after overwrite: %v", err)
	}
	if got.Id() != "shelly-010" {
		t.Errorf("id after overwrite: got %q, want %q", got.Id(), "shelly-010")
	}
}

func TestCache_SetDevice_NoOverwrite_RejectsDuplicate(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-011", "my-device", "aa:bb:cc:dd:ee:11", "192.168.1.11")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice (first): %v", err)
	}

	_, err := c.SetDevice(ctx, d, false)
	if err == nil {
		t.Error("expected error on duplicate insert without overwrite; got nil")
	}
}

// ── tests: Flush ──────────────────────────────────────────────────────────────

// TestCache_Flush_ClearsAllMaps verifies that Flush resets all five internal
// data structures to empty without touching the registry.
func TestCache_Flush_ClearsAllMaps(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-020", "temp", "aa:bb:cc:dd:ee:20", "192.168.1.20")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	if err := c.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if len(c.devices) != 0 {
		t.Errorf("devices slice not cleared: %d entries remain", len(c.devices))
	}
	if len(c.devicesById) != 0 {
		t.Errorf("devicesById not cleared: %d entries remain", len(c.devicesById))
	}
	if len(c.devicesByMAC) != 0 {
		t.Errorf("devicesByMAC not cleared: %d entries remain", len(c.devicesByMAC))
	}
	if len(c.devicesByHost) != 0 {
		t.Errorf("devicesByHost not cleared: %d entries remain", len(c.devicesByHost))
	}
	if len(c.devicesByName) != 0 {
		t.Errorf("devicesByName not cleared: %d entries remain", len(c.devicesByName))
	}
}

// ── tests: delegation to registry (the TODO methods) ─────────────────────────

// TestCache_GetAllDevices_DelegatesToRegistry documents the current TODO
// behaviour: GetAllDevices always calls the registry, even if the cache is
// populated. This test will need updating when the TODO is resolved.
func TestCache_GetAllDevices_DelegatesToRegistry(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	// Write two devices directly to the registry, bypassing the cache.
	reg.devices["shelly-030"] = fakeDevice("shelly-030", "a", "aa:bb:cc:dd:ee:30", "192.168.1.30")
	reg.devices["shelly-031"] = fakeDevice("shelly-031", "b", "aa:bb:cc:dd:ee:31", "192.168.1.31")

	all, err := c.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("GetAllDevices: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("count: got %d, want 2", len(all))
	}
	// The registry must have been called (current TODO behaviour).
	if reg.calls["GetAllDevices"] == 0 {
		t.Error("expected GetAllDevices to delegate to registry")
	}
}

// TestCache_ForgetDevice_DelegatesToRegistry documents the current TODO
// behaviour: ForgetDevice always calls the registry.
func TestCache_ForgetDevice_DelegatesToRegistry(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	reg.devices["shelly-040"] = fakeDevice("shelly-040", "to-delete", "aa:bb:cc:dd:ee:40", "192.168.1.40")

	if err := c.ForgetDevice(ctx, "shelly-040"); err != nil {
		t.Fatalf("ForgetDevice: %v", err)
	}
	if reg.calls["ForgetDevice"] == 0 {
		t.Error("expected ForgetDevice to delegate to registry")
	}
	if _, ok := reg.devices["shelly-040"]; ok {
		t.Error("device still in registry after ForgetDevice")
	}
}

// ── tests: room management ────────────────────────────────────────────────────

// TestCache_SetDeviceRoom_UpdatesInPlacePointer verifies that SetDeviceRoom
// mutates the RoomId field of the already-cached device pointer, so a subsequent
// GetDeviceById returns the updated room without going to the registry.
func TestCache_SetDeviceRoom_UpdatesInPlacePointer(t *testing.T) {
	reg := newFakeRegistry()
	c := NewCache(testCtx(t), reg)
	ctx := context.Background()

	d := fakeDevice("shelly-050", "office-heater", "aa:bb:cc:dd:ee:50", "192.168.1.50")
	if _, err := c.SetDevice(ctx, d, false); err != nil {
		t.Fatalf("SetDevice: %v", err)
	}

	if _, err := c.SetDeviceRoom(ctx, "shelly-050", "office"); err != nil {
		t.Fatalf("SetDeviceRoom: %v", err)
	}

	got, err := c.GetDeviceById(ctx, "shelly-050")
	if err != nil {
		t.Fatalf("GetDeviceById after SetDeviceRoom: %v", err)
	}
	if got.RoomId != "office" {
		t.Errorf("RoomId: got %q, want %q", got.RoomId, "office")
	}
}

// ── tests: concurrency ────────────────────────────────────────────────────────

// TestCache_Concurrent_NoRace runs concurrent SetDevice and Get operations and
// relies on the race detector (go test -race) to catch data races.
func TestCache_Concurrent_NoRace(t *testing.T) {
	c := NewCache(testCtx(t), newFakeRegistry())
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("shelly-%03d", n)
			mac := fmt.Sprintf("aa:bb:cc:dd:00:%02x", n)
			host := fmt.Sprintf("192.168.3.%d", n)
			d := fakeDevice(id, id, mac, host)
			_, _ = c.SetDevice(ctx, d, true)
			_, _ = c.GetDeviceById(ctx, id)
			_, _ = c.GetDeviceByMAC(ctx, mac)
			_, _ = c.GetDeviceByHost(ctx, host)
		}(i)
	}
	wg.Wait()
}
