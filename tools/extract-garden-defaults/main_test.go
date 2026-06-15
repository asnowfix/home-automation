package main

import (
	"testing"
)

const minimalGardenJS = `
var CONFIG_SCHEMA = {
  earliestStartHour: { description: "x", key: "earliest-start", default: 3, type: "number" },
  lunchStart:        { description: "x", key: "lunch-start",    default: 12, type: "number" },
  lunchEnd:          { description: "x", key: "lunch-end",      default: 14, type: "number" },
  eveningStart:      { description: "x", key: "evening-start",  default: 19, type: "number" },
  eveningEnd:        { description: "x", key: "evening-end",    default: 23.5, type: "number" },
  fallbackStartHour: { description: "x", key: "fallback-start", default: 5, type: "number" },
  frostCutoffC:      { description: "x", key: "frost-cutoff-c", default: 2, type: "number" },
  rainHoldoffMm:     { description: "x", key: "rain-holdoff-mm", default: 8, type: "number" },
  maxDeficitMm:      { description: "x", key: "max-deficit-mm",  default: 25, type: "number" }
};
`

func TestExtractDefaults(t *testing.T) {
	d, err := extractDefaults(minimalGardenJS)
	if err != nil {
		t.Fatalf("extractDefaults: %v", err)
	}

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"EarliestStartHour", float64(d.EarliestStartHour), 3},
		{"LunchStart", d.LunchStart, 12.0},
		{"LunchEnd", d.LunchEnd, 14.0},
		{"EveningStart", d.EveningStart, 19.0},
		{"EveningEnd", d.EveningEnd, 23.5},
		{"FallbackStartHour", float64(d.FallbackStartHour), 5},
		{"FrostCutoffC", d.FrostCutoffC, 2.0},
		{"RainHoldoffMm", d.RainHoldoffMm, 8.0},
		{"MaxDeficitMm", d.MaxDeficitMm, 25.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestExtractDefaults_MissingField(t *testing.T) {
	_, err := extractDefaults("var x = {};")
	if err == nil {
		t.Fatal("expected error for missing fields, got nil")
	}
}

func TestExtractIntDefault(t *testing.T) {
	content := `foo: { description: "x", default: 42, type: "number" }`
	v, err := extractIntDefault(content, "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Errorf("got %d, want 42", v)
	}
}

func TestExtractIntDefault_NotFound(t *testing.T) {
	_, err := extractIntDefault("nothing here", "foo")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestExtractFloatDefault(t *testing.T) {
	content := `bar: { description: "x", default: 3.14, type: "number" }`
	v, err := extractFloatDefault(content, "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 3.14 {
		t.Errorf("got %v, want 3.14", v)
	}
}

func TestExtractFloatDefault_NotFound(t *testing.T) {
	_, err := extractFloatDefault("nothing here", "bar")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}
