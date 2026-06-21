package garden

import (
	"fmt"
	"testing"
)

// TestDefaultZoneKVS verifies that defaultZoneKVS() emits the differentiated-
// cadence keys (zoneN-group, zoneN-interval) added alongside ZONE_DEFAULTS in
// garden.js, with the expected lawn/beds grouping and intervalDays values.
func TestDefaultZoneKVS(t *testing.T) {
	m := defaultZoneKVS()

	want := map[string]string{
		kvsPrefix + "zone0-group":    "lawn",
		kvsPrefix + "zone0-interval": "1",
		kvsPrefix + "zone1-group":    "beds",
		kvsPrefix + "zone1-interval": "4",
		kvsPrefix + "zone2-group":    "lawn",
		kvsPrefix + "zone2-interval": "1",
	}
	for k, wantV := range want {
		gotV, ok := m[k]
		if !ok {
			t.Errorf("defaultZoneKVS() missing key %q", k)
			continue
		}
		if gotV != wantV {
			t.Errorf("defaultZoneKVS()[%q] = %q, want %q", k, gotV, wantV)
		}
	}

	// Every zone must define both group and interval — garden.js's group
	// cadence gating silently treats a missing group as its own singleton
	// group, so a dropped key here would fail quietly on-device.
	for i := range defaultZoneDefaults {
		for _, suffix := range []string{"group", "interval"} {
			key := kvsPrefix + fmt.Sprintf("zone%d-%s", i, suffix)
			if _, ok := m[key]; !ok {
				t.Errorf("defaultZoneKVS() missing %q for zone %d", suffix, i)
			}
		}
	}
}

// TestDefaultZoneKVS_KeyLengthBudget guards the KVS key length budget
// documented above gardenKVSKeys ("prefix (14) + suffix <=18 chars = <=32
// chars total"). Exceeding it risks hitting Shelly's hard KVS key limit.
func TestDefaultZoneKVS_KeyLengthBudget(t *testing.T) {
	const maxTotal = 32
	for k := range defaultZoneKVS() {
		if len(k) > maxTotal {
			t.Errorf("KVS key %q is %d chars, exceeds the %d-char budget", k, len(k), maxTotal)
		}
	}
}
