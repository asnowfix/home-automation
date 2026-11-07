package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestCardTemplateFuncs_IsActive(t *testing.T) {
	fn := rowTemplateFuncs()["isActive"].(func(*bool) bool)

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
	fn := rowTemplateFuncs()["turnoverText"].(func(*float64, *float64) string)

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

// renderDevice renders one device through the shared row templates.
func renderDevice(t *testing.T, v DeviceView) string {
	t.Helper()
	var buf bytes.Buffer
	if err := deviceRowTmpl.ExecuteTemplate(&buf, "device", v); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return buf.String()
}

// TestDeviceRow_PoolTags verifies the pool water-supply/turnover pills render
// for a pool-pump device and are absent for an ordinary device, guarding
// against html/template's non-dereferencing behavior for pointer fields in
// {{if}}/{{printf}} (see rowTemplateFuncs' doc comment).
func TestDeviceRow_PoolTags(t *testing.T) {
	achieved, target := 3.2, 5.0
	active := true
	out := renderDevice(t, DeviceView{
		Id: "pool-device", Name: "Pool Pump", IsPoolPump: true,
		WaterSupplyActive: &active, TurnoverAchieved: &achieved, TurnoverTarget: &target,
	})
	for _, want := range []string{"Water supply paused", "3.2/5.0 x/day"} {
		if !strings.Contains(out, want) {
			t.Errorf("device row missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0x") {
		t.Errorf("device row leaked a pointer address (dereference bug):\n%s", out)
	}
	if out := renderDevice(t, DeviceView{Id: "other-device", Name: "Other"}); strings.Contains(out, "water-supply-") || strings.Contains(out, "turnover-") {
		t.Errorf("non-pool device unexpectedly rendered pool pills:\n%s", out)
	}
}

// TestDeviceRow_NoWebUILink verifies the row opens no new browser window: the
// kiosk has no browser chrome to come back from (#683).
func TestDeviceRow_NoWebUILink(t *testing.T) {
	out := renderDevice(t, DeviceView{Id: "shellyplus1-abc", Name: "Switch", LinkToken: "shellyplus1-abc", HasWebUI: true})
	if strings.Contains(out, "/devices/") || strings.Contains(out, "_blank") {
		t.Errorf("device row links out of the shell:\n%s", out)
	}
}
