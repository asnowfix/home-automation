package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func TestOfflineBannerMarkupHiddenByDefault(t *testing.T) {
	html := get(testMux(), "/").Body.String()
	m := regexp.MustCompile(`(?s)<div id="mh-offline"([^>]*)>(.*?)</div>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatal("offline banner missing")
	}
	if !regexp.MustCompile(`\shidden[\s>]`).MatchString(" " + m[1] + ">") {
		t.Errorf("banner is not hidden in markup: %q", m[1])
	}
	if !strings.Contains(m[2], `<span class="mh-offline-text">Penates (My Home Daemon) is offline</span>`) {
		t.Errorf("banner text not exact: %q", m[2])
	}
	if !strings.Contains(m[2], "<svg") {
		t.Error("banner has no icon")
	}
	if strings.Contains(html, "mh-content is-stale") {
		t.Error("content is stale in markup")
	}
	// polling stops while offline
	if !strings.Contains(html, "kiosk.visible('vacuum') && kiosk.online") {
		t.Error("vacuum poll is not gated on kiosk.online")
	}
}

func TestOfflineCSS(t *testing.T) {
	css := string(staticAsset(t, "/static/myhome.css"))
	for _, want := range []string{"filter: grayscale(1)", "pointer-events: none", "flex: 0 0 var(--mh-tap)"} {
		if !strings.Contains(css, want) {
			t.Errorf("myhome.css lacks %q", want)
		}
	}
}

// kioskVM loads myhome.js (definitions only) into goja.
func kioskVM(t *testing.T) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(string(staticAsset(t, "/static/myhome.js"))); err != nil {
		t.Fatalf("myhome.js: %v", err)
	}
	return vm
}

func evalJS(t *testing.T, vm *goja.Runtime, src string) goja.Value {
	t.Helper()
	v, err := vm.RunString(src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

func TestKioskLivenessStateMachine(t *testing.T) {
	vm := kioskVM(t)
	// refresh/dirty touch the DOM: stub them
	evalJS(t, vm, `var calls = []; kiosk.refresh = function(n){calls.push('r:'+n)}; kiosk.dirty = function(n){calls.push('d:'+n)};
		kiosk.section = 'rooms';
		var P = function(ts, st, v){ return {ts: ts, started: st, version: v}; };
		var t0 = new Date(2026, 10, 7, 14, 32, 5).getTime();
		var now0 = new Date(2026, 10, 7, 15, 0, 0);`)
	js := func(src string) string { return evalJS(t, vm, src).String() }

	// first ping sets the baseline; no reload, no recovery
	if got := js(`var r = kiosk.frame(1000, t0, P(t0/1000, 100, "1.0")); r.recovered + "," + r.reload`); got != "false,false" {
		t.Errorf("first ping = %s", got)
	}
	// silence under 40 s keeps us online, beyond it flips offline once
	if got := js(`String(kiosk.tick(30000, now0)) + kiosk.online`); got != "falsetrue" {
		t.Errorf("tick 29s = %s", got)
	}
	if got := js(`String(kiosk.tick(41001, now0)) + kiosk.online + String(kiosk.tick(50000, now0))`); got != "truefalsefalse" {
		t.Errorf("tick 40s = %s", got)
	}
	// stale since = the daemon's last ping time, not the moment of detection
	if got := js(`kiosk.staleText`); got != "stale since 14:32" {
		t.Errorf("staleText = %q", got)
	}
	// resumed pings: recovered once, same version: no reload; visible refreshed, rest dirty
	if got := js(`var r2 = kiosk.frame(60000, t0 + 60000, P(t0/1000 + 60, 200, "1.0")); r2.recovered + "," + r2.reload`); got != "true,false" {
		t.Errorf("recovery = %s", got)
	}
	if got := js(`kiosk.recovered(); calls.join(" ")`); got != "d:home r:rooms d:pool d:vacuum d:events" {
		t.Errorf("recovered calls = %s", got)
	}
	if got := js(`String(kiosk.online) + "|" + kiosk.staleText`); got != "true|" {
		t.Errorf("after recovery = %s", got)
	}
	// a second recovery is not reported
	if got := js(`String(kiosk.frame(61000, t0, null).recovered)`); got != "false" {
		t.Errorf("repeat frame recovered = %s", got)
	}
	// stream error flips offline immediately; label from last ping
	if got := js(`String(kiosk.lost(now0)) + "|" + kiosk.staleText`); got != "true|stale since 14:33" {
		t.Errorf("lost = %s", got)
	}
	// a version change after reconnect asks for exactly one reload
	if got := js(`var r3 = kiosk.frame(90000, t0, P(t0/1000 + 90, 300, "1.1")); r3.recovered + "," + r3.reload`); got != "true,true" {
		t.Errorf("upgrade = %s", got)
	}
}

func TestKioskStaleLabelFallbackAndOtherDay(t *testing.T) {
	vm := kioskVM(t)
	evalJS(t, vm, `var now = new Date(2026, 10, 7, 15, 0, 0);`)
	// no ping yet: browser time of the last frame
	evalJS(t, vm, `kiosk.lastFrameWall = new Date(2026, 10, 7, 9, 5).getTime();`)
	if got := evalJS(t, vm, `kiosk.staleLabel(now)`).String(); got != "stale since 09:05" {
		t.Errorf("fallback label = %q", got)
	}
	// another day: date prefix, time kept last
	evalJS(t, vm, `kiosk.lastPingTs = new Date(2026, 10, 5, 23, 59).getTime() / 1000;`)
	got := evalJS(t, vm, `kiosk.staleLabel(now)`).String()
	if !strings.HasPrefix(got, "stale since ") || !strings.HasSuffix(got, " 23:59") {
		t.Errorf("other-day label = %q", got)
	}
}

func TestShellWiresLiveness(t *testing.T) {
	js := string(staticAsset(t, "/static/myhome.js"))
	for _, want := range []string{"addEventListener('ping'", "kiosk.lost(", "kiosk.tick(", "htmx:beforeRequest"} {
		if !strings.Contains(js, want) {
			t.Errorf("myhome.js lacks %q", want)
		}
	}
	if regexp.MustCompile(`localStorage|sessionStorage`).MatchString(js) {
		t.Error("v1 keeps the cache in memory only")
	}
}
