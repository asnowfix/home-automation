package shelly

import (
	"encoding/json"
	"fmt"

	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
)

// The device status is copy-on-write: a published *shelly.Status is never
// mutated, only replaced under statusMu. Readers (UI rendering) therefore
// need no lock beyond the pointer read in Status(), and a notification merge
// running on the MQTT watcher goroutine is not a data race with a concurrent
// Refresh. It is still a lost update: a Refresh that read the device before a
// flip can SetStatus after the merge and overwrite it. That is masked today
// because every notification also queues a follow-up refresh, and has never
// been observed.

// SetStatus replaces the status snapshot (used by Refresh, which reads the
// full status from the device).
func (d *Device) SetStatus(s *shelly.Status) {
	d.statusMu.Lock()
	d.status = s
	d.statusMu.Unlock()
}

// ApplyStatusNotification merges a NotifyStatus / NotifyFullStatus `params`
// object into the in-memory status, for the components whose state the UI
// shows: switch:N (output and the other switch fields present in the delta)
// and input:N (state). Fields absent from the delta keep their value.
// Everything else in the notification is ignored here.
//
// It reports whether a user-visible state changed: a switch output or an
// input state. Pure measurement deltas (apower, aenergy, temperature, ...) are
// merged but report false, so a metering plug does not trigger a UI refresh
// on every sample.
//
// A device with no status yet (never refreshed) is left alone: a partial
// delta cannot stand in for a full status, and the state stays unknown until
// the first refresh.
func (d *Device) ApplyStatusNotification(params json.RawMessage) (bool, error) {
	var delta map[string]json.RawMessage
	if err := json.Unmarshal(params, &delta); err != nil {
		return false, fmt.Errorf("status notification params: %w", err)
	}

	d.statusMu.Lock()
	defer d.statusMu.Unlock()
	if d.status == nil {
		return false, nil
	}

	next := *d.status // shallow copy; component pointers are replaced below, never written through
	changed := false

	switches := [4]**sswitch.Status{&next.Switch0, &next.Switch1, &next.Switch2, &next.Switch3}
	for i, slot := range switches {
		raw, ok := delta[fmt.Sprintf("switch:%d", i)]
		if !ok {
			continue
		}
		var merged sswitch.Status
		if *slot != nil {
			merged = **slot
			// Slices would alias the published snapshot, and Unmarshal reuses
			// a slice's backing array: give the copy its own.
			merged.Aenergy.ByMinute = append([]float32(nil), merged.Aenergy.ByMinute...)
			merged.Errors = append([]string(nil), merged.Errors...)
		}
		// Unmarshalling over a populated struct only overwrites the fields present.
		if err := json.Unmarshal(raw, &merged); err != nil {
			return false, fmt.Errorf("switch:%d: %w", i, err)
		}
		if *slot == nil || (*slot).Output != merged.Output {
			changed = true
		}
		*slot = &merged
	}

	inputs := [4]**sswitch.InputStatus{&next.Input0, &next.Input1, &next.Input2, &next.Input3}
	for i, slot := range inputs {
		raw, ok := delta[fmt.Sprintf("input:%d", i)]
		if !ok {
			continue
		}
		var merged sswitch.InputStatus
		if *slot != nil {
			merged = **slot
		}
		if err := json.Unmarshal(raw, &merged); err != nil {
			return false, fmt.Errorf("input:%d: %w", i, err)
		}
		if *slot == nil || (*slot).State != merged.State {
			changed = true
		}
		*slot = &merged
	}

	d.status = &next
	return changed, nil
}
