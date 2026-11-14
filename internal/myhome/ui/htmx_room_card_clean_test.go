package ui

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// Rooms list card Clean shortcut and rail Reload (#817). These tests use HTTP and
// literals only, so they also compile (and fail) against a tree without the feature.

// cardEnv is cleanEnv with a second room, "cuisine", that has no mapped area.
func newCardEnv(t *testing.T, robots ...myhome.VacuumRobot) *cleanEnv {
	t.Helper()
	e := newCleanEnv(t, robots...)
	registerRooms(t, myhome.TemperatureRoomList{
		"sejour":  {RoomID: "sejour", Name: "Séjour"},
		"cuisine": {RoomID: "cuisine", Name: "Cuisine"},
	})
	return e
}

func (e *cleanEnv) roomsList(t *testing.T) string {
	t.Helper()
	code, body := roomBody(t, nil, "/htmx/rooms")
	if code != 200 {
		t.Fatalf("GET /htmx/rooms = %d", code)
	}
	return body
}

// cardOf is the markup of one room's Clean container, "" when the card has none.
func cardOf(t *testing.T, list, roomName string) string {
	t.Helper()
	i := strings.Index(list, ">"+roomName+"<")
	if i < 0 {
		t.Fatalf("no card for %s:\n%s", roomName, list)
	}
	rest := list[i:]
	// up to the next card (or the end)
	if j := strings.Index(rest[1:], `class="column is-4-desktop`); j >= 0 {
		rest = rest[:j+1]
	}
	k := strings.Index(rest, "data-room-clean-card")
	if k < 0 {
		return ""
	}
	return rest[k:]
}

func TestRoomCardCleanShownOnlyWithAMappedArea(t *testing.T) {
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour"), reg("m1", "3", "Salle", "sejour"), reg("m1", "7", "Cuisine", "other")}
	list := e.roomsList(t)

	sejour := cardOf(t, list, "Séjour")
	if !strings.Contains(sejour, "data-room-clean ") || strings.Contains(sejour, "data-room-clean disabled") ||!strings.Contains(sejour, "data-room-clean-confirm") {
		t.Errorf("Séjour (mapped) must offer an enabled Clean:\n%s", sejour)
	}
	// one button for the whole room, however many areas
	if strings.Count(sejour, "data-room-clean ") != 1 || strings.Count(sejour, "hx-post") != 1 {
		t.Errorf("want one Clean button and one POST for a 2-area room:\n%s", sejour)
	}
	if c := cardOf(t, list, "Cuisine"); c != "" {
		t.Errorf("Cuisine has no mapped area, no control expected:\n%s", c)
	}

	// no robot configured: nothing on any card
	e.robots = nil
	if strings.Contains(e.roomsList(t), "data-room-clean") {
		t.Error("no robot configured: the card must not show Clean")
	}
	// robot, but nothing mapped to any room
	e.robots = []myhome.VacuumRobot{idleRobot("R1")}
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "7", "Garage", "garage")}
	if strings.Contains(e.roomsList(t), "data-room-clean") {
		t.Error("no area mapped: the card must not show Clean")
	}
	// the vacuum service is not registered at all
	setVerb(t, myhome.VacuumList, nil)
	if strings.Contains(e.roomsList(t), "data-room-clean") {
		t.Error("vacuum service absent: the card must not show Clean")
	}
}

// The card and the room page render the very same control: same plan, same
// confirm wording and markup; only the label and the swap target differ.
func TestRoomCardCleanIsTheRoomPageControl(t *testing.T) {
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}
	card := cardOf(t, e.roomsList(t), "Séjour")
	page := e.block(t)

	confirm := func(s string) string {
		i := strings.Index(s, `<div class="mh-vac-confirm"`)
		if i < 0 {
			t.Fatalf("no confirm in:\n%s", s)
		}
		j := strings.Index(s[i:],"</button>\n  </div>")
		if i < 0 || j < 0 {
			t.Fatalf("no confirm in:\n%s", s)
		}
		return s[i : i+j]
	}
	norm := func(s string) string {
		s = strings.ReplaceAll(s, `hx-target="#room-vacuum"`, "TARGET")
		s = strings.ReplaceAll(s, `hx-target="closest [data-room-clean-card]"`, "TARGET")
		return strings.ReplaceAll(s, ",&#34;view&#34;:&#34;card&#34;", "")
	}
	if norm(confirm(card)) != norm(confirm(page)) {
		t.Errorf("confirm markup differs.\ncard: %s\npage: %s", confirm(card), confirm(page))
	}
	for _, want := range []string{"Clean Séjour now? The robot leaves its dock.", `@click="close()"`, "20000", ":data-confirming",
		`hx-target="closest [data-room-clean-card]"`, `hx-swap="outerHTML"`, "&#34;room_id&#34;:&#34;sejour&#34;", "&#34;view&#34;:&#34;card&#34;"} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
	// the opener only opens; Cancel sends nothing; no native dialog
	if open := buttonTag(t, card, "Clean"); strings.Contains(open, "hx-post") || !strings.Contains(open, `@click="open()"`) {
		t.Errorf("Clean must only open the confirm: %s", open)
	}
	if cancel := buttonTag(t, card, "Cancel"); strings.Contains(cancel, "hx-") {
		t.Errorf("Cancel must send nothing: %s", cancel)
	}
	if strings.Contains(card, "confirm(") || strings.Contains(card, "alert(") {
		t.Error("native dialog used")
	}
}

// The confirmed card request goes through the room page's code path to CleanOnMap
// and is answered with the card's own container, never the room page's block.
func TestRoomCardCleanClickReachesCleanOnMapAndSwapsTheCard(t *testing.T) {
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{
		reg("m7", "1", "Salon", "sejour"), reg("m7", "3", "Salle", "sejour"), reg("m7", "9", "Cuisine", "cuisine"),
	}
	code, out := e.post(t, roomForm("view", "card"))
	if code != 200 || len(e.cleaned) != 1 {
		t.Fatalf("code %d, cleaned %v", code, e.cleaned)
	}
	got := e.cleaned[0]
	if got.Robot != "R1" || got.Map != "m7" || strings.Join(got.Rooms, ",") != "1,3" {
		t.Errorf("vacuum.clean got %+v, want robot R1 map m7 rooms [1 3]", got)
	}
	if !strings.Contains(out, "data-room-clean-card") || strings.Contains(out, `id="room-vacuum"`) || !strings.Contains(out, "Cleaning Séjour") {
		t.Errorf("want the card's container saying Cleaning Séjour, not the room block:\n%s", out)
	}
	// the room page's own request is unchanged
	if _, out = e.post(t, roomForm()); !strings.Contains(out, `id="room-vacuum"`) || strings.Contains(out, "data-room-clean-card") {
		t.Errorf("room page POST must still return its block:\n%s", out)
	}
	// a failure shows on the card
	e.clean = func(*myhome.VacuumCleanParams) (any, error) { return nil, os.ErrDeadlineExceeded }
	if _, out = e.post(t, roomForm("view", "card")); !strings.Contains(out, "data-room-clean-error") || !strings.Contains(out, "data-room-clean-card") {
		t.Errorf("card error:\n%s", out)
	}
	// the server re-checks: a robot that went offline since the render starts nothing
	e.robots[0].Connected = false
	e.cleaned = nil
	if _, out = e.post(t, roomForm("view", "card")); len(e.cleaned) != 0 || !strings.Contains(out, "Robot offline") {
		t.Errorf("offline robot: cleaned %v\n%s", e.cleaned, out)
	}
}

func TestRoomCardCleanDisabledStatesCarryTheirReason(t *testing.T) {
	cleaning := idleRobot("R1")
	cleaning.Status = &myhome.VacuumStatus{Phase: "run", Cycle: "clean", Regions: []string{"9"}}
	offline := idleRobot("R1")
	offline.Connected = false
	undocked := idleRobot("R1")
	undocked.Status = &myhome.VacuumStatus{Phase: "stuck", Cycle: "clean"}
	stale := reg("m1", "2", "Old", "sejour")
	stale.Stale = true

	tests := []struct {
		name   string
		robot  myhome.VacuumRobot
		region myhome.VacuumRegion
		reason string
	}{
		{"offline", offline, reg("m1", "1", "Salon", "sejour"), "Robot offline (last seen at robot.test)"},
		{"cleaning", cleaning, reg("m1", "1", "Salon", "sejour"), "Robot busy: cleaning"},
		{"not docked", undocked, reg("m1", "1", "Salon", "sejour"), "Robot busy"},
		{"map changed", idleRobot("R1"), stale, "Areas are out of date on the robot"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newCardEnv(t, tc.robot)
			e.regions["R1"] = []myhome.VacuumRegion{tc.region}
			card := cardOf(t, e.roomsList(t), "Séjour")
			if !strings.Contains(card, "data-room-clean disabled") || strings.Contains(card, "data-room-clean-confirm") ||
				!strings.Contains(card, "data-room-clean-reason>") || !strings.Contains(card, tc.reason) {
				t.Errorf("want a disabled Clean with reason %q:\n%s", tc.reason, card)
			}
			// identical to what the room page says
			if page := e.block(t); !strings.Contains(page, tc.reason) {
				t.Errorf("room page disagrees on %q:\n%s", tc.reason, page)
			}
		})
	}
}

// Rendering the Rooms list reads the daemon's cache only: no device is called and
// no clean is started.
func TestRoomCardCleanRenderMakesNoDeviceCall(t *testing.T) {
	calls := unreachableDevices(t)
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}
	var commands int
	setVerb(t, myhome.VacuumCommand, func(context.Context, any) (any, error) { commands++; return nil, nil })

	if !strings.Contains(e.roomsList(t), "data-room-clean-confirm") {
		t.Fatal("card control missing")
	}
	if n := calls.Load(); n != 0 || len(e.cleaned) != 0 || commands != 0 {
		t.Errorf("render made %d device call(s), %d clean(s), %d command(s)", n, len(e.cleaned), commands)
	}
}

// Tap targets: the rail Reload and the card's Clean/Cancel/Confirm are at least
// 48 px, the Reload is a one-tap client-side reload (no long press, no request),
// and the card cannot widen its column.
func TestRailReloadAndCardCleanTapTargets(t *testing.T) {
	css := string(staticAsset(t, "/static/myhome.css"))
	rule := func(sel string) string {
		m := regexp.MustCompile(`(?s)\n` + regexp.QuoteMeta(sel) + ` \{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil {
			t.Fatalf("no rule for %s", sel)
		}
		return m[1]
	}
	if m := regexp.MustCompile(`min-height:\s*calc\(var\(--mh-tap\) \+ 16px\)`); !m.MatchString(rule(".mh-rail-item")) {
		t.Error(".mh-rail-item (and so Reload) is not at least --mh-tap high")
	}
	m := regexp.MustCompile(`min-height:\s*(\d+)px`).FindStringSubmatch(rule(".mh-vac-btn"))
	if m == nil {
		t.Fatal(".mh-vac-btn has no px min-height")
	}
	if px, _ := strconv.Atoi(m[1]); px < 48 {
		t.Errorf(".mh-vac-btn (card Clean, Cancel, Confirm) is %d px, under 48", px)
	}
	if !strings.Contains(css, ".mh-rail-reload { margin-top: auto; }") {
		t.Error("Reload is not pinned to the bottom of the rail")
	}
	if !strings.Contains(css, ".mh-room-clean { width: 100%;") {
		t.Error("card Clean is not confined to the card's width")
	}

	html := get(testMux(), "/").Body.String()
	m2 := regexp.MustCompile(`(?s)<button[^>]*data-reload[^>]*>`).FindString(html)
	if m2 == "" {
		t.Fatal("no Reload button in the shell")
	}
	for _, want := range []string{`class="mh-rail-item mh-rail-reload"`, `@click="kiosk.reload()"`, `:disabled="!online"`} {
		if !strings.Contains(m2, want) {
			t.Errorf("Reload button missing %q: %s", want, m2)
		}
	}
	if strings.Contains(m2, "hx-") || strings.Contains(m2, "contextmenu") || strings.Contains(m2, "touchstart") || strings.Contains(m2, "pointerdown") {
		t.Errorf("Reload must be a plain tap with no request or long press: %s", m2)
	}
	// after the last section item, inside the rail
	rail := html[strings.Index(html, `<nav class="mh-rail"`):]
	rail = rail[:strings.Index(rail, "</nav>")]
	if strings.Index(rail, "data-reload") < strings.Index(rail, `@click="go('events')"`) {
		t.Error("Reload is not the last item of the rail")
	}
}

// The reload itself lives in myhome.js (the shell html must not inline it) and is
// refused while the daemon is down, since a reload then strands the kiosk on
// Chromium's error page (683 design). Nothing calls it from a timer.
func TestKioskReloadIsGuardedAndNeverAutomatic(t *testing.T) {
	js := string(staticAsset(t, "/static/myhome.js"))
	if !regexp.MustCompile(`reload: function\(\) \{\s*if \(this\.online\) \{ location\.reload\(\); \}\s*\}`).MatchString(js) {
		t.Error("kiosk.reload is not guarded by online")
	}
	if strings.Contains(js, "kiosk.reload(") || regexp.MustCompile(`set(Timeout|Interval)\([^)]*reload`).MatchString(js) {
		t.Error("kiosk.reload is called from script: it must only be the button's tap")
	}
}
