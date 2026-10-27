package ui

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

func renderVacuumPanel(t *testing.T, p vacuumPanel) string {
	t.Helper()
	tmpl := template.Must(template.New("vacuum-panel").Parse(vacuumPanelTemplate))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return buf.String()
}

func TestVacuumPanel_Docked(t *testing.T) {
	out := renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
			Status: &myhome.VacuumStatus{Battery: 100, BinPresent: true, Phase: "charge", Cycle: "none"}},
		PhaseText: myhome.VacuumPhaseText("charge"),
		Rooms:     []myhome.VacuumRegion{{ID: "11", Type: "rid", Name: "Kitchen"}, {ID: "4", Type: "rid"}},
	}}})
	for _, want := range []string{"Edmond", "docked, charging", "battery 100%", "bin ok", "Clean all", "Dock",
		"Kitchen", "region 4", "Clean selected rooms", `"robot":"B1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, ">Pause<") || strings.Contains(out, ">Resume<") {
		t.Error("docked robot shows mission controls")
	}
}

func TestVacuumPanel_CleaningAndPaused(t *testing.T) {
	running := renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
			Status: &myhome.VacuumStatus{Battery: 15, BinPresent: true, BinFull: true, Phase: "run", Cycle: "clean", MissionMinutes: 12}},
		PhaseText: "cleaning", Running: true,
	}}})
	for _, want := range []string{">Pause<", ">Stop<", "bin full", "is-danger", "12 min"} {
		if !strings.Contains(running, want) {
			t.Errorf("running: missing %q", want)
		}
	}
	if strings.Contains(running, "Clean all") {
		t.Error("running: offers Clean all")
	}

	paused := renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond",
			Status: &myhome.VacuumStatus{Battery: 60, Phase: "stop", Cycle: "clean", Error: 17}},
		PhaseText: "stopped", Paused: true, Error: "resume failed: not acknowledged",
	}}})
	for _, want := range []string{">Resume<", "offline", "error 17", "bin missing", "resume failed: not acknowledged"} {
		if !strings.Contains(paused, want) {
			t.Errorf("paused: missing %q", want)
		}
	}
}

func TestVacuumPanel_NoStatusYet(t *testing.T) {
	out := renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Host: "192.0.2.50"},
	}}})
	if !strings.Contains(out, "No status received yet (last seen at 192.0.2.50)") {
		t.Errorf("unexpected output:\n%s", out)
	}
}
