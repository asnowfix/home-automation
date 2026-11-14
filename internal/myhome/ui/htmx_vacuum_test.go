package ui

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"

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
	c.derive(time.Now())
	if mod != nil {
		mod(&c)
	}
	return vacuumPanel{Cards: []vacuumCard{c}}
}

var disabledAttr = regexp.MustCompile(`\sdisabled(\s|$)`)

func isDisabled(tag string) bool { return disabledAttr.MatchString(tag) }

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
		if isDisabled(buttonTag(t, out, on)) {
			t.Errorf("%s disabled while docked", on)
		}
	}
	for _, off := range []string{"Pause", "Resume", "Stop"} {
		if !isDisabled(buttonTag(t, out, off)) {
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
		c.Targets = []string{"Kitchen", "region 4"}
	}))
	for _, want := range []string{"- cleaning cycle", "bin full", "is-danger", "mission 12 min", "Cleaning: Kitchen, region 4"} {
		if !strings.Contains(running, want) {
			t.Errorf("running: missing %q", want)
		}
	}
	for _, on := range []string{"Pause", "Stop", "Dock", "Find"} {
		if isDisabled(buttonTag(t, running, on)) {
			t.Errorf("running: %s disabled", on)
		}
	}
	for _, off := range []string{"Clean all", "Resume", "Empty bin"} {
		if !isDisabled(buttonTag(t, running, off)) {
			t.Errorf("running: %s enabled", off)
		}
	}

	paused := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Battery: 60, Phase: "stop", Cycle: "clean"},
		func(c *vacuumCard) { c.Error = "resume failed: not acknowledged" }))
	for _, want := range []string{"bin missing", "resume failed: not acknowledged"} {
		if !strings.Contains(paused, want) {
			t.Errorf("paused: missing %q", want)
		}
	}
	if isDisabled(buttonTag(t, paused, "Resume")) {
		t.Error("paused: Resume disabled")
	}
}

func TestVacuumPanel_Offline(t *testing.T) {
	out := renderVacuumPanel(t, card(false, &myhome.VacuumStatus{Battery: 80, BinPresent: true, Phase: "run", Cycle: "clean"},
		nil))
	for _, want := range []string{"offline", "not connected", "Controls are disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, b := range []string{"Clean all", "Pause", "Resume", "Stop", "Dock", "Find", "Empty bin"} {
		if !isDisabled(buttonTag(t, out, b)) {
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

func TestDeriveVacuumState(t *testing.T) {
	tests := []struct {
		name      string
		connected bool
		st        *myhome.VacuumStatus
		want      vacuumState
		ctl       vacuumControls
	}{
		{"offline", false, &myhome.VacuumStatus{Phase: "run", Cycle: "clean"}, vacOffline, vacuumControls{}},
		{"no status", true, nil, vacIdle, controlsFor(vacIdle)},
		{"docked", true, &myhome.VacuumStatus{Phase: "charge", Cycle: "none"}, vacIdle, vacuumControls{CleanAll: true, Dock: true, Find: true, Evac: true}},
		{"empty cycle is idle", true, &myhome.VacuumStatus{Phase: "stop", Cycle: ""}, vacIdle, controlsFor(vacIdle)},
		{"stopped none is idle", true, &myhome.VacuumStatus{Phase: "stop", Cycle: "none"}, vacIdle, controlsFor(vacIdle)},
		{"running", true, &myhome.VacuumStatus{Phase: "run", Cycle: "clean"}, vacRunning, vuc(false, true, false, true)},
		{"returning", true, &myhome.VacuumStatus{Phase: "hmUsrDock", Cycle: "dock"}, vacRunning, vuc(false, true, false, true)},
		{"mid mission recharge", true, &myhome.VacuumStatus{Phase: "hmMidMsn", Cycle: "clean"}, vacRunning, vuc(false, true, false, true)},
		{"paused", true, &myhome.VacuumStatus{Phase: "stop", Cycle: "clean"}, vacPaused, vuc(false, false, true, true)},
		{"stuck", true, &myhome.VacuumStatus{Phase: "stuck", Cycle: "clean"}, vacStuck, vuc(false, false, false, true)},
		{"evac", true, &myhome.VacuumStatus{Phase: "evac", Cycle: "evac"}, vacEvac, vuc(false, false, false, false)},
	}
	for _, tc := range tests {
		got := deriveVacuumState(tc.connected, tc.st)
		if got != tc.want {
			t.Errorf("%s: state %q, want %q", tc.name, got, tc.want)
			continue
		}
		ctl := controlsFor(got)
		if ctl != tc.ctl {
			t.Errorf("%s: controls %+v, want %+v", tc.name, ctl, tc.ctl)
		}
		if got != vacOffline && !ctl.Find {
			t.Errorf("%s: Find must stay available while online", tc.name)
		}
	}
}

// vuc builds the controls of a non-idle state: Dock and Find are always on.
func vuc(cleanAll, pause, resume, stop bool) vacuumControls {
	return vacuumControls{CleanAll: cleanAll, Pause: pause, Resume: resume, Stop: stop, Dock: true, Find: true}
}

func TestVacuumErrorTextNotDoubled(t *testing.T) {
	got := vacuumErrorText(224)
	if strings.Contains(got, "error 224") || !strings.HasPrefix(got, "code 224: ") {
		t.Errorf("224 = %q", got)
	}
	out := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Phase: "stop", Error: 224, BinPresent: true}, nil))
	if strings.Contains(out, "Error: error") {
		t.Error("doubled Error: error")
	}
}

func TestVacuumPanel_StuckAndEvac(t *testing.T) {
	stuck := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Phase: "stuck", Cycle: "clean", BinPresent: true}, nil))
	if isDisabled(buttonTag(t, stuck, "Stop")) || !isDisabled(buttonTag(t, stuck, "Clean all")) ||
		!isDisabled(buttonTag(t, stuck, "Empty bin")) {
		t.Error("stuck: Stop must be on, Clean all and Empty bin off")
	}
	evac := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Phase: "evac", Cycle: "evac", BinPresent: true}, nil))
	if !isDisabled(buttonTag(t, evac, "Clean all")) || !isDisabled(buttonTag(t, evac, "Empty bin")) {
		t.Error("evac: Clean all and Empty bin must be off")
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
	if got := vacuumUpdatedText(now.Add(-3*time.Hour-10*time.Minute), now); !strings.HasSuffix(got, "(3 h ago, may be out of date)") {
		t.Errorf("3 h = %q", got)
	}
	if got := vacuumUpdatedText(now.Add(-50*time.Hour), now); !strings.HasSuffix(got, "(2 d ago, may be out of date)") {
		t.Errorf("2 d = %q", got)
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
	if strings.Contains(out, "confirm(") || strings.Contains(out, "alert(") {
		t.Error("native dialog used")
	}
	// the confirm cancels itself, so it cannot hold the poll open indefinitely
	if !strings.Contains(out, "20000") || !strings.Contains(out, "close()") {
		t.Error("confirm has no auto-cancel")
	}
	// a double tap must not post twice
	if strings.Count(out, `hx-disabled-elt="this"`) < 7 {
		t.Error("action buttons are not disabled while their request is in flight")
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
		PhaseText: "cleaning", Targets: []string{"Cuisine", "Couloir"},
	}}})
	if !strings.Contains(out, "Cleaning: Cuisine, Couloir") {
		t.Errorf("target rooms not shown:\n%s", out)
	}
	out = renderVacuumPanel(t, vacuumPanel{Cards: []vacuumCard{{
		VacuumRobot: myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
			Status: &myhome.VacuumStatus{Battery: 90, BinPresent: true, Phase: "run", Cycle: "clean"}},
		PhaseText: "cleaning",
	}}})
	if strings.Contains(out, "Cleaning:") {
		t.Error("whole-home clean shows a room list")
	}
}

// stubVacuum registers vacuum.list and vacuum.command handlers for one test and
// restores the previous handlers afterwards (the registry is package-level state, so
// no t.Parallel here). It returns the commands the handler received.
func stubVacuum(t *testing.T, robot myhome.VacuumRobot) *[]string {
	t.Helper()
	var sent []string
	for _, verb := range []myhome.Verb{myhome.VacuumList, myhome.VacuumRooms, myhome.VacuumCommand} {
		old, err := myhome.Methods(verb)
		t.Cleanup(func() {
			if err == nil {
				myhome.RegisterMethodHandler(verb, old.ActionE)
				return
			}
			myhome.RegisterMethodHandler(verb, func(context.Context, any) (any, error) {
				return nil, fmt.Errorf("vacuum service not registered")
			})
		})
	}
	myhome.RegisterMethodHandler(myhome.VacuumList, func(context.Context, any) (any, error) {
		return &myhome.VacuumListResult{Robots: []myhome.VacuumRobot{robot}}, nil
	})
	myhome.RegisterMethodHandler(myhome.VacuumRooms, func(context.Context, any) (any, error) {
		return &myhome.VacuumRoomsResult{}, nil
	})
	myhome.RegisterMethodHandler(myhome.VacuumCommand, func(_ context.Context, in any) (any, error) {
		sent = append(sent, in.(*myhome.VacuumCommandParams).Command)
		return &myhome.VacuumOKResult{OK: true}, nil
	})
	return &sent
}

func postVacuum(t *testing.T, command string) string {
	t.Helper()
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard()}
	req := httptest.NewRequest(http.MethodPost, "/htmx/vacuum/action", strings.NewReader("robot=B1&command="+command))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.VacuumAction(rec, req)
	return rec.Body.String()
}

func TestVacuumAction_RechecksStateForStartAndStop(t *testing.T) {
	running := myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
		Status: &myhome.VacuumStatus{Phase: "run", Cycle: "clean", BinPresent: true}}
	sent := stubVacuum(t, running)
	if out := postVacuum(t, "start"); !strings.Contains(out, "nothing was started") || len(*sent) != 0 {
		t.Errorf("start while running: sent %v, page %q", *sent, out)
	}
	if postVacuum(t, "stop"); len(*sent) != 1 || (*sent)[0] != "stop" {
		t.Errorf("stop while running: sent %v", *sent)
	}

	idle := myhome.VacuumRobot{ID: "B1", Name: "Edmond", Connected: true,
		Status: &myhome.VacuumStatus{Phase: "charge", Cycle: "none", BinPresent: true}}
	sent = stubVacuum(t, idle)
	if out := postVacuum(t, "stop"); !strings.Contains(out, "not on a mission") || len(*sent) != 0 {
		t.Errorf("stop while idle: sent %v, page %q", *sent, out)
	}
	if postVacuum(t, "start"); len(*sent) != 1 || (*sent)[0] != "start" {
		t.Errorf("start while idle: sent %v", *sent)
	}

	idle.Connected = false
	sent = stubVacuum(t, idle)
	if out := postVacuum(t, "start"); !strings.Contains(out, "offline") || len(*sent) != 0 {
		t.Errorf("start while offline: sent %v, page %q", *sent, out)
	}
	if postVacuum(t, "find"); len(*sent) != 1 {
		t.Errorf("find is not re-checked, sent %v", *sent)
	}
}

// The vacuum poll runs only while the section is shown, the daemon is reachable
// and no confirm is open; the shell's beforeRequest guard refuses non-GET htmx
// requests while offline, which covers the confirm bar's POSTs.
func TestVacuumPollFilterAndOfflineGuard(t *testing.T) {
	html := get(testMux(), "/").Body.String()
	for _, want := range []string{"kiosk.visible('vacuum')", "kiosk.online", "#vacuum-container [data-confirming]"} {
		if !strings.Contains(html, want) {
			t.Errorf("vacuum poll filter lacks %q", want)
		}
	}
	js := string(staticAsset(t, "/static/myhome.js"))
	if !strings.Contains(js, "kiosk.blocks(") {
		t.Error("shell no longer refuses htmx writes while offline")
	}
}
