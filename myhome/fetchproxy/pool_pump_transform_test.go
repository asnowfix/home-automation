package fetchproxy

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// poolPumpScriptPath is internal/shelly/scripts/pool-pump.js, relative to
// this package. #465's device half embeds its Open-Meteo reduction
// (FORECAST_TRANSFORM) as a JS string literal built by concatenation, so it
// can be extracted and run through the real Evaluate() the daemon uses —
// proving the string the device actually publishes both compiles and
// produces the fields pool-pump.js expects, rather than trusting a hand
// copy that could silently drift from the shipped source.
const poolPumpScriptPath = "../../internal/shelly/scripts/pool-pump.js"

// forecastTransformSegment matches one single-quoted JS string literal
// segment of pool-pump.js's string-concatenation-built FORECAST_TRANSFORM.
var forecastTransformSegment = regexp.MustCompile(`'([^']*)'`)

// extractForecastTransform re-derives the JS function text pool-pump.js
// assigns to FORECAST_TRANSFORM by concatenating every single-quoted string
// literal between "var FORECAST_TRANSFORM =" and the statement-terminating
// ";" — exactly what the Espruino interpreter does at load time for a plain
// string concatenation. Fails the test (not silently skips) if the marker or
// the closing ";" cannot be found, so a rename or reformat in pool-pump.js
// is caught here rather than silently testing nothing.
func extractForecastTransform(t *testing.T, src string) string {
	t.Helper()

	start := strings.Index(src, "var FORECAST_TRANSFORM =")
	if start < 0 {
		t.Fatal("pool-pump.js: could not find \"var FORECAST_TRANSFORM =\"")
	}
	rest := src[start:]

	end := strings.Index(rest, "';\n")
	if end < 0 {
		t.Fatal("pool-pump.js: could not find FORECAST_TRANSFORM's closing \"';\"")
	}
	// Include the char at `end` so the final segment's closing quote is in
	// range for the regexp below.
	block := rest[:end+2]

	matches := forecastTransformSegment.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		t.Fatal("pool-pump.js: FORECAST_TRANSFORM block contained no quoted segments")
	}

	var b strings.Builder
	for _, m := range matches {
		b.WriteString(m[1])
	}
	return b.String()
}

func readForecastTransform(t *testing.T) string {
	t.Helper()
	buf, err := os.ReadFile(poolPumpScriptPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", poolPumpScriptPath, err)
	}
	return extractForecastTransform(t, string(buf))
}

// TestPoolPumpForecastTransform_HappyPath runs pool-pump.js's actual
// FORECAST_TRANSFORM (extracted from the .js source, not a hand copy)
// through the real Evaluate() against an Open-Meteo-shaped body, and checks
// every field the device's onForecastResult() reads.
func TestPoolPumpForecastTransform_HappyPath(t *testing.T) {
	transform := readForecastTransform(t)

	body := []byte(`{
		"hourly": {"temperature_2m": [10,11,12,13,14,15,16,17,18,19,20,21,32,24,23,22,21,20,19,18,17,16,15,14]},
		"daily": {"sunrise": ["2026-08-07T06:15"], "sunset": ["2026-08-07T20:45"]}
	}`)

	out, err := Evaluate(transform, body, DefaultLimits())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	var got struct {
		MaxTempC *float64 `json:"max_temp_c"`
		PeakHour *int     `json:"peak_hour"`
		SunriseH *float64 `json:"sunrise_h"`
		SunsetH  *float64 `json:"sunset_h"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal transform output %s: %v", out, err)
	}

	if got.MaxTempC == nil || *got.MaxTempC != 32 {
		t.Errorf("max_temp_c: got %v, want 32", got.MaxTempC)
	}
	if got.PeakHour == nil || *got.PeakHour != 12 {
		t.Errorf("peak_hour: got %v, want 12 (index of 32)", got.PeakHour)
	}
	if got.SunriseH == nil || *got.SunriseH != 6.25 {
		t.Errorf("sunrise_h: got %v, want 6.25 (06:15)", got.SunriseH)
	}
	if got.SunsetH == nil || *got.SunsetH != 20.75 {
		t.Errorf("sunset_h: got %v, want 20.75 (20:45)", got.SunsetH)
	}

	// Output must respect #465's "few hundred bytes" hard cap — this is the
	// entire point of the migration, so pin it down rather than trust it.
	if len(out) > DefaultMaxOutputBytes {
		t.Errorf("transform output %d bytes exceeds DefaultMaxOutputBytes (%d)", len(out), DefaultMaxOutputBytes)
	}
}

// TestPoolPumpForecastTransform_MalformedBody proves the transform degrades
// the same way the old on-device onForecast() did (#465 "never publish a bad
// payload" applies to the daemon; the device-side degraded path is that this
// simply returns nulls, which onForecastResult() then treats as "no data").
func TestPoolPumpForecastTransform_MalformedBody(t *testing.T) {
	transform := readForecastTransform(t)

	out, err := Evaluate(transform, []byte("not json"), DefaultLimits())
	if err != nil {
		t.Fatalf("Evaluate should not error on a malformed body (transform itself handles it): %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal transform output %s: %v", out, err)
	}
	for _, field := range []string{"max_temp_c", "peak_hour", "sunrise_h", "sunset_h"} {
		if v, ok := got[field]; !ok || v != nil {
			t.Errorf("field %q: got %v, want null on a malformed body", field, v)
		}
	}
}

// TestPoolPumpForecastTransform_EmptyHourly proves an Open-Meteo response
// with no forecast_days/hourly data (the shape onForecast() used to call
// "Invalid forecast structure" and skip) still yields a well-formed object
// with null max_temp_c/peak_hour, not an error — same degraded contract.
func TestPoolPumpForecastTransform_EmptyHourly(t *testing.T) {
	transform := readForecastTransform(t)

	out, err := Evaluate(transform, []byte(`{}`), DefaultLimits())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal transform output %s: %v", out, err)
	}
	if got["max_temp_c"] != nil {
		t.Errorf("max_temp_c: got %v, want null", got["max_temp_c"])
	}
	if got["peak_hour"] != nil {
		t.Errorf("peak_hour: got %v, want null", got["peak_hour"])
	}
}
