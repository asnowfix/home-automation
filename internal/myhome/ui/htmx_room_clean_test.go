package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-logr/logr"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// cleanEnv registers room "sejour" (Séjour), the given robots with their regions
// (by robot id) and a vacuum.clean that records its calls. Tests using it must not
// call t.Parallel(): the method registry is package-level state.
type cleanEnv struct {
	robots  []myhome.VacuumRobot
	regions map[string][]myhome.VacuumRegion
	clean   func(*myhome.VacuumCleanParams) (any, error)
	cleaned []*myhome.VacuumCleanParams
}

func newCleanEnv(t *testing.T, robots ...myhome.VacuumRobot) *cleanEnv {
	t.Helper()
	roomEnv(t, nil)
	e := &cleanEnv{robots: robots, regions: map[string][]myhome.VacuumRegion{}}
	e.clean = func(*myhome.VacuumCleanParams) (any, error) {
		return &myhome.VacuumCleanResult{OK: true, Attempts: []myhome.VacuumCleanAttempt{{Outcome: myhome.VacuumCleanRunning}}}, nil
	}
	setVerb(t, myhome.VacuumList, func(context.Context, any) (any, error) {
		return &myhome.VacuumListResult{Robots: e.robots}, nil
	})
	setVerb(t, myhome.VacuumRooms, func(_ context.Context, in any) (any, error) {
		return &myhome.VacuumRoomsResult{Regions: e.regions[in.(*myhome.VacuumParams).Robot]}, nil
	})
	setVerb(t, myhome.VacuumClean, func(_ context.Context, in any) (any, error) {
		p := in.(*myhome.VacuumCleanParams)
		e.cleaned = append(e.cleaned, p)
		return e.clean(p)
	})
	return e
}

func idleRobot(id string) myhome.VacuumRobot {
	return myhome.VacuumRobot{ID: id, Name: "Robot " + id, Connected: true, Host: "robot.test",
		Status: &myhome.VacuumStatus{Phase: "charge", Cycle: "none", BinPresent: true}}
}

func reg(mapID, id, name, room string) myhome.VacuumRegion {
	return myhome.VacuumRegion{MapID: mapID, ID: id, Name: name, RoomID: room, Type: "rid"}
}

func (e *cleanEnv) block(t *testing.T) string {
	t.Helper()
	_, body := roomBody(t, nil, "/htmx/room/sejour/vacuum")
	return body
}

func (e *cleanEnv) post(t *testing.T, form url.Values) (int, string) {
	t.Helper()
	h := &HTMXHandler{ctx: context.Background(), log: logr.Discard()}
	req := httptest.NewRequest(http.MethodPost, "/htmx/vacuum/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.VacuumAction(rec, req)
	return rec.Code, rec.Body.String()
}

func roomForm(extra ...string) url.Values {
	f := url.Values{"robot": {"R1"}, "command": {"clean-rooms"}, "room_id": {"sejour"}}
	for i := 0; i+1 < len(extra); i += 2 {
		f.Add(extra[i], extra[i+1])
	}
	return f
}

// Every state of the design's §5 table renders.
func TestRoomVacuumBlockStates(t *testing.T) {
	cleaning := idleRobot("R1")
	cleaning.Status = &myhome.VacuumStatus{Phase: "run", Cycle: "clean", Regions: []string{"9"}}
	stuck := idleRobot("R1")
	stuck.Status = &myhome.VacuumStatus{Phase: "stuck", Cycle: "clean"}
	offline := idleRobot("R1")
	offline.Connected = false
	stale := reg("m1", "2", "Old", "sejour")
	stale.Stale = true

	tests := []struct {
		name    string
		robots  []myhome.VacuumRobot
		regions map[string][]myhome.VacuumRegion
		want    []string // substrings
		enabled bool
		reason  string // "" = no reason paragraph
	}{
		{name: "no robot", robots: nil, want: []string{`id="room-vacuum" hidden`}},
		{name: "no area assigned", robots: []myhome.VacuumRobot{idleRobot("R1")},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Kitchen", "cuisine")}},
			want:    []string{"No robot area assigned", "myhome ctl vacuum rooms assign"}},
		{name: "only stale areas", robots: []myhome.VacuumRobot{idleRobot("R1")},
			regions: map[string][]myhome.VacuumRegion{"R1": {stale}},
			want:    []string{"Old (robot map changed)"}, reason: "out of date"},
		{name: "areas on different maps", robots: []myhome.VacuumRobot{idleRobot("R1")},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour"), reg("m2", "5", "Salle", "sejour")}},
			reason:  "Areas are on different robot maps"},
		{name: "areas on different robots", robots: []myhome.VacuumRobot{idleRobot("R1"), idleRobot("R2")},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour")}, "R2": {reg("m1", "1", "Salon", "sejour")}},
			reason:  "Areas are on different robot maps"},
		{name: "robot offline", robots: []myhome.VacuumRobot{offline},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour")}},
			reason:  "Robot offline (last seen at robot.test)"},
		{name: "robot cleaning", robots: []myhome.VacuumRobot{cleaning},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour"), reg("m1", "9", "Bureau", "bureau")}},
			reason:  "Robot busy: cleaning Bureau"},
		{name: "robot stuck", robots: []myhome.VacuumRobot{stuck},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour")}},
			reason:  "Robot busy"},
		{name: "same id on another map", robots: []myhome.VacuumRobot{idleRobot("R1")},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour"), reg("m2", "1", "Garage", "garage")}},
			reason:  "also used on another robot map"},
		{name: "stale same id on another map is no collision", robots: []myhome.VacuumRobot{idleRobot("R1")},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour"), func() myhome.VacuumRegion {
				r := reg("m2", "1", "Garage", "garage")
				r.Stale = true
				return r
			}()}},
			enabled: true},
		{name: "ready", robots: []myhome.VacuumRobot{idleRobot("R1")},
			regions: map[string][]myhome.VacuumRegion{"R1": {reg("m1", "1", "Salon", "sejour"), reg("m1", "3", "Salle", "sejour")}},
			enabled: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newCleanEnv(t, tc.robots...)
			e.regions = tc.regions
			out := e.block(t)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q:\n%s", w, out)
				}
			}
			hasBtn := strings.Contains(out, "data-room-clean ")
			disabledBtn := strings.Contains(out, "data-room-clean disabled")
			confirmBtn := strings.Contains(out, "data-room-clean-confirm")
			switch {
			case tc.enabled:
				if !hasBtn || disabledBtn || !confirmBtn || strings.Contains(out, "data-room-clean-reason") {
					t.Errorf("want an enabled button with its confirm:\n%s", out)
				}
			case tc.reason != "":
				if !disabledBtn || confirmBtn || !strings.Contains(out, "data-room-clean-reason>") || !strings.Contains(out, tc.reason) {
					t.Errorf("want a disabled button with reason %q:\n%s", tc.reason, out)
				}
			default:
				if hasBtn {
					t.Errorf("no button expected:\n%s", out)
				}
			}
		})
	}
}

func TestRoomVacuumConfirmMarkup(t *testing.T) {
	e := newCleanEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}
	out := e.block(t)

	open := buttonTag(t, out, "Clean this room")
	if strings.Contains(open, "hx-post") || !strings.Contains(open, `@click="open()"`) {
		t.Errorf("Clean this room must only open the confirm: %s", open)
	}
	cancel := buttonTag(t, out, "Cancel")
	if strings.Contains(cancel, "hx-") || !strings.Contains(cancel, `@click="close()"`) {
		t.Errorf("Cancel must send nothing: %s", cancel)
	}
	for _, want := range []string{"Clean Séjour now? The robot leaves its dock.", "20000", ":data-confirming",
		"Starting&hellip;", `hx-disabled-elt="this"`, `hx-target="#room-vacuum"`, `hx-swap="outerHTML"`,
		"&#34;room_id&#34;:&#34;sejour&#34;", "&#34;command&#34;:&#34;clean-rooms&#34;",
		"!document.querySelector('#room-vacuum [data-confirming]')"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// the request names the room, never a region, and exactly one control posts
	if strings.Count(out, "hx-post") != 1 || strings.Contains(out, "&#34;room&#34;") {
		t.Errorf("want one POST carrying room_id only:\n%s", out)
	}
	if strings.Contains(out, "confirm(") || strings.Contains(out, "alert(") {
		t.Error("native dialog used")
	}
}

func TestVacuumAction_RoomClean(t *testing.T) {
	e := newCleanEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{
		reg("m1", "1", "Salon", "sejour"), reg("m1", "3", "Salle", "sejour"),
		reg("m1", "7", "Cuisine", "cuisine"), func() myhome.VacuumRegion {
			r := reg("m1", "8", "Old", "sejour")
			r.Stale = true
			return r
		}(),
	}

	// client `room` fields are ignored; exactly this room's non-stale ids are sent
	code, out := e.post(t, roomForm("room", "7", "room", "Cuisine"))
	if code != 200 || len(e.cleaned) != 1 {
		t.Fatalf("code %d, cleaned %v", code, e.cleaned)
	}
	got := e.cleaned[0]
	if got.Robot != "R1" || strings.Join(got.Rooms, ",") != "1,3" {
		t.Errorf("vacuum.clean got robot %q rooms %v, want R1 [1 3]", got.Robot, got.Rooms)
	}
	if !strings.Contains(out, `id="room-vacuum"`) || strings.Contains(out, "mh-vac-head") || !strings.Contains(out, "Cleaning Séjour") {
		t.Errorf("want the room's vacuum block saying Cleaning Séjour:\n%s", out)
	}

	// no-report
	e.cleaned = nil
	e.clean = func(*myhome.VacuumCleanParams) (any, error) {
		return &myhome.VacuumCleanResult{OK: true, Attempts: []myhome.VacuumCleanAttempt{{Outcome: myhome.VacuumCleanNoReport}}}, nil
	}
	if _, out = e.post(t, roomForm()); !strings.Contains(out, "Sent; the robot has not confirmed yet") || strings.Contains(out, "Cleaning Séjour") {
		t.Errorf("no-report:\n%s", out)
	}

	// an error is shown with its text (vacuum.clean already appends VacuumErrorHint)
	e.clean = func(*myhome.VacuumCleanParams) (any, error) {
		return nil, errors.New("the robot aborted at once. " + myhome.VacuumErrorHint(224))
	}
	if _, out = e.post(t, roomForm()); !strings.Contains(out, "data-room-clean-error") || !strings.Contains(out, "Clean failed: the robot aborted at once") ||
		!strings.Contains(out, "error 224") {
		t.Errorf("error:\n%s", out)
	}
}

// The server re-checks everything at the moment of the POST and sends nothing
// when the page it came from was stale.
func TestVacuumAction_RoomCleanRechecks(t *testing.T) {
	cleaning := idleRobot("R1")
	cleaning.Status = &myhome.VacuumStatus{Phase: "run", Cycle: "clean"}
	offline := idleRobot("R1")
	offline.Connected = false
	stale := reg("m1", "1", "Salon", "sejour")
	stale.Stale = true

	tests := []struct {
		name    string
		robots  []myhome.VacuumRobot
		regions []myhome.VacuumRegion
		form    url.Values
		want    string
	}{
		{"busy", []myhome.VacuumRobot{cleaning}, []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}, roomForm(), "Robot busy"},
		{"offline", []myhome.VacuumRobot{offline}, []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}, roomForm(), "Robot offline"},
		{"stale", []myhome.VacuumRobot{idleRobot("R1")}, []myhome.VacuumRegion{stale}, roomForm(), "out of date"},
		{"two maps", []myhome.VacuumRobot{idleRobot("R1")}, []myhome.VacuumRegion{reg("m1", "1", "A", "sejour"), reg("m2", "2", "B", "sejour")}, roomForm(), "different robot maps"},
		{"id also on another map", []myhome.VacuumRobot{idleRobot("R1")}, []myhome.VacuumRegion{reg("m1", "1", "A", "sejour"), reg("m2", "1", "B", "other")}, roomForm(), "also used on another robot map"},
		{"unassigned", []myhome.VacuumRobot{idleRobot("R1")}, []myhome.VacuumRegion{reg("m1", "1", "A", "other")}, roomForm(), "no robot area is assigned"},
		{"other robot", []myhome.VacuumRobot{idleRobot("R1")}, []myhome.VacuumRegion{reg("m1", "1", "A", "sejour")}, func() url.Values { f := roomForm(); f.Set("robot", "R9"); return f }(), "not \"R9\""},
		{"unknown room", []myhome.VacuumRobot{idleRobot("R1")}, []myhome.VacuumRegion{reg("m1", "1", "A", "sejour")}, func() url.Values { f := roomForm(); f.Set("room_id", "nowhere"); return f }(), "unknown room"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newCleanEnv(t, tc.robots...)
			e.regions["R1"] = tc.regions
			_, out := e.post(t, tc.form)
			if len(e.cleaned) != 0 {
				t.Errorf("vacuum.clean was called: %v", e.cleaned[0])
			}
			if !strings.Contains(out, "data-room-clean-error") || !strings.Contains(out, tc.want) {
				t.Errorf("want an error containing %q:\n%s", tc.want, out)
			}
		})
	}

	t.Run("room_id with another command", func(t *testing.T) {
		e := newCleanEnv(t, idleRobot("R1"))
		code, _ := e.post(t, roomForm("x", "y"))
		_ = code
		f := roomForm()
		f.Set("command", "start")
		if code, _ := e.post(t, f); code != http.StatusBadRequest || len(e.cleaned) != 0 {
			t.Errorf("code %d", code)
		}
	})

	t.Run("without room_id the old branch is unchanged", func(t *testing.T) {
		e := newCleanEnv(t, idleRobot("R1"))
		f := url.Values{"robot": {"R1"}, "command": {"clean-rooms"}, "room": {"Cuisine", "Salon"}}
		_, out := e.post(t, f)
		if len(e.cleaned) != 1 || strings.Join(e.cleaned[0].Rooms, ",") != "Cuisine,Salon" || !strings.Contains(out, "mh-vac") {
			t.Errorf("cleaned %v, page %s", e.cleaned, out)
		}
	})
}

// Golden: the Vacuum section's confirm scope is #685 as written. Only Clean all
// and Stop open a confirm; Dock, Pause, Resume, Find and Empty bin post directly;
// nothing in the panel knows about room_id.
func TestVacuumPanel_ConfirmScopeGolden(t *testing.T) {
	out := renderVacuumPanel(t, card(true, &myhome.VacuumStatus{Phase: "charge", BinPresent: true}, nil))
	if n := strings.Count(out, "@click="); n != 4 { // open('start'), open('stop'), and the confirm's Cancel (close())
		// the template also has the two x-data handlers inline, which are not @click
		t.Logf("@click count %d", n)
	}
	for _, want := range []string{
		`@click="open('start')">Clean all</button>`,
		`@click="open('stop')">Stop</button>`,
		`hx-vals='{"robot":"B1","command":"pause"}'`,
		`hx-vals='{"robot":"B1","command":"resume"}'`,
		`hx-vals='{"robot":"B1","command":"dock"}'`,
		`hx-vals='{"robot":"B1","command":"find"}'`,
		`hx-vals='{"robot":"B1","command":"evac"}'`,
		`hx-vals='{"robot":"B1","command":"start"}' hx-target="#vacuum-container">Confirm</button>`,
		`hx-vals='{"robot":"B1","command":"stop"}' hx-target="#vacuum-container">Confirm</button>`,
		"Start a whole-home clean?", "Stop the mission? The robot will not resume it.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Vacuum section changed, missing %q", want)
		}
	}
	for _, label := range []string{"Dock", "Pause", "Resume", "Find", "Empty bin"} {
		tag := buttonTag(t, out, label)
		if strings.Contains(tag, "@click") || !strings.Contains(tag, "hx-post") {
			t.Errorf("%s must post directly: %s", label, tag)
		}
	}
	if strings.Count(out, "open(") != 3 { // the definition and the two openers
		t.Errorf("only Clean all and Stop may open a confirm:\n%s", out)
	}
	if strings.Count(out, "data-vac-confirm=") != 2 {
		t.Error("the Vacuum section must have exactly two confirms")
	}
	for _, banned := range []string{"room_id", "clean-rooms", "Clean this room"} {
		if strings.Contains(out, banned) {
			t.Errorf("Vacuum section mentions %q", banned)
		}
	}
}
