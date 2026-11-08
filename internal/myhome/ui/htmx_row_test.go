package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	pkgshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/go-logr/logr"
)

func f64(v float64) *float64 { return &v }
func bptr(v bool) *bool      { return &v }

func switchView(on, unknown bool) DeviceView {
	v := DeviceView{
		Id: "plug-1", Name: "Kitchen", Manufacturer: "Shelly",
		Switches: map[int]pkgshelly.SwitchSummary{0: {Id: 0, Name: "main", On: on}},
	}
	if unknown {
		v.SwitchUnknown = map[int]bool{0: true}
	}
	return v
}

func TestDeviceRowSwitchStates(t *testing.T) {
	cases := []struct {
		name         string
		on, unknown  bool
		wantClass    string
		wantToggle   bool
		wantDisabled bool
		wantPressed  string
	}{
		{"off", false, false, `class="mh-power"`, true, false, "false"},
		{"on", true, false, `class="mh-power is-on"`, true, false, "true"},
		{"unknown", false, true, `class="mh-power is-unknown"`, false, true, "false"},
	}
	for _, c := range cases {
		out := renderDevice(t, switchView(c.on, c.unknown))
		if !strings.Contains(out, c.wantClass) {
			t.Errorf("%s: want %s in\n%s", c.name, c.wantClass, out)
		}
		if got := strings.Contains(out, `hx-post="/htmx/switch/toggle"`); got != c.wantToggle {
			t.Errorf("%s: hx-post present = %v", c.name, got)
		}
		if got := regexp.MustCompile(`\sdisabled\s`).MatchString(out); got != c.wantDisabled {
			t.Errorf("%s: disabled = %v", c.name, got)
		}
		if !strings.Contains(out, `aria-pressed="`+c.wantPressed+`"`) {
			t.Errorf("%s: aria-pressed should be %s", c.name, c.wantPressed)
		}
		if strings.Contains(out, "toggleSwitch") || strings.Contains(out, "is-info") || strings.Contains(out, "is-warning") {
			t.Errorf("%s: legacy card markup left behind", c.name)
		}
	}
}

func TestPendingLooksDifferentFromUnknown(t *testing.T) {
	css := string(staticAsset(t, "/static/myhome.css"))
	if regexp.MustCompile(`\.mh-power:disabled`).MatchString(css) {
		t.Error("a disabled (pending) power button is styled like an unknown one")
	}
	if !strings.Contains(css, ".mh-power.is-unknown {") || !strings.Contains(css, ".mh-power.htmx-request {") {
		t.Error("unknown and pending styles missing")
	}
}

func TestDeviceRowOnePerSwitchAndTitles(t *testing.T) {
	v := DeviceView{Id: "pro2-1", Name: "Hall", Switches: map[int]pkgshelly.SwitchSummary{
		1: {Id: 1, Name: "", On: false}, 0: {Id: 0, Name: "ceiling", On: true},
	}}
	out := renderDevice(t, v)
	if n := strings.Count(out, `class="mh-row"`); n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
	if n := strings.Count(out, `id="device-pro2-1"`); n != 1 {
		t.Errorf("device wrapper ids = %d, want 1", n)
	}
	i, j := strings.Index(out, "Hall · ceiling"), strings.Index(out, "Hall · Switch 1")
	if i < 0 || j < 0 || i > j {
		t.Errorf("titles missing or unordered (%d, %d):\n%s", i, j, out)
	}
	// a single switch keeps the plain device name
	if out := renderDevice(t, switchView(false, false)); strings.Contains(out, "Kitchen · ") {
		t.Error("single-switch device got a suffixed title")
	}
	// sensor pills only on the first row
	v.HasTemperatureSensor = true
	if n := strings.Count(renderDevice(t, v), `id="sensor-pro2-1-temperature"`); n != 1 {
		t.Errorf("temperature pill count = %d, want 1", n)
	}
}

func TestDeviceRowPillsShowDashesWhenUnknown(t *testing.T) {
	out := renderDevice(t, DeviceView{Id: "ht-1", Name: "Sensor", HasTemperatureSensor: true, HasHumiditySensor: true, HasDoorSensor: true})
	for _, want := range []string{`>--°C<`, `>--%<`, `<span data-door-text>--</span>`} {
		if !strings.Contains(out, want) {
			t.Errorf("unknown sensor lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mh-power") {
		t.Error("a sensor-only device has a state button")
	}
	out = renderDevice(t, DeviceView{Id: "ht-1", Name: "Sensor", HasTemperatureSensor: true, HasHumiditySensor: true, HasDoorSensor: true,
		Temperature: f64(21.44), Humidity: f64(48), DoorOpened: bptr(true)})
	for _, want := range []string{`>21.4°C<`, `>48.0%<`, `<span data-door-text>Open</span>`, "mh-pill is-alert"} {
		if !strings.Contains(out, want) {
			t.Errorf("known sensor lacks %q:\n%s", want, out)
		}
	}
	// a door that is known to be closed is neither unknown nor alert
	if out := renderDevice(t, DeviceView{Id: "d", Name: "D", HasDoorSensor: true, DoorOpened: bptr(false)}); !strings.Contains(out, "Closed") || strings.Contains(out, "is-alert") {
		t.Errorf("closed door rendered wrongly:\n%s", out)
	}
}

func TestDeviceRowTapTargetsAndSheet(t *testing.T) {
	out := renderDevice(t, switchView(true, false))
	// the row opens the sheet from data-* attributes, never from script built out of names
	if !strings.Contains(out, `@click="openDeviceSheet($el.dataset)"`) || !strings.Contains(out, `data-device-id="plug-1"`) {
		t.Errorf("row does not open the device sheet from data attributes:\n%s", out)
	}
	if strings.Contains(out, "$dispatch") {
		t.Error("row embeds names in script")
	}
	css := string(staticAsset(t, "/static/myhome.css"))
	for _, sel := range []string{".mh-power", ".mh-avatar", ".mh-row-main", ".mh-action"} {
		m := regexp.MustCompile(`(?s)\n` + regexp.QuoteMeta(sel) + ` \{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil || !strings.Contains(m[1], "var(--mh-tap)") {
			t.Errorf("%s is not sized from --mh-tap", sel)
		}
	}
	// no fixed pixel widths on the layout containers: a phone layout comes later
	for _, sel := range []string{".mh-grid", ".mh-row", ".mh-device"} {
		m := regexp.MustCompile(`(?s)\n` + regexp.QuoteMeta(sel) + ` \{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil || regexp.MustCompile(`\bwidth:\s*\d+px`).MatchString(m[1]) {
			t.Errorf("%s missing or has a fixed pixel width", sel)
		}
	}
	html := get(testMux(), "/").Body.String()
	if !regexp.MustCompile(`class="modal" :class="\{'is-active': modals.device\}"`).MatchString(html) {
		t.Error("device sheet missing or open by default")
	}
	if strings.Contains(html, "columns is-multiline\"\n             id=\"devices-container\"") {
		t.Error("devices container still uses Bulma columns")
	}
	for _, s := range []string{"is-warning", "is-success is-small", "is-danger is-small"} {
		if strings.Contains(out, s) {
			t.Errorf("row has legacy colour button %s", s)
		}
	}
}

// stubSwitchToggle registers a switch.toggle handler for one test and restores the registry.
func stubSwitchToggle(t *testing.T, h myhome.MethodHandler) {
	t.Helper()
	prev, err := myhome.Methods(myhome.SwitchToggle)
	myhome.RegisterMethodHandler(myhome.SwitchToggle, h)
	t.Cleanup(func() {
		if err == nil {
			myhome.RegisterMethodHandler(myhome.SwitchToggle, prev.ActionE)
		} else {
			myhome.UnregisterMethodHandler(myhome.SwitchToggle)
		}
	})
}

func postToggle(h http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/htmx/switch/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestToggleRouteRendersLogicalState(t *testing.T) {
	var got myhome.SwitchParams
	for _, on := range []bool{true, false} {
		stubSwitchToggle(t, func(_ context.Context, in any) (any, error) {
			got = in.(myhome.SwitchParams)
			return &myhome.SwitchResult{DeviceID: "plug-1", SwitchId: 1, On: on}, nil
		})
		rec := postToggle(testMux(), url.Values{"device_id": {"plug-1"}, "switch_id": {"1"}, "label": {`Kitchen "main"`}})
		if rec.Code != 200 {
			t.Fatalf("toggle = %d: %s", rec.Code, rec.Body)
		}
		body := rec.Body.String()
		if got.Identifier != "plug-1" || got.SwitchId != 1 {
			t.Errorf("handler got %+v", got)
		}
		if has := strings.Contains(body, `class="mh-power is-on"`); has != on {
			t.Errorf("on=%v but ring class present=%v:\n%s", on, has, body)
		}
		for _, want := range []string{`id="btn-switch-plug-1-1"`, `hx-disabled-elt="this"`, `hx-swap="outerHTML"`} {
			if !strings.Contains(body, want) {
				t.Errorf("response lacks %s", want)
			}
		}
		// the label (quotes and all) is JSON in hx-vals, escaped for the attribute
		if !strings.Contains(body, `&#34;label&#34;:&#34;Kitchen \&#34;main\&#34;&#34;`) {
			t.Errorf("label not carried in hx-vals:\n%s", body)
		}
	}
}

func TestToggleRouteRejectsBadInput(t *testing.T) {
	stubSwitchToggle(t, func(context.Context, any) (any, error) {
		t.Error("handler must not run on bad input")
		return nil, nil
	})
	h := testMux()
	if rec := postToggle(h, url.Values{"device_id": {"x"}, "switch_id": {"abc"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric switch_id = %d, want 400", rec.Code)
	}
	if rec := postToggle(h, url.Values{"device_id": {"x"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("missing switch_id = %d, want 400", rec.Code)
	}
	if rec := get(h, "/htmx/switch/toggle"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", rec.Code)
	}
}

type fakeRegistry struct{ devs []*myhome.Device }

func (f fakeRegistry) GetAllDevices(context.Context) ([]*myhome.Device, error) { return f.devs, nil }
func (f fakeRegistry) GetDeviceById(_ context.Context, id string) (*myhome.Device, error) {
	for _, d := range f.devs {
		if d.Id() == id {
			return d, nil
		}
	}
	return nil, http.ErrMissingFile
}
func (f fakeRegistry) GetDeviceByAny(ctx context.Context, id string) (*myhome.Device, error) {
	return f.GetDeviceById(ctx, id)
}

// Rendering rows, switches included, must not actuate switches: only the
// explicit toggle does. (The polarity read in DeviceToView is a separate
// device call, tracked in #732, and is not asserted here.)
func TestRenderingRowsCallsNoSwitchMethod(t *testing.T) {
	calls := 0
	stubSwitchToggle(t, func(context.Context, any) (any, error) { calls++; return nil, nil })
	sensor := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyht-abc").WithName("Hall sensor")
	plug := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyplus1-abc").WithName("Lamp")
	plug.Config = &pkgshelly.Config{Switch0: &sswitch.Config{Id: 0, Name: "main"}}
	h := newMux(logr.NewContext(context.Background(), logr.Discard()), logr.Discard(), nil, fakeRegistry{devs: []*myhome.Device{sensor, plug}}, nil, nil, "", nil, nil)
	for _, p := range []string{"/htmx/devices", "/htmx/device/shellyht-abc", "/htmx/device/shellyplus1-abc"} {
		rec := get(h, p)
		if rec.Code != 200 {
			t.Fatalf("%s = %d: %s", p, rec.Code, rec.Body)
		}
		if p != "/htmx/device/shellyht-abc" && !strings.Contains(rec.Body.String(), `id="btn-switch-shellyplus1-abc-0"`) {
			t.Errorf("%s renders no switch row:\n%s", p, rec.Body)
		}
	}
	if calls != 0 {
		t.Errorf("switch.toggle called %d times while rendering", calls)
	}
}

func TestToggleFailureIsReportedOnTheSection(t *testing.T) {
	vm := kioskVM(t)
	got := evalJS(t, vm, `
		var pill = {hidden: true, textContent: ''};
		var sec = {querySelector: function(){ return pill; }};
		var btn = {classList: {contains: function(c){ return c === 'mh-power'; }},
		           getAttribute: function(){ return 'Toggle Kitchen'; },
		           closest: function(){ return sec; }};
		kiosk.failed(btn);
		var plain = {classList: {contains: function(){ return false; }}, closest: function(){ return sec; }};
		var first = pill.textContent;
		kiosk.failed(plain);
		first + '|' + pill.textContent + '|' + pill.hidden`).String()
	parts := strings.Split(got, "|")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "couldn't toggle Kitchen, ") || !strings.HasPrefix(parts[1], "couldn't refresh, ") || parts[2] != "false" {
		t.Errorf("failure pills = %q", got)
	}
}
