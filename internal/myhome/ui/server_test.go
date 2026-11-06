package ui

import (
	"errors"
	"fmt"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/internal/myhome"

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
	if strings.Contains(html, `class="modal is-active"`) {
		t.Error("an overlay is open on load")
	}
}

func TestShellScriptHasNoExternalNavigation(t *testing.T) {
	js := string(staticAsset(t, "/static/myhome.js"))
	for _, s := range []string{"window.open", "location.reload", "_blank"} {
		if strings.Contains(js, s) {
			t.Errorf("myhome.js contains %q", s)
		}
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
	ok := render(poolPanelView{State: "ok", Name: "Pump", TurnoverAchieved: 3.2, TurnoverTarget: 5, RuntimeMin: 90})
	for _, s := range []string{"Pump", "Water supply: OK", "3.2 / 5.0", "90 min"} {
		if !strings.Contains(ok, s) {
			t.Errorf("ok state lacks %q: %s", s, ok)
		}
	}
	if !strings.Contains(render(poolPanelView{State: "ok", WaterSupplyActive: true}), "pump paused") {
		t.Error("paused state not rendered")
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
	if got := poolViewFromStatus(&myhome.PoolGetStatusResult{DeviceID: "p"}, nil); got.State != "ok" || got.Name != "p" {
		t.Errorf("ok result -> %+v", got)
	}
}
