package ui

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

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

func card(connected bool, st *myhome.VacuumStatus, mod func(*vacuumCard)) vacuumPanel {
	c := vacuumCard{VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: connected, Status: st}}
	if st != nil {
		c.PhaseText = myhome.VacuumPhaseText(st.Phase)
		c.CycleText = vacuumCycleText(st.Cycle)
		c.ErrorText = vacuumErrorText(st.Error)
		c.NotReady = vacuumNotReadyText(st.NotReady)
		c.Updated = vacuumUpdatedText(st.UpdatedAt, time.Now())
	}
	if mod != nil {
		mod(&c)
	}
	return vacuumPanel{Cards: []vacuumCard{c}}
}

// buttonTag returns the opening tag of the button labelled label.
func buttonTag(t *testing.T, out, label string) string {
	t.Helper()
	i := strings.Index(out, ">"+label+"</button>")
	if i < 0 {
		t.Fatalf("no %q button", label)
	}
	return out[strings.LastIndex(out[:i], "<button"):i]
}

func TestVacuumPanel_Docked(t *testing.T) {
	out := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Battery: 100, BinPresent: true, Phase: "charge", Cycle: "none",
		MissionMinutes: 0, UpdatedAt: time.Now()}, nil))
	for _, want := range []string{"Edmond", "connected", "docked, charging", "battery 100%", "bin ok", "Last update:", "just now"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, on := range []string{"Clean all", "Dock", "Find", "Empty bin"} {
		if strings.Contains(buttonTag(t, out, on), "disabled") {
			t.Errorf("%s disabled while docked", on)
		}
	}
	for _, off := range []string{"Pause", "Resume", "Stop"} {
		if !strings.Contains(buttonTag(t, out, off), "disabled") {
			t.Errorf("%s enabled while docked", off)
		}
	}
	if strings.Contains(out, "Clean selected rooms") {
		t.Error("room checklist belongs to the room view (#684)")
	}
}

func TestVacuumPanel_CleaningAndPaused(t *testing.T) {
	running := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Battery: 15, BinPresent: true, BinFull: true,
		Phase: "run", Cycle: "clean", MissionMinutes: 12}, func(c *vacuumCard) {
		c.Running = true
		c.Targets = []string{"Kitchen", "region 4"}
	}))
	for _, want := range []string{"- cleaning cycle", "bin full", "is-danger", "mission 12 min", "Cleaning: Kitchen, region 4"} {
		if !strings.Contains(running, want) {
			t.Errorf("running: missing %q", want)
		}
	}
	for _, on := range []string{"Pause", "Stop", "Dock", "Find"} {
		if strings.Contains(buttonTag(t, running, on), "disabled") {
			t.Errorf("running: %s disabled", on)
		}
	}
	for _, off := range []string{"Clean all", "Resume", "Empty bin"} {
		if !strings.Contains(buttonTag(t, running, off), "disabled") {
			t.Errorf("running: %s enabled", off)
		}
	}

	paused := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Battery: 60, Phase: "stop", Cycle: "clean"},
		func(c *vacuumCard) { c.Paused = true; c.Error = "resume failed: not acknowledged" }))
	for _, want := range []string{"bin missing", "resume failed: not acknowledged"} {
		if !strings.Contains(paused, want) {
			t.Errorf("paused: missing %q", want)
		}
	}
	if strings.Contains(buttonTag(t, paused, "Resume"), "disabled") {
		t.Error("paused: Resume disabled")
	}
}

func TestVacuumPanel_Offline(t *testing.T) {
	out := renderVacuumPanel(t, card(false, &myhome.VacuumStatus{Battery: 80, BinPresent: true, Phase: "run", Cycle: "clean"},
		func(c *vacuumCard) { c.Running = true }))
	for _, want := range []string{"offline", "not connected", "Controls are disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, b := range []string{"Clean all", "Pause", "Resume", "Stop", "Dock", "Find", "Empty bin"} {
		if !strings.Contains(buttonTag(t, out, b), "disabled") {
			t.Errorf("%s enabled while offline", b)
		}
	}
}

func TestVacuumErrorWords(t *testing.T) {
	if got := vacuumErrorText(0); got != "" {
		t.Errorf("no error = %q", got)
	}
	if got := vacuumErrorText(17); got != "code 17" {
		t.Errorf("unknown code = %q, want %q", got, "code 17")
	}
	if got := vacuumErrorText(224); !strings.Contains(got, "aborted at once") {
		t.Errorf("224 = %q", got)
	}
	if got := vacuumNotReadyText(39); got != "code 39" {
		t.Errorf("not ready = %q", got)
	}
	out := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Phase: "stop", Error: 17, NotReady: 39, BinPresent: true}, nil))
	for _, want := range []string{"Error: code 17", "Not ready to start: code 39"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestVacuumCycleAndUpdated(t *testing.T) {
	if vacuumCycleText("none") != "" || vacuumCycleText("clean") != "cleaning cycle" || vacuumCycleText("zz") != "cycle zz" {
		t.Error("cycle words")
	}
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.Local)
	if got := vacuumUpdatedText(time.Time{}, now); got != "never" {
		t.Errorf("zero time = %q", got)
	}
	if got := vacuumUpdatedText(now.Add(-5*time.Minute), now); !strings.HasSuffix(got, "(5 min ago)") {
		t.Errorf("5 min = %q", got)
	}
}

func TestVacuumPanel_ConfirmMarkup(t *testing.T) {
	out := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Phase: "charge", BinPresent: true}, nil))
	// Clean all and Stop only open the confirm; no request is bound to them directly
	for _, b := range []string{"Clean all", "Stop"} {
		tag := buttonTag(t, out, b)
		if strings.Contains(tag, "hx-post") || !strings.Contains(tag, "@click=") {
			t.Errorf("%s must only open the confirm: %s", b, tag)
		}
	}
	for _, want := range []string{"Start a whole-home clean?", "Stop the mission?", ">Cancel</button>",
		`data-vac-confirm="start"`, `data-vac-confirm="stop"`, `:data-confirming`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "hx-confirm") || strings.Contains(out, "confirm(") {
		t.Error("native confirm used")
	}
	// the request fires from the confirm button, and Dock/Find do not ask
	if !strings.Contains(out, `"command":"start"}' hx-target`) {
		t.Error("start request missing")
	}
	if strings.Contains(buttonTag(t, out, "Dock"), "@click") {
		t.Error("Dock asks for confirmation")
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

func TestTargetNames(t *testing.T) {
	rooms := []myhome.VacuumRegion{{ID: "21", Name: "Cuisine"}, {ID: "8", Name: "Chambre"}, {ID: "5"}}
	got := targetNames([]string{"21", "5", "99"}, rooms)
	want := []string{"Cuisine", "region 5", "region 99"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("targetNames = %v, want %v", got, want)
	}
	if n := targetNames(nil, rooms); len(n) != 0 {
		t.Errorf("whole-home clean has targets: %v", n)
	}
}

func TestVacuumPanel_ShowsTargetRooms(t *testing.T) {
	out := renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
			Status: &myhome.VacuumStatus{Battery: 90, BinPresent: true, Phase: "run", Cycle: "clean"}},
		PhaseText: "cleaning", Running: true, Targets: []string{"Cuisine", "Couloir"},
	}}})
	if !strings.Contains(out, "Cleaning: Cuisine, Couloir") {
		t.Errorf("target rooms not shown:\n%s", out)
	}
	out = renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
			Status: &myhome.VacuumStatus{Battery: 90, BinPresent: true, Phase: "run", Cycle: "clean"}},
		PhaseText: "cleaning", Running: true,
	}}})
	if strings.Contains(out, "Cleaning:") {
		t.Error("whole-home clean shows a room list")
	}
}
