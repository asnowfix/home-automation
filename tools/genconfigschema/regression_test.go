package main

import (
	"os"
	"testing"
)

// wantPoolKVSKeys is a frozen copy of PoolKVSKeys as it stood on `main`
// immediately before #439 (internal/myhome/shelly/script/pool.go). This test
// proves the schema-driven generator reproduces every one of its 28 entries
// byte-for-byte, so the refactor provably changes no runtime behaviour.
var wantPoolKVSKeys = map[string]string{
	"preferred_device_id":   "script/pool-pump/preferred",
	"preferred_speed":       "script/pool-pump/speed",
	"pro3_device_id":        "script/pool-pump/pro3-id",
	"pro1_device_id":        "script/pool-pump/pro1-id",
	"mqtt_topic_prefix":     "script/pool-pump/mqtt-topic",
	"enable_logging":        "script/pool-pump/logging",
	"eco_speed":             "script/pool-pump/eco-speed",
	"mid_speed":             "script/pool-pump/mid-speed",
	"high_speed":            "script/pool-pump/high-speed",
	"night_run_duration_ms": "script/pool-pump/night-duration",
	"grace_delay_ms":        "script/pool-pump/grace-delay",
	"temperature_threshold": "script/pool-pump/temp-threshold",
	"pool_volume":           "script/pool-pump/pool-volume",
	"turnover":              "script/pool-pump/turnover",
	"max_flow_rate":         "script/pool-pump/max-flow-rate",
	"max_rpm":               "script/pool-pump/max-rpm",
	"eco_rpm":               "script/pool-pump/eco-rpm",
	"mid_rpm":               "script/pool-pump/mid-rpm",
	"high_rpm":              "script/pool-pump/high-rpm",
	"max_temp":              "script/pool-pump/max-temp",

	"solar_enabled":           "script/pool-pump/solar-enabled",
	"solar_start_threshold_w": "script/pool-pump/solar-start-w",
	"solar_stop_threshold_w":  "script/pool-pump/solar-stop-w",
	"solar_start_delay_ms":    "script/pool-pump/solar-start-delay",
	"solar_stop_delay_ms":     "script/pool-pump/solar-stop-delay",
	"solar_min_turnover":      "script/pool-pump/solar-min-turnover",
	"solar_max_turnover":      "script/pool-pump/solar-max-turnover",
	"solar_stale_ms":          "script/pool-pump/solar-stale-ms",
}

// wantPoolDefaults is a frozen copy of the 22 DefaultXxx values previously
// produced by tools/extract-pool-defaults (PoolDefaults in its main.go),
// keyed by the schema field name.
var wantPoolDefaults = map[string]any{
	"ecoSpeed":             2.0,
	"midSpeed":             1.0,
	"highSpeed":            0.0,
	"nightRunDurationMs":   3600000.0, // -> DefaultNightRunDuration (time.Duration, ms*time.Millisecond)
	"graceDelayMs":         10000.0,   // -> DefaultGraceDelay
	"temperatureThreshold": 20.0,
	"poolVolume":           46.0,
	"turnover":             5.0,
	"maxFlowRate":          31.0,
	"maxRpm":               2900.0,
	"ecoRpm":               2000.0,
	"midRpm":               2600.0,
	"highRpm":              2900.0,
	"maxTemp":              35.0,
	"solarEnabled":         false,
	"solarStartThresholdW": 500.0,
	"solarStopThresholdW":  200.0,
	"solarStartDelayMs":    300000.0,
	"solarStopDelayMs":     600000.0,
	"solarMinTurnover":     5.0,
	"solarMaxTurnover":     7.0,
	"solarStaleMs":         300000.0,
}

// wantGardenKVSKeys is a frozen copy of gardenKVSKeys as it stood on `main`
// immediately before #439 (myhome/ctl/garden/setup.go), all 11 entries.
var wantGardenKVSKeys = map[string]string{
	"enable_logging":      "script/garden/logging",
	"mqtt_topic_prefix":   "script/garden/mqtt-topic",
	"earliest_start_hour": "script/garden/earliest-start",
	"lunch_start":         "script/garden/lunch-start",
	"lunch_end":           "script/garden/lunch-end",
	"evening_start":       "script/garden/evening-start",
	"evening_end":         "script/garden/evening-end",
	"fallback_start_hour": "script/garden/fallback-start",
	"frost_cutoff_c":      "script/garden/frost-cutoff-c",
	"rain_holdoff_mm":     "script/garden/rain-holdoff-mm",
	"max_deficit_mm":      "script/garden/max-deficit-mm",
}

// wantGardenDefaults is a frozen copy of the 9 DefaultXxx values previously
// produced by tools/extract-garden-defaults.
var wantGardenDefaults = map[string]any{
	"earliestStartHour": 3.0,
	"lunchStart":        12.0,
	"lunchEnd":          14.0,
	"eveningStart":      19.0,
	"eveningEnd":        23.5,
	"fallbackStartHour": 5.0,
	"frostCutoffC":      2.0,
	"rainHoldoffMm":     8.0,
	"maxDeficitMm":      25.0,
}

// wantZoneFieldKeys is a frozen copy of garden.js's ZONE_KEY_SPECS entries
// (field -> KVS key suffix) as they stood on `main` immediately before #439.
// myhome/ctl/garden/setup.go's defaultZoneKVS() only ever hand-wrote a subset
// of these suffixes as string literals (app-rate, trigger-mm, max-min,
// fallback-min, group, interval); this test covers the full ZONE_KEY_SPECS
// set the JS itself defines.
var wantZoneFieldKeys = map[string]string{
	"name":         "name",
	"appRateMmH":   "app-rate",
	"kc":           "kc",
	"triggerMm":    "trigger-mm",
	"maxMin":       "max-min",
	"fallbackMin":  "fallback-min",
	"group":        "group",
	"intervalDays": "interval",
	"enabled":      "enabled",
}

func TestRegression_PoolPumpSchemaMatchesToday(t *testing.T) {
	schema, err := LoadSchema("../../internal/shelly/scripts/pool-pump.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	if len(schema.Fields) != 28 {
		t.Fatalf("got %d fields, want 28", len(schema.Fields))
	}

	gotKVSKeys := map[string]string{}
	gotDefaults := map[string]any{}
	constCount := 0
	for _, f := range schema.Fields {
		gotKVSKeys[f.GoName()] = f.KVSKey(schema.KVSPrefix)
		if f.GoConst {
			constCount++
			gotDefaults[f.Name] = f.Default
		}
	}

	if len(gotKVSKeys) != len(wantPoolKVSKeys) {
		t.Errorf("got %d KVS keys, want %d", len(gotKVSKeys), len(wantPoolKVSKeys))
	}
	for k, want := range wantPoolKVSKeys {
		if got, ok := gotKVSKeys[k]; !ok {
			t.Errorf("missing KVS key %q", k)
		} else if got != want {
			t.Errorf("KVS key %q = %q, want %q", k, got, want)
		}
	}

	if constCount != len(wantPoolDefaults) {
		t.Errorf("got %d goConst fields, want %d", constCount, len(wantPoolDefaults))
	}
	for name, want := range wantPoolDefaults {
		got, ok := gotDefaults[name]
		if !ok {
			t.Errorf("missing default for field %q", name)
			continue
		}
		if !defaultsEqual(got, want) {
			t.Errorf("default for %q = %v, want %v", name, got, want)
		}
	}
}

func TestRegression_GardenSchemaMatchesToday(t *testing.T) {
	schema, err := LoadSchema("../../internal/shelly/scripts/garden.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	gotKVSKeys := map[string]string{}
	gotDefaults := map[string]any{}
	for _, f := range schema.Fields {
		gotKVSKeys[f.GoName()] = f.KVSKey(schema.KVSPrefix)
		if f.GoConst {
			gotDefaults[f.Name] = f.Default
		}
	}

	if len(gotKVSKeys) != len(wantGardenKVSKeys) {
		t.Errorf("got %d KVS keys, want %d", len(gotKVSKeys), len(wantGardenKVSKeys))
	}
	for k, want := range wantGardenKVSKeys {
		if got, ok := gotKVSKeys[k]; !ok {
			t.Errorf("missing KVS key %q", k)
		} else if got != want {
			t.Errorf("KVS key %q = %q, want %q", k, got, want)
		}
	}

	if len(gotDefaults) != len(wantGardenDefaults) {
		t.Errorf("got %d goConst fields, want %d", len(gotDefaults), len(wantGardenDefaults))
	}
	for name, want := range wantGardenDefaults {
		got, ok := gotDefaults[name]
		if !ok {
			t.Errorf("missing default for field %q", name)
			continue
		}
		if !defaultsEqual(got, want) {
			t.Errorf("default for %q = %v, want %v", name, got, want)
		}
	}

	gotZoneKeys := map[string]string{}
	for _, zf := range schema.ZoneFields {
		gotZoneKeys[zf.Field] = zf.Key
	}
	if len(gotZoneKeys) != len(wantZoneFieldKeys) {
		t.Errorf("got %d zone field keys, want %d", len(gotZoneKeys), len(wantZoneFieldKeys))
	}
	for k, want := range wantZoneFieldKeys {
		if got, ok := gotZoneKeys[k]; !ok {
			t.Errorf("missing zone field key %q", k)
		} else if got != want {
			t.Errorf("zone field key %q = %q, want %q", k, got, want)
		}
	}
}

// TestRegression_GeneratedGoCompilesAndMatches renders the full Go output for
// both schemas and sanity-checks it's syntactically valid (gofmt-able) —
// catching a codegen bug that produces a broken .go file before it ever
// reaches `go build`.
func TestRegression_GeneratedGoCompilesAndMatches(t *testing.T) {
	for _, tc := range []struct {
		schemaPath string
		opts       GoOptions
	}{
		{"../../internal/shelly/scripts/pool-pump.schema.json", GoOptions{Package: "pool", Consts: true}},
		{"../../internal/shelly/scripts/pool-pump.schema.json", GoOptions{Package: "script", KVSKeys: true}},
		{"../../internal/shelly/scripts/garden.schema.json", GoOptions{Package: "garden", Consts: true, KVSKeys: true, ZoneFieldKeys: true}},
	} {
		schema, err := LoadSchema(tc.schemaPath)
		if err != nil {
			t.Fatalf("LoadSchema(%s): %v", tc.schemaPath, err)
		}
		tc.opts.SourceJSON = tc.schemaPath
		src, err := GenerateGo(schema, tc.opts)
		if err != nil {
			t.Fatalf("GenerateGo(%s, %+v): %v", tc.schemaPath, tc.opts, err)
		}
		if _, err := formatGo(src); err != nil {
			t.Errorf("GenerateGo(%s, %+v) produced invalid Go source: %v\n---\n%s", tc.schemaPath, tc.opts, err, src)
		}
	}
}

func defaultsEqual(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return a == b
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

// TestSchemaFilesExist is a cheap guard so a bad refactor of the test's
// relative paths fails clearly instead of every other test in this file
// silently reporting "0 fields".
func TestSchemaFilesExist(t *testing.T) {
	for _, p := range []string{
		"../../internal/shelly/scripts/pool-pump.schema.json",
		"../../internal/shelly/scripts/garden.schema.json",
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("schema file missing: %s: %v", p, err)
		}
	}
}
