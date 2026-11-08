package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/events"

	"github.com/go-logr/logr"
)

func testMux() http.Handler {
	ctx := context.WithValue(context.Background(), global.VersionKey, "9.9.9-test")
	return newMux(ctx, logr.Discard(), nil, nil, nil, nil, "", nil, nil)
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestShellHasFiveRailItemsAndSections(t *testing.T) {
	rec := get(testMux(), "/")
	if rec.Code != 200 {
		t.Fatalf("GET / = %d", rec.Code)
	}
	html := rec.Body.String()
	if n := strings.Count(html, `class="mh-rail-item"`); n != 5 {
		t.Errorf("rail items = %d, want 5", n)
	}
	want := map[string]string{
		"home": "/htmx/devices", "rooms": "/htmx/rooms", "pool": "/htmx/pool",
		"vacuum": "/htmx/vacuum", "events": "/htmx/events",
	}
	for sec, url := range want {
		i := strings.Index(html, `id="sec-`+sec+`"`)
		if i < 0 {
			t.Errorf("section container sec-%s missing", sec)
			continue
		}
		rest := html[i:]
		if j := strings.Index(rest, "</section>"); j >= 0 {
			rest = rest[:j]
		}
		if !strings.Contains(rest, `hx-get="`+url+`"`) {
			t.Errorf("sec-%s does not hx-get %s", sec, url)
		}
	}
	if n := strings.Count(html, `data-section=`); n != 5 {
		t.Errorf("data-section containers = %d, want 5", n)
	}
}

// The kiosk opens on Rooms: Rooms is the first rail item and section, Devices
// (section id "home", unchanged) the second (#744).
func TestShellRailOrderRoomsThenDevices(t *testing.T) {
	html := get(testMux(), "/").Body.String()
	var labels []string
	for _, m := range regexp.MustCompile(`class="mh-rail-item"[\s\S]*?<span>([^<]*)</span>`).FindAllStringSubmatch(html, -1) {
		labels = append(labels, m[1])
	}
	if got, want := strings.Join(labels, ","), "Rooms,Devices,Pool,Vacuum,Events"; got != want {
		t.Errorf("rail = %s, want %s", got, want)
	}
	if strings.Index(html, `id="sec-rooms"`) > strings.Index(html, `id="sec-home"`) {
		t.Error("sec-rooms must precede sec-home")
	}
	js, err := os.ReadFile("static/myhome.js")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(js), "section: 'rooms'"); n != 2 {
		t.Errorf("default section 'rooms' appears %d times in myhome.js, want 2 (kiosk + app)", n)
	}
}

func TestShellHasNoExternalNavigationOrAutoOpenOverlay(t *testing.T) {
	html := get(testMux(), "/").Body.String()
	if strings.Contains(html, `target="_blank"`) {
		t.Error(`shell contains target="_blank"`)
	}
	if regexp.MustCompile(`https?://`).MatchString(html) {
		t.Error("shell contains an external URL")
	}
	for _, s := range []string{"window.open", "alert(", "confirm(", "location.reload"} {
		if strings.Contains(html, s) {
			t.Errorf("shell contains %q", s)
		}
	}
	// the backdrop must not dismiss any overlay
	if regexp.MustCompile(`modal-background"[^>]*@click`).MatchString(html) {
		t.Error("a modal backdrop dismisses on click")
	}
	// overlays open from Alpine state: every modal flag must start false, and none
	// may be forced open in markup
	js := string(staticAsset(t, "/static/myhome.js"))
	m := regexp.MustCompile(`(?s)modals:\s*\{(.*?)\}`).FindStringSubmatch(js)
	if m == nil || strings.Contains(m[1], "true") || strings.Count(m[1], "false") != 5 {
		t.Errorf("modals initial state must be five false flags, got %q", m)
	}
	if regexp.MustCompile(`class="modal[^"]*is-active`).MatchString(html) {
		t.Error("an overlay is open in markup")
	}
}

func TestShellScriptHasNoExternalNavigation(t *testing.T) {
	js := string(staticAsset(t, "/static/myhome.js"))
	for _, s := range []string{"window.open", "_blank"} {
		if strings.Contains(js, s) {
			t.Errorf("myhome.js contains %q", s)
		}
	}
	// the one permitted reload: a daemon upgrade seen on a ping (offline_test.go)
	if n := strings.Count(js, "location.reload"); n != 1 {
		t.Errorf("myhome.js has %d location.reload calls, want exactly 1", n)
	}
}

func TestEventLogRedirectsToEventsSection(t *testing.T) {
	rec := get(testMux(), "/event-log")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/#events" {
		t.Errorf("GET /event-log = %d %q, want 302 /#events", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPoolFragmentDisabledState(t *testing.T) {
	rec := get(testMux(), "/htmx/pool")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Pool tracking is not enabled") {
		t.Errorf("GET /htmx/pool = %d %q", rec.Code, rec.Body.String())
	}
}

func TestPoolPanelTemplateStates(t *testing.T) {
	render := func(v poolPanelView) string {
		var b bytes.Buffer
		if err := poolPanelTmpl.Execute(&b, v); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	achieved, target, active := 3.2, 5.0, false
	ok := render(poolPanelView{State: "ok", Name: "Pump", WaterSupplyActive: &active, TurnoverAchieved: &achieved, TurnoverTarget: &target})
	for _, s := range []string{"Pump", "Water supply: OK", "3.2/5.0 x/day"} {
		if !strings.Contains(ok, s) {
			t.Errorf("ok state lacks %q: %s", s, ok)
		}
	}
	paused := true
	if !strings.Contains(render(poolPanelView{State: "ok", WaterSupplyActive: &paused}), "pump paused") {
		t.Error("paused state not rendered")
	}
	unknown := render(poolPanelView{State: "ok", Name: "Pump"})
	for _, s := range []string{"Water supply: --", "Filtered 24 h: --"} {
		if !strings.Contains(unknown, s) {
			t.Errorf("unknown state lacks %q: %s", s, unknown)
		}
	}
	if strings.Contains(unknown, "OK") {
		t.Errorf("unknown water supply rendered as OK: %s", unknown)
	}
	if !strings.Contains(render(poolPanelView{State: "unavailable"}), "unavailable") {
		t.Error("unavailable state not rendered")
	}
}

func TestShellScriptServed(t *testing.T) {
	rec := get(testMux(), "/static/myhome.js?v=1")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/javascript; charset=utf-8" {
		t.Errorf("myhome.js = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestRoomsFragmentWhenTemperatureDisabled(t *testing.T) {
	rec := get(testMux(), "/htmx/rooms")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Rooms are not enabled") {
		t.Errorf("GET /htmx/rooms = %d %q", rec.Code, rec.Body.String())
	}
}

func TestVacuumFragmentWhenNoRobot(t *testing.T) {
	rec := get(testMux(), "/htmx/vacuum")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "No vacuum robot is configured") {
		t.Errorf("GET /htmx/vacuum = %d %q", rec.Code, rec.Body.String())
	}
}

func TestPoolViewFromStatus(t *testing.T) {
	wrapped := fmt.Errorf("pool status unavailable: %w", myhome.ErrPoolNotEnabled)
	if got := poolViewFromStatus(nil, wrapped).State; got != "disabled" {
		t.Errorf("not-enabled error -> %q, want disabled", got)
	}
	if got := poolViewFromStatus(nil, errors.New("compute turnover: timeout")).State; got != "unavailable" {
		t.Errorf("real error -> %q, want unavailable", got)
	}
	if got := poolViewFromStatus(&myhome.PoolSnapshotResult{DeviceID: "p"}, nil); got.State != "ok" || got.Name != "p" {
		t.Errorf("ok result -> %+v", got)
	}
}

func TestLiveEventRowMatchesServerRow(t *testing.T) {
	e := events.Event{Ts: 1700000000, DeviceID: "dev-1", Component: "switch:0", Event: "toggled", Severity: "info"}
	data := `{"a":1}`
	e.Data = &data
	row := toEventRows([]events.Event{e}, map[string]string{"dev-1": "Kitchen"})[0]
	html := renderEventRow(row)
	for _, want := range []string{"<tr", "Kitchen", `<code class="is-size-7 has-text-grey">dev-1</code>`, "<details", "switch:0"} {
		if !strings.Contains(html, want) {
			t.Errorf("live row lacks %q: %s", want, html)
		}
	}
	// the SSE payload carries that same row
	b := newTestBroadcaster(t)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)
	b.BroadcastEvent(e)
	msg := <-ch
	if !strings.Contains(msg, "event: eventlog") || !strings.Contains(msg, `\u003ctr`) {
		t.Errorf("eventlog payload lacks the rendered row: %s", msg)
	}
}

func TestShellScriptWiresEventLog(t *testing.T) {
	js := string(staticAsset(t, "/static/myhome.js"))
	if !strings.Contains(js, "addEventListener('eventlog'") || !strings.Contains(js, "kiosk.prependEvent") {
		t.Error("myhome.js does not listen for eventlog and call prependEvent")
	}
}

func TestHiddenAttributeWinsOverComponentDisplay(t *testing.T) {
	css := string(staticAsset(t, "/static/myhome.css"))
	if !regexp.MustCompile(`\[hidden\]\s*\{\s*display:\s*none\s*!important`).MatchString(css) {
		t.Error("myhome.css lacks [hidden] { display: none !important }")
	}
}
