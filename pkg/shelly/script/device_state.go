package script

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/go-logr/logr"
)

// DeviceState represents the persistent state of a Shelly device for testing
type DeviceState struct {
	KVS     map[string]interface{} `json:"kvs"`
	Storage map[string]interface{} `json:"storage"`

	// Schedules tracks jobs created via Schedule.Create during script execution.
	// Each entry is the raw params map passed to Schedule.Create, plus an "id" field.
	Schedules []map[string]interface{} `json:"schedules,omitempty"`

	// ComponentStatus controls what Shelly.getComponentStatus() returns for
	// each component key (e.g. "switch:0", "input:1", "sys", "mqtt").
	// Populated by tests or loaded from a device snapshot (see FetchDeviceState).
	ComponentStatus map[string]interface{} `json:"component_status,omitempty"`

	// EventInjector, when non-nil, allows a test to push raw JSON-encoded
	// device-event objects into the running script's event-handler loop.
	// Send a JSON object with the same shape as Shelly device events:
	//   {"info": {"event": "pool-pump/night-stop"}}
	EventInjector chan []byte `json:"-"`

	// ScheduleEvalInjector, when non-nil, lets a test fire a Schedule job's
	// code the way a real device does: Schedule.eval on the schedule's due
	// instant calls Script.Eval(id, code) against the already-running
	// script's global scope. The emulator does not itself track wall-clock
	// time against Schedules' timespecs (see Schedule.List/.Create/.Update in
	// run.go — pure storage, never fired automatically), so a test that needs
	// a specific job's handler (e.g. "handleEveningStop()") to run sends a
	// JSON object {"code": "handleEveningStop()"} on this channel instead of
	// waiting on real time.
	ScheduleEvalInjector chan []byte `json:"-"`

	// OnModified is called whenever the device state is modified (KVS.Set, config changes, etc.)
	// This allows automatic persistence of state changes during script execution
	OnModified func() `json:"-"`

	// pendingNestedEvent, when set via SetPendingNestedEvent, is delivered to
	// the script's own event handlers synchronously from INSIDE the next
	// Switch.Set() call, immediately before that call's completion callback
	// fires — i.e. nested inside the Shelly.call the script itself issued.
	//
	// This exists to reproduce a real Shelly device's event interleaving,
	// which the default emulator event loop (a Go `select` over
	// EventInjector and friends in run.go) cannot: that loop only dequeues
	// the next event after the previous one's handler has fully returned, so
	// two events sent back-to-back on EventInjector are always processed
	// strictly in sequence, never nested. On real firmware, a fresh system
	// event (e.g. input:0 flapping) CAN be dispatched while a previous RPC's
	// native completion handler is still executing on the interpreter stack
	// — that is the exact mechanism behind the #450 crash ("Too much
	// recursion" in pool-pump.js's water-supply handling, triggered by
	// input:0 flapping faster than a Switch.Set round-trip). Without this
	// hook, no emulator test can reproduce that interleaving at all.
	//
	// Consumed exactly once per Switch.Set call (cleared by
	// TakePendingNestedEvent) so it targets a single call, not every one.
	pendingNestedEvent []byte

	nextScheduleID int

	// emittedEvents records every Shelly.emitEvent(name, data) call the
	// script makes, in order. Shelly.emitEvent only loops the event back to
	// the script's OWN Shelly.addEventHandler callback (see run.go) — it is
	// not published over MQTT or otherwise visible outside the running
	// script — so without this a test cannot tell whether a given event was
	// actually emitted, as opposed to merely inferring it from a side effect
	// that might have another cause. Populated unconditionally; cheap
	// (append to a slice) and nothing reads it unless a test calls
	// EmittedEvents()/EmittedEventCount().
	emittedEvents []EmittedEvent

	// mu guards every map and slice above. The emulator mutates them from the
	// goroutine running the script while tests poll them from the test
	// goroutine, which without this is a data race — and an unsynchronised map
	// read racing a write is not merely flaky: Go aborts the whole test binary
	// with "fatal error: concurrent map read and map write", losing every
	// result in the package rather than failing one test (#451).
	//
	// Access the fields through the accessors below rather than directly.
	// The exported fields remain exported only so DeviceState can still be
	// JSON-marshalled for snapshots; treat them as private otherwise.
	mu sync.RWMutex
}

// KVSValue returns one KVS entry under the read lock.
func (d *DeviceState) KVSValue(key string) (interface{}, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.KVS[key]
	return v, ok
}

// KVSString returns one KVS entry as a string. The second result is false when
// the key is absent or the value is not a string, so callers can distinguish
// "not set yet" from "set to something unexpected".
func (d *DeviceState) KVSString(key string) (string, bool) {
	v, ok := d.KVSValue(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// SetKVSValue writes one KVS entry under the write lock.
func (d *DeviceState) SetKVSValue(key string, value interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.KVS == nil {
		d.KVS = make(map[string]interface{})
	}
	d.KVS[key] = value
}

// DeleteKVSValue removes one KVS entry under the write lock.
func (d *DeviceState) DeleteKVSValue(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.KVS, key)
}

// KVSSnapshot returns a copy of the whole KVS map. A copy, not the live map:
// handing back the original would just move the race into the caller.
func (d *DeviceState) KVSSnapshot() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]interface{}, len(d.KVS))
	for k, v := range d.KVS {
		out[k] = v
	}
	return out
}

// StorageValue returns one Script.storage entry under the read lock.
func (d *DeviceState) StorageValue(key string) (interface{}, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.Storage[key]
	return v, ok
}

// SetStorageValue writes one Script.storage entry under the write lock.
func (d *DeviceState) SetStorageValue(key string, value interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Storage == nil {
		d.Storage = make(map[string]interface{})
	}
	d.Storage[key] = value
}

// DeleteStorageValue removes one Script.storage entry under the write lock.
func (d *DeviceState) DeleteStorageValue(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.Storage, key)
}

// ComponentStatusValue returns one component's status under the read lock.
//
// When the status is a map — which it is for every real component — a copy is
// returned. Handing back the live inner map would defeat the lock entirely: the
// caller would hold a reference it could read or mutate while the script
// goroutine writes the same map, which is precisely the race this type exists
// to prevent. Callers that need to change a field must use
// SetComponentStatusField.
func (d *DeviceState) ComponentStatusValue(name string) (interface{}, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.ComponentStatus[name]
	if !ok {
		return nil, false
	}
	if m, isMap := v.(map[string]interface{}); isMap {
		cp := make(map[string]interface{}, len(m))
		for k, mv := range m {
			cp[k] = mv
		}
		return cp, true
	}
	return v, true
}

// SetComponentStatusField sets one field on a component's status map in place,
// under the write lock, and reports whether the component existed and was a
// map. This is the safe form of the read-then-mutate pattern: doing it via
// ComponentStatusValue would only mutate the copy.
func (d *DeviceState) SetComponentStatusField(name, field string, value interface{}) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing, ok := d.ComponentStatus[name]
	if !ok {
		return false
	}
	m, isMap := existing.(map[string]interface{})
	if !isMap {
		return false
	}
	m[field] = value
	return true
}

// SetComponentStatusValue writes one component's status under the write lock.
func (d *DeviceState) SetComponentStatusValue(name string, value interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ComponentStatus == nil {
		d.ComponentStatus = make(map[string]interface{})
	}
	d.ComponentStatus[name] = value
}

// DeleteComponentStatusValue removes one component's status under the write lock.
func (d *DeviceState) DeleteComponentStatusValue(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.ComponentStatus, name)
}

// StorageLen reports how many Script.storage entries exist.
func (d *DeviceState) StorageLen() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.Storage)
}

// ScheduleJobs returns a copy of the schedule list, with each job map copied
// one level deep. Tests read fields out of individual jobs (timespec, enable),
// so returning the live job maps would leave the race in place.
func (d *DeviceState) ScheduleJobs() []map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]map[string]interface{}, 0, len(d.Schedules))
	for _, job := range d.Schedules {
		cp := make(map[string]interface{}, len(job))
		for k, v := range job {
			cp[k] = v
		}
		out = append(out, cp)
	}
	return out
}

// UpdateSchedule merges updates into the job with the given id and reports
// whether it existed. Held under the write lock so a concurrent ScheduleJobs()
// cannot observe a half-applied update.
func (d *DeviceState) UpdateSchedule(id int, updates map[string]interface{}) (map[string]interface{}, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, job := range d.Schedules {
		if scheduleID(job["id"]) != id {
			continue
		}
		for k, v := range updates {
			if k == "id" {
				continue
			}
			job[k] = v
		}
		cp := make(map[string]interface{}, len(job))
		for k, v := range job {
			cp[k] = v
		}
		return cp, true
	}
	return nil, false
}

// GetKVS returns a snapshot of the KVS map. It is a copy: see KVSSnapshot.
func (d *DeviceState) GetKVS() map[string]interface{} {
	return d.KVSSnapshot()
}

// SetPendingNestedEvent arms the next Switch.Set() call to deliver this raw
// JSON device event to the script's own event handlers synchronously, nested
// inside that call — see pendingNestedEvent's doc comment for why this
// exists (#450). Tests only; nil (the zero value) in normal operation.
func (d *DeviceState) SetPendingNestedEvent(event []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingNestedEvent = event
}

// TakePendingNestedEvent returns and clears the pending nested event, if any.
// Called by the Switch.Set emulation in run.go; consuming it here (rather
// than leaving it for the caller to clear) guarantees it fires at most once
// even if TakePendingNestedEvent is called from more than one nested call.
func (d *DeviceState) TakePendingNestedEvent() ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pendingNestedEvent == nil {
		return nil, false
	}
	event := d.pendingNestedEvent
	d.pendingNestedEvent = nil
	return event, true
}

// EmittedEvent is one recorded Shelly.emitEvent(name, data) call.
type EmittedEvent struct {
	Name string
	Data interface{}
}

// RecordEmittedEvent appends one Shelly.emitEvent(name, data) call. Called
// from run.go's Shelly.emitEvent implementation; not for test use.
func (d *DeviceState) RecordEmittedEvent(name string, data interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.emittedEvents = append(d.emittedEvents, EmittedEvent{Name: name, Data: data})
}

// EmittedEvents returns a copy of every Shelly.emitEvent call recorded so
// far, in order.
func (d *DeviceState) EmittedEvents() []EmittedEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]EmittedEvent, len(d.emittedEvents))
	copy(out, d.emittedEvents)
	return out
}

// EmittedEventCount returns how many times the script called
// Shelly.emitEvent(name, ...), for any data.
func (d *DeviceState) EmittedEventCount(name string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := 0
	for _, e := range d.emittedEvents {
		if e.Name == name {
			n++
		}
	}
	return n
}

// GetStorage returns a snapshot of the Script.storage map.
func (d *DeviceState) GetStorage() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]interface{}, len(d.Storage))
	for k, v := range d.Storage {
		out[k] = v
	}
	return out
}

// AddSchedule appends a schedule and returns its assigned ID (1-based).
func (d *DeviceState) AddSchedule(job map[string]interface{}) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextScheduleID++
	id := d.nextScheduleID
	job["id"] = id
	d.Schedules = append(d.Schedules, job)
	return id
}

// DeleteSchedule removes the schedule with the given ID.
func (d *DeviceState) DeleteSchedule(id int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	filtered := d.Schedules[:0]
	for _, s := range d.Schedules {
		if s["id"] != id {
			filtered = append(filtered, s)
		}
	}
	d.Schedules = filtered
}

// LoadDeviceState loads device state from a JSON file
// If the file doesn't exist, returns an empty state
func LoadDeviceState(log logr.Logger, filename string) (*DeviceState, error) {
	state := &DeviceState{
		KVS:             make(map[string]interface{}),
		Storage:         make(map[string]interface{}),
		ComponentStatus: make(map[string]interface{}),
	}

	// If file doesn't exist, return empty state
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		log.Info("Device state file not found, starting with empty state", "file", filename)
		return state, nil
	}

	// Read file
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read device state file: %w", err)
	}

	// Parse JSON
	err = json.Unmarshal(data, state)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device state file: %w", err)
	}

	// Ensure maps are initialized
	if state.KVS == nil {
		state.KVS = make(map[string]interface{})
	}
	if state.Storage == nil {
		state.Storage = make(map[string]interface{})
	}
	if state.ComponentStatus == nil {
		state.ComponentStatus = make(map[string]interface{})
	}

	log.Info("Loaded device state", "file", filename, "kvs_keys", len(state.KVS), "storage_keys", len(state.Storage))
	return state, nil
}

// SaveDeviceState saves device state to a JSON file with indentation
func SaveDeviceState(log logr.Logger, filename string, state *DeviceState) error {
	// Marshal with indentation
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device state: %w", err)
	}

	// Write to file
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write device state file: %w", err)
	}

	log.Info("Saved device state", "file", filename, "kvs_keys", len(state.KVS), "storage_keys", len(state.Storage))
	return nil
}

// RunWithDeviceFile runs a Shelly script with device state persistence
func RunWithDeviceFile(ctx context.Context, name string, buf []byte, minify bool, deviceFile string) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	// Load device state if file specified
	var deviceState *DeviceState
	if deviceFile != "" {
		deviceState, err = LoadDeviceState(log, deviceFile)
		if err != nil {
			log.Error(err, "Failed to load device state", "file", deviceFile)
			return err
		}

		// Set up automatic save on modification
		deviceState.OnModified = func() {
			log.V(1).Info("Device state modified, auto-saving", "file", deviceFile)
			if saveErr := SaveDeviceState(log, deviceFile, deviceState); saveErr != nil {
				log.Error(saveErr, "Failed to auto-save device state", "file", deviceFile)
			} else {
				log.V(1).Info("Device state auto-saved successfully", "file", deviceFile)
			}
		}
	} else {
		// Empty state
		deviceState = &DeviceState{
			KVS:     make(map[string]interface{}),
			Storage: make(map[string]interface{}),
		}
	}

	// Run the script with device state
	err = RunWithDeviceState(ctx, name, buf, minify, deviceState)

	// Save device state after script completes, but not if it was canceled
	// (cancellation means Ctrl+C or timeout, and the state may be incomplete)
	if deviceFile != "" && err != context.Canceled {
		if saveErr := SaveDeviceState(log, deviceFile, deviceState); saveErr != nil {
			log.Error(saveErr, "Failed to save device state", "file", deviceFile)
			if err == nil {
				err = saveErr
			}
		}
	}

	return err
}
