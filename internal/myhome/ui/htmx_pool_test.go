package ui

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestCardTemplateFuncs_IsActive(t *testing.T) {
	fn := cardTemplateFuncs()["isActive"].(func(*bool) bool)

	trueVal, falseVal := true, false
	if fn(nil) {
		t.Error("isActive(nil) = true, want false")
	}
	if fn(&falseVal) {
		t.Error("isActive(&false) = true, want false")
	}
	if !fn(&trueVal) {
		t.Error("isActive(&true) = false, want true")
	}
}

func TestCardTemplateFuncs_TurnoverText(t *testing.T) {
	fn := cardTemplateFuncs()["turnoverText"].(func(*float64, *float64) string)

	if got := fn(nil, nil); got != "" {
		t.Errorf("turnoverText(nil, nil) = %q, want empty", got)
	}
	achieved, target := 3.2, 5.0
	if got := fn(&achieved, nil); got != "" {
		t.Errorf("turnoverText(achieved, nil) = %q, want empty", got)
	}
	if got, want := fn(&achieved, &target), "3.2/5.0 x/day"; got != want {
		t.Errorf("turnoverText = %q, want %q", got, want)
	}
}

// TestDeviceCardTemplate_PoolTags verifies the pool water-supply/turnover
// tags render for a pool-pump device and are absent for an ordinary device,
// guarding against html/template's non-dereferencing behavior for pointer
// fields in {{if}}/{{printf}} (see cardTemplateFuncs' doc comment).
func TestDeviceCardTemplate_PoolTags(t *testing.T) {
	tmpl := template.Must(template.New("device-card").Funcs(cardTemplateFuncs()).Parse(deviceCardTemplate))

	achieved, target := 3.2, 5.0
	active := true
	poolView := DeviceView{
		Id:                "pool-device",
		Name:              "Pool Pump",
		IsPoolPump:        true,
		WaterSupplyActive: &active,
		TurnoverAchieved:  &achieved,
		TurnoverTarget:    &target,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, poolView); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Paused", "3.2/5.0 x/day"} {
		if !strings.Contains(out, want) {
			t.Errorf("device card missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0x") {
		t.Errorf("device card leaked a pointer address (dereference bug):\n%s", out)
	}

	buf.Reset()
	ordinaryView := DeviceView{Id: "other-device", Name: "Other"}
	if err := tmpl.Execute(&buf, ordinaryView); err != nil {
		t.Fatalf("execute ordinary device: %v", err)
	}
	if out := buf.String(); strings.Contains(out, "water-supply-") || strings.Contains(out, "turnover-") {
		t.Errorf("non-pool device card unexpectedly rendered pool tags:\n%s", out)
	}
}
