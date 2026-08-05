package script

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
)

// ExecutionMode controls how strictly the goja-based emulator enforces
// Shelly Gen2 per-script resource limits (see AGENTS.md "Per-script
// limits"). This is a minimal slice of issue #250 — only the concurrent
// Shelly.call ceiling is modeled here; timer/event-handler/status-handler/
// MQTT-subscription count limits are #250's remaining scope, not this one's.
type ExecutionMode int

const (
	// DeviceTestMode is the zero value / default: the emulator mirrors the
	// real device's concurrent-call ceiling, panicking with a message
	// matching the real device's "Too many calls in progress" error once
	// MaxConcurrentCalls calls are already in flight. Use for all unit
	// tests exercising scripts against simulated device behavior.
	DeviceTestMode ExecutionMode = iota
	// DeviceExtensionMode disables the ceiling entirely, for daemon-side
	// script execution that intentionally aggregates state across multiple
	// devices and must not be constrained by any single device's per-script
	// limits.
	DeviceExtensionMode
)

// MaxConcurrentCalls mirrors the real Shelly Gen2 per-script ceiling on
// in-flight Shelly.call RPCs (see AGENTS.md "Per-script limits"). Exceeding
// it on real hardware raises an uncaught "Too many calls in progress"
// exception that kills the entire script (see issue #421).
const MaxConcurrentCalls = 5

// DeviceState represents the persistent state of a Shelly device for testing
type DeviceState struct {
	KVS     map[string]interface{} `json:"kvs"`
	Storage map[string]interface{} `json:"storage"`

	// Mode selects resource-limit enforcement (see ExecutionMode). The zero
	// value is DeviceTestMode, so existing callers that don't set this field
	// keep getting strict enforcement by default.
	Mode ExecutionMode `json:"-"`

	// CallDelay, when non-zero and Mode is DeviceTestMode, artificially
	// delays the *release* of a Shelly.call's in-flight slot (not the
	// call's dispatch or its callback — those stay synchronous, preserving
	// every script's existing callback-ordering assumptions). This lets a
	// test create genuinely overlapping in-flight calls — e.g. several
	// calls dispatched a fixed tick apart, each taking longer than that
	// tick to "complete" from the device's perspective — to exercise the
	// concurrent-call ceiling (see issue #421). Zero (default) preserves
	// the emulator's original synchronous-release behavior. Ignored in
	// DeviceExtensionMode.
	CallDelay time.Duration `json:"-"`

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

	// OnModified is called whenever the device state is modified (KVS.Set, config changes, etc.)
	// This allows automatic persistence of state changes during script execution
	OnModified func() `json:"-"`

	nextScheduleID int
}

// GetKVS implements the DeviceState interface
func (d *DeviceState) GetKVS() map[string]interface{} {
	return d.KVS
}

// GetStorage implements the DeviceState interface
func (d *DeviceState) GetStorage() map[string]interface{} {
	return d.Storage
}

// AddSchedule appends a schedule and returns its assigned ID (1-based).
func (d *DeviceState) AddSchedule(job map[string]interface{}) int {
	d.nextScheduleID++
	id := d.nextScheduleID
	job["id"] = id
	d.Schedules = append(d.Schedules, job)
	return id
}

// DeleteSchedule removes the schedule with the given ID.
func (d *DeviceState) DeleteSchedule(id int) {
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
