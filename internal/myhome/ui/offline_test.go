package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func TestOfflineBannerMarkupHiddenByDefault(t *testing.T) {
	html := get(testMux(), "/").Body.String()
	if !strings.Contains(html, `<body class="mh-shell" data-version="9.9.9-test"`) {
		t.Error("page does not carry the daemon version as the reload baseline")
	}
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
	rule := func(sel string) string {
		m := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil {
			t.Fatalf("no rule for %s", sel)
		}
		return m[1]
	}
	if !strings.Contains(rule(".mh-content.is-stale"), "filter: grayscale(1)") {
		t.Error("stale content is not greyed")
	}
	if !strings.Contains(rule(".mh-offline"), "flex: 0 0 var(--mh-tap)") {
		t.Error("banner is not one tap-target high")
	}
	// the inert selectors form one comma list ending in the rule that carries pointer-events
	m := regexp.MustCompile(`(?s)((?:\.mh-content\.is-stale [^,{]+,\s*)+\.mh-content\.is-stale select)\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if m == nil || !strings.Contains(m[2], "pointer-events: none") {
		t.Fatal("no inert rule for stale content")
	}
	for _, sel := range []string{"[hx-post]", "[hx-put]", "[hx-delete]", "[data-action]", "button", ".button", "input"} {
		if !strings.Contains(m[1], ".mh-content.is-stale "+sel) {
			t.Errorf("inert rule lacks %s", sel)
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
		kiosk.section = 'rooms'; kiosk.version = '1.0';
		var P = function(ts, st, v){ return {ts: ts, started: st, version: v}; };
		var t0 = new Date(2026, 10, 7, 14, 32, 5).getTime();
		var now0 = new Date(2026, 10, 7, 15, 0, 0);
		var J = function(o){ return JSON.stringify(o); };`)
	js := func(src string) string { return evalJS(t, vm, src).String() }

	// first ping: baseline for started; the page already supplied the version
	if got := js(`kiosk.frame(1000, t0, P(t0/1000, 100, "1.0"))`); got != "" {
		t.Errorf("first ping action = %q", got)
	}
	// silence under 40 s keeps us online
	if got := js(`J(kiosk.tick(30000, now0))`); got != `{"changed":false,"reopen":false}` {
		t.Errorf("tick 29s = %s", got)
	}
	// silent partition: flips once and asks to reopen immediately
	if got := js(`J(kiosk.tick(41001, now0))`); got != `{"changed":true,"reopen":true}` {
		t.Errorf("tick 40s = %s", got)
	}
	// stale since = the daemon's last ping time, not the moment of detection
	if got := js(`kiosk.staleText`); got != "stale since 14:32" {
		t.Errorf("staleText = %q", got)
	}
	// still offline: reopen only once the backoff (3 s, then 6 s) has elapsed
	if got := js(`J(kiosk.tick(43000, now0))`); got != `{"changed":false,"reopen":false}` {
		t.Errorf("tick before backoff = %s", got)
	}
	if got := js(`J(kiosk.tick(44001, now0))`); got != `{"changed":false,"reopen":true}` {
		t.Errorf("tick after 3s backoff = %s", got)
	}
	if got := js(`J(kiosk.tick(49000, now0))`); got != `{"changed":false,"reopen":false}` {
		t.Errorf("tick inside 6s backoff = %s", got)
	}
	if got := js(`J(kiosk.tick(50001, now0))`); got != `{"changed":false,"reopen":true}` {
		t.Errorf("tick after 6s backoff = %s", got)
	}
	// the backoff caps at 30 s
	if got := js(`for (var i = 0; i < 10; i++) { kiosk.scheduleReopen(60000); } kiosk.reopenDelay`); got != "30000" {
		t.Errorf("backoff cap = %s", got)
	}
	// resumed pings from the same daemon: recover, backoff reset, label cleared
	if got := js(`kiosk.frame(90000, t0 + 60000, P(t0/1000 + 60, 100, "1.0"))`); got != "recover" {
		t.Errorf("recovery = %q", got)
	}
	if got := js(`String(kiosk.online) + "|" + kiosk.staleText + "|" + kiosk.reopenDelay`); got != "true||3000" {
		t.Errorf("after recovery = %s", got)
	}
	if got := js(`kiosk.recovered(false); calls.join(" ")`); got != "r:rooms d:home d:pool d:vacuum d:events" {
		t.Errorf("recovered calls = %s", got)
	}
	if got := js(`calls = []; kiosk.recovered(true); calls.join(" ")`); got != "r:rooms r:home r:pool r:vacuum r:events" {
		t.Errorf("recovered(all) calls = %s", got)
	}
	// a second frame is not a recovery
	if got := js(`kiosk.frame(91000, t0, null)`); got != "" {
		t.Errorf("repeat frame = %q", got)
	}
	// a different started means a restart: refresh every section
	if got := js(`kiosk.frame(92000, t0, P(t0/1000 + 90, 200, "1.0"))`); got != "refresh-all" {
		t.Errorf("restart = %q", got)
	}
	// a version different from the page's means an upgrade: reload (wins over restart)
	if got := js(`kiosk.frame(93000, t0, P(t0/1000 + 91, 300, "1.1"))`); got != "reload" {
		t.Errorf("upgrade = %q", got)
	}
}

func TestKioskStreamError(t *testing.T) {
	vm := kioskVM(t)
	evalJS(t, vm, `var t0 = new Date(2026, 10, 7, 14, 32, 5).getTime(); var now0 = new Date(2026, 10, 7, 15, 0, 0);
		var J = function(o){ return JSON.stringify(o); };
		kiosk.frame(1000, t0, {ts: t0/1000, started: 1, version: "1"});`)
	js := func(src string) string { return evalJS(t, vm, src).String() }
	// CONNECTING: the browser retries by itself, we only show the banner
	if got := js(`J(kiosk.streamError(0, 2000, now0))`); got != `{"changed":true,"reopen":false}` {
		t.Errorf("CONNECTING error = %s", got)
	}
	// CLOSED (e.g. a non-200 answer): EventSource never retries, so reopen
	if got := js(`J(kiosk.streamError(2, 3000, now0))`); got != `{"changed":false,"reopen":true}` {
		t.Errorf("CLOSED error = %s", got)
	}
	// ... and the watchdog does not reopen again before the backoff
	if got := js(`J(kiosk.tick(4000, now0))`); got != `{"changed":false,"reopen":false}` {
		t.Errorf("tick right after CLOSED = %s", got)
	}
	if got := js(`J(kiosk.tick(6001, now0))`); got != `{"changed":false,"reopen":true}` {
		t.Errorf("tick after CLOSED backoff = %s", got)
	}
	// an error on a stream that is OPEN is not an outage
	if got := js(`kiosk.frame(7000, t0, null); J(kiosk.streamError(1, 7001, now0))`); got != `{"changed":false,"reopen":false}` {
		t.Errorf("OPEN error = %s", got)
	}
}

func TestKioskBlocksWritesOnlyOffline(t *testing.T) {
	vm := kioskVM(t)
	got := evalJS(t, vm, `var r = []; r.push(kiosk.blocks('post')); kiosk.online = false;
		r.push(kiosk.blocks('post'), kiosk.blocks('DELETE'), kiosk.blocks('get'), kiosk.blocks('GET'), kiosk.blocks(undefined)); r.join()`).String()
	if got != "false,true,true,false,false,false" {
		t.Errorf("blocks = %s", got)
	}
}

func TestKioskStaleLabelAcrossMidnight(t *testing.T) {
	vm := kioskVM(t)
	evalJS(t, vm, `kiosk.lastPingTs = new Date(2026, 10, 7, 23, 58).getTime() / 1000;
		kiosk.lost(new Date(2026, 10, 7, 23, 59));`)
	if got := evalJS(t, vm, `kiosk.staleText`).String(); got != "stale since 23:58" {
		t.Fatalf("before midnight = %q", got)
	}
	got := evalJS(t, vm, `var r = kiosk.tick(1000, new Date(2026, 10, 8, 0, 1)); r.changed + "|" + kiosk.staleText`).String()
	if !strings.HasPrefix(got, "true|stale since ") || !strings.HasSuffix(got, " 23:58") || got == "true|stale since 23:58" {
		t.Errorf("after midnight = %q", got)
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
	for _, want := range []string{"addEventListener('ping'", "kiosk.streamError(", "kiosk.tick(", "kiosk.blocks(", "htmx:beforeRequest"} {
		if !strings.Contains(js, want) {
			t.Errorf("myhome.js lacks %q", want)
		}
	}
	if regexp.MustCompile(`localStorage|sessionStorage`).MatchString(js) {
		t.Error("v1 keeps the cache in memory only")
	}
}

func TestShellPageIsNotCached(t *testing.T) {
	if cc := get(testMux(), "/").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control on / = %q, want no-cache", cc)
	}
}
