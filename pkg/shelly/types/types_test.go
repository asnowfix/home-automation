package types

import "testing"

// TestApiString_AllConstantsNamed guards against the exact failure mode a
// positional []string{...} literal has: a new Api constant inserted into the
// iota block above without a corresponding apiNames entry. Since apiNames is
// keyed by the constants themselves, this only fails if a constant's entry
// is missing outright — never from a silent reordering.
func TestApiString_AllConstantsNamed(t *testing.T) {
	all := []Api{
		Shelly,
		Schedule,
		Webhook,
		HTTP,
		KVS,
		System,
		WiFi,
		Ethernet,
		BluetoothLowEnergy,
		Cloud,
		Mqtt,
		OutboundWebsocket,
		Script,
		Input,
		Modbus,
		Voltmeter,
		Cover,
		Switch,
		Light,
		DevicePower,
		Humidity,
		Temperature,
		None,
	}

	if got, want := len(apiNames), len(all); got != want {
		t.Errorf("apiNames has %d entries, expected exactly %d (one per Api constant) — a constant was added/removed without updating apiNames", got, want)
	}

	seen := make(map[Api]bool, len(all))
	for _, api := range all {
		if seen[api] {
			t.Errorf("Api constant %d listed twice in this test's all slice", api)
		}
		seen[api] = true

		name, ok := apiNames[api]
		if !ok || name == "" {
			t.Errorf("Api constant %d (%q via String()) has no entry in apiNames", api, api.String())
		}
	}
}

func TestApiString_UnknownValueDoesNotPanic(t *testing.T) {
	unknown := Api(9999)
	if got, want := unknown.String(), "Api(9999)"; got != want {
		t.Errorf("String() for unknown Api = %q, want %q", got, want)
	}
}
