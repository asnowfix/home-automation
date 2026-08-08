package main

import "testing"

func TestCamelToSnake(t *testing.T) {
	// Every one of these pairs is taken verbatim from the pre-#439
	// hand-maintained PoolKVSKeys map (internal/myhome/shelly/script/pool.go)
	// and gardenKVSKeys map (myhome/ctl/garden/setup.go). If camelToSnake
	// stops reproducing them, generated Go map keys will silently rename,
	// which is exactly the kind of behaviour drift #439 exists to prevent.
	cases := map[string]string{
		"enableLogging":        "enable_logging",
		"mqttTopicPrefix":      "mqtt_topic_prefix",
		"preferredDeviceId":    "preferred_device_id",
		"pro3DeviceId":         "pro3_device_id",
		"pro1DeviceId":         "pro1_device_id",
		"preferredSpeed":       "preferred_speed",
		"ecoSpeed":             "eco_speed",
		"midSpeed":             "mid_speed",
		"highSpeed":            "high_speed",
		"nightRunDurationMs":   "night_run_duration_ms",
		"graceDelayMs":         "grace_delay_ms",
		"temperatureThreshold": "temperature_threshold",
		"poolVolume":           "pool_volume",
		"turnover":             "turnover",
		"maxFlowRate":          "max_flow_rate",
		"maxRpm":               "max_rpm",
		"ecoRpm":               "eco_rpm",
		"midRpm":               "mid_rpm",
		"highRpm":              "high_rpm",
		"maxTemp":              "max_temp",
		"solarEnabled":         "solar_enabled",
		"solarStartThresholdW": "solar_start_threshold_w",
		"solarStopThresholdW":  "solar_stop_threshold_w",
		"solarStartDelayMs":    "solar_start_delay_ms",
		"solarStopDelayMs":     "solar_stop_delay_ms",
		"solarMinTurnover":     "solar_min_turnover",
		"solarMaxTurnover":     "solar_max_turnover",
		"solarStaleMs":         "solar_stale_ms",
		"earliestStartHour":    "earliest_start_hour",
		"lunchStart":           "lunch_start",
		"lunchEnd":             "lunch_end",
		"eveningStart":         "evening_start",
		"eveningEnd":           "evening_end",
		"fallbackStartHour":    "fallback_start_hour",
		"frostCutoffC":         "frost_cutoff_c",
		"rainHoldoffMm":        "rain_holdoff_mm",
		"maxDeficitMm":         "max_deficit_mm",
	}
	for in, want := range cases {
		if got := camelToSnake(in); got != want {
			t.Errorf("camelToSnake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapitalize(t *testing.T) {
	cases := map[string]string{
		"ecoSpeed":             "EcoSpeed",
		"solarStartThresholdW": "SolarStartThresholdW",
		"":                     "",
	}
	for in, want := range cases {
		if got := capitalize(in); got != want {
			t.Errorf("capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSchemaValidate_KVSKeyTooLong(t *testing.T) {
	s := &Schema{
		Script:     "x.js",
		KVSPrefix:  "script/pool-pump/", // 18 chars
		KVSKeysVar: "XKeys",
		Fields: []Field{
			{Name: "x", Key: "this-suffix-is-deliberately-way-too-long-to-fit", Default: 1.0, Type: "number"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for over-long KVS key, got nil")
	}
}

func TestSchemaValidate_DuplicateFieldName(t *testing.T) {
	s := &Schema{
		Script:     "x.js",
		KVSPrefix:  "script/x/",
		KVSKeysVar: "XKeys",
		Fields: []Field{
			{Name: "dup", Key: "a", Default: 1.0, Type: "number"},
			{Name: "dup", Key: "b", Default: 2.0, Type: "number"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for duplicate field name, got nil")
	}
}

func TestSchemaValidate_ZoneFieldKVSKeyTooLong(t *testing.T) {
	s := &Schema{
		Script:           "garden.js",
		KVSPrefix:        "script/garden/", // 14 chars
		KVSKeysVar:       "GardenKVSKeys",
		ZoneFieldKeysVar: "ZoneFieldKeys",
		ZoneFields: []ZoneField{
			{Field: "x", Key: "this-zone-suffix-is-also-deliberately-too-long", Type: "string"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for over-long zone KVS key, got nil")
	}
}

func TestSchemaValidate_UnknownType(t *testing.T) {
	s := &Schema{
		Script:     "x.js",
		KVSPrefix:  "script/x/",
		KVSKeysVar: "XKeys",
		Fields: []Field{
			{Name: "x", Key: "a", Default: 1.0, Type: "array"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for unknown type, got nil")
	}
}

func TestLoadSchema_PoolPump(t *testing.T) {
	s, err := LoadSchema("../../internal/shelly/scripts/pool-pump.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	if len(s.Fields) != 28 {
		t.Errorf("pool-pump.schema.json: got %d fields, want 28", len(s.Fields))
	}
}

func TestLoadSchema_Garden(t *testing.T) {
	s, err := LoadSchema("../../internal/shelly/scripts/garden.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	if len(s.Fields) != 11 {
		t.Errorf("garden.schema.json: got %d global fields, want 11", len(s.Fields))
	}
	if len(s.ZoneFields) != 9 {
		t.Errorf("garden.schema.json: got %d zone fields, want 9", len(s.ZoneFields))
	}
	if total := len(s.Fields) + len(s.ZoneFields); total != 20 {
		t.Errorf("garden.schema.json: got %d total schema entries, want 20 (matches issue #439's count)", total)
	}
}
