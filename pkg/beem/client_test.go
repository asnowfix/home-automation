package beem

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// buildTestServer returns an httptest.Server that serves one login response and
// one (or more) summary responses.  The caller is responsible for closing it.
func buildTestServer(t *testing.T, loginFn, summaryFn http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/beemapp/user/login", loginFn)
	mux.HandleFunc("/beemapp/box/summary", summaryFn)
	return httptest.NewServer(mux)
}

// loginOK returns a valid login handler.
func loginOK(token string, expiresIn float64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loginResponse{
			AccessToken: token,
			ExpiresIn:   expiresIn,
		})
	}
}

// summaryOK returns a valid summary handler serving the real API's shape: a
// JSON array with one entry per registered Beem box.
func summaryOK(solarW, dailyWh, monthlyWh float64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]boxSummary{{
			WattHour:   solarW,
			TotalDay:   dailyWh,
			TotalMonth: monthlyWh,
		}})
	}
}

// newClientForTest creates a Client whose HTTP requests are redirected to srv.
func newClientForTest(cfg ClientConfig, srv *httptest.Server) *Client {
	c := NewClient(cfg)
	// Override the URLs so requests go to the test server.
	c.http = *srv.Client()
	return c
}

// TestPollSummary_HappyPath verifies that PollSummary returns the expected
// PowerSample on a normal 200-OK flow.
func TestPollSummary_HappyPath(t *testing.T) {
	srv := buildTestServer(t,
		loginOK("tok-happy", 3600),
		summaryOK(1230, 4500, 62000),
	)
	defer srv.Close()

	c := newClientForTest(ClientConfig{
		Email:        "user@example.com",
		Password:     "secret",
		PollInterval: 60 * time.Second,
	}, srv)

	// Patch URLs to point to our test server.
	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	ctx := context.Background()
	sample, err := c.PollSummary(ctx)
	if err != nil {
		t.Fatalf("PollSummary returned unexpected error: %v", err)
	}
	if sample.SolarW != 1230 {
		t.Errorf("SolarW = %v, want 1230", sample.SolarW)
	}
	if sample.DailyWh != 4500 {
		t.Errorf("DailyWh = %v, want 4500", sample.DailyWh)
	}
	if sample.MonthlyWh != 62000 {
		t.Errorf("MonthlyWh = %v, want 62000", sample.MonthlyWh)
	}
	if sample.Source != "rest" {
		t.Errorf("Source = %q, want \"rest\"", sample.Source)
	}
	if sample.TS.IsZero() {
		t.Error("TS is zero, want a non-zero timestamp")
	}
	if c.token != "tok-happy" {
		t.Errorf("token = %q, want \"tok-happy\"", c.token)
	}
}

// TestPollSummary_401TriggersRelogin verifies that a 401 from the summary
// endpoint causes the client to re-authenticate and retry.
func TestPollSummary_401TriggersRelogin(t *testing.T) {
	loginCalls := 0
	summaryCalls := 0

	summaryHandler := func(w http.ResponseWriter, r *http.Request) {
		summaryCalls++
		if summaryCalls == 1 {
			// First call: return 401 to trigger re-login.
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second call (after re-login): return a valid response.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]boxSummary{{
			WattHour:   500,
			TotalDay:   1000,
			TotalMonth: 5000,
		}})
	}

	loginHandler := func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loginResponse{
			AccessToken: "tok-refresh",
			ExpiresIn:   3600,
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/beemapp/user/login", loginHandler)
	mux.HandleFunc("/beemapp/box/summary", summaryHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(ClientConfig{Email: "u@example.com", Password: "pw"})
	c.http = *srv.Client()

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	ctx := context.Background()
	sample, err := c.PollSummary(ctx)
	if err != nil {
		t.Fatalf("PollSummary returned unexpected error after re-login: %v", err)
	}
	if loginCalls != 2 {
		t.Errorf("loginCalls = %d, want 2 (initial + retry after 401)", loginCalls)
	}
	if summaryCalls != 2 {
		t.Errorf("summaryCalls = %d, want 2 (401 + retry)", summaryCalls)
	}
	if sample.SolarW != 500 {
		t.Errorf("SolarW = %v, want 500", sample.SolarW)
	}
}

// TestTokenProactiveRefresh verifies that a token close to expiry is refreshed
// before polling the summary endpoint.
func TestTokenProactiveRefresh(t *testing.T) {
	loginCalls := 0

	loginHandler := func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loginResponse{
			AccessToken: "tok-proactive",
			ExpiresIn:   3600,
		})
	}

	srv := buildTestServer(t, loginHandler, summaryOK(100, 200, 300))
	defer srv.Close()

	c := NewClient(ClientConfig{Email: "u@example.com", Password: "pw"})
	c.http = *srv.Client()

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	// Pre-populate a nearly-expired token so refreshIfNeeded triggers re-login.
	c.token = "tok-expiring"
	c.tokenExpAt = time.Now().Add(30 * time.Second) // within 60s margin

	ctx := context.Background()
	if _, err := c.PollSummary(ctx); err != nil {
		t.Fatalf("PollSummary returned unexpected error: %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("loginCalls = %d, want 1 (proactive refresh)", loginCalls)
	}
	if c.token != "tok-proactive" {
		t.Errorf("token = %q, want \"tok-proactive\"", c.token)
	}
}

// TestPollSummary_EmptyBoxList verifies that an empty array response (no
// registered Beem box) is treated as an error rather than a zero-valued sample.
func TestPollSummary_EmptyBoxList(t *testing.T) {
	emptyListHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "[]")
	}

	srv := buildTestServer(t, loginOK("tok-empty", 3600), emptyListHandler)
	defer srv.Close()

	c := newClientForTest(ClientConfig{Email: "u@example.com", Password: "pw"}, srv)

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	ctx := context.Background()
	if _, err := c.PollSummary(ctx); err == nil {
		t.Fatal("PollSummary returned nil error for an empty box list, want an error")
	}
}

// TestFetchSummary_RequestShape verifies the client sends a POST with a
// {month, year} JSON body, matching the real API (a plain GET returns 404).
func TestFetchSummary_RequestShape(t *testing.T) {
	var gotMethod string
	var gotBody map[string]int

	summaryHandler := func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]boxSummary{{WattHour: 1, TotalDay: 2, TotalMonth: 3}})
	}

	srv := buildTestServer(t, loginOK("tok-shape", 3600), summaryHandler)
	defer srv.Close()

	c := newClientForTest(ClientConfig{Email: "u@example.com", Password: "pw"}, srv)

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	ctx := context.Background()
	if _, err := c.PollSummary(ctx); err != nil {
		t.Fatalf("PollSummary returned unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	now := time.Now()
	if gotBody["month"] != int(now.Month()) || gotBody["year"] != now.Year() {
		t.Errorf("body = %+v, want month=%d year=%d", gotBody, int(now.Month()), now.Year())
	}
}

// realWorldSummaryBody is a redacted copy of an actual Beem API
// POST /beemapp/box/summary response: a JSON array containing several
// fields (serialNumber, lastAlive, name, power, weather, ...) that
// boxSummary doesn't model. wattHour is 0 here because the capture was
// taken at night (no solar production).
const realWorldSummaryBody = `[{"boxId":45967,"serialNumber":"REDACTED","lastAlive":"2026-07-06T21:46:12.217Z","lastProduction":"2026-07-06T21:05:00.000Z","name":"Home","totalMonth":37555,"totalDay":6248,"wattHour":0,"year":2026,"month":7,"lastDbm":-51,"power":1000,"weather":null}]`

// TestPollSummary_RealWorldSummaryPayload verifies that the client correctly
// extracts solar_w/daily_wh/monthly_wh from the actual shape of a Beem API
// summary response, ignoring the fields boxSummary doesn't model.
func TestPollSummary_RealWorldSummaryPayload(t *testing.T) {
	summaryHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, realWorldSummaryBody)
	}

	srv := buildTestServer(t, loginOK("tok-real-summary", 3600), summaryHandler)
	defer srv.Close()

	c := newClientForTest(ClientConfig{Email: "u@example.com", Password: "pw"}, srv)

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	ctx := context.Background()
	sample, err := c.PollSummary(ctx)
	if err != nil {
		t.Fatalf("PollSummary returned unexpected error: %v", err)
	}
	if sample.SolarW != 0 {
		t.Errorf("SolarW = %v, want 0", sample.SolarW)
	}
	if sample.DailyWh != 6248 {
		t.Errorf("DailyWh = %v, want 6248", sample.DailyWh)
	}
	if sample.MonthlyWh != 37555 {
		t.Errorf("MonthlyWh = %v, want 37555", sample.MonthlyWh)
	}
}

// realWorldLoginBody is a redacted copy of an actual Beem API login response:
// HTTP 201 Created (not 200 OK), a raft of account fields the client doesn't
// model, and no "expiresIn" field at all.
const realWorldLoginBody = `{"lastname":"DOE","firstname":"Jane","email":"jane.doe@example.com","userId":50969,"journeyStatus":"house_filled","countryCode":"FR","toggles":[],"isVerified":true,"accessToken":"tok-real-world","phoneNumber":"+33600000000","birthday":null,"civility":"sir","motivationForBeem":"energySelfSufficient"}`

// TestLogin_201CreatedRealWorldPayload verifies that login succeeds against
// the actual shape of a Beem API response: status 201 (not 200), and a body
// containing many account fields the client doesn't care about, decoded via
// loginResponse which only extracts accessToken/expiresIn.
func TestLogin_201CreatedRealWorldPayload(t *testing.T) {
	loginHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, realWorldLoginBody)
	}

	srv := buildTestServer(t, loginHandler, summaryOK(100, 200, 300))
	defer srv.Close()

	c := NewClient(ClientConfig{Email: "u@example.com", Password: "pw"})
	c.http = *srv.Client()

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	ctx := context.Background()
	if _, err := c.PollSummary(ctx); err != nil {
		t.Fatalf("PollSummary returned unexpected error: %v", err)
	}
	if c.token != "tok-real-world" {
		t.Errorf("token = %q, want \"tok-real-world\"", c.token)
	}
}

// buildDevicesTestServer wires up login + devices routes, mirroring
// buildTestServer but for GetDevices instead of PollSummary.
func buildDevicesTestServer(t *testing.T, loginFn, devicesFn http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/beemapp/user/login", loginFn)
	mux.HandleFunc("/beemapp/devices", devicesFn)
	return httptest.NewServer(mux)
}

// devicesOK returns a valid devices handler.
func devicesOK(payload DevicesResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}
}

// patchDevicesURLs points loginURL/devicesURL at srv and returns a restore func.
func patchDevicesURLs(srv *httptest.Server) func() {
	origLogin, origDevices := loginURL, devicesURL
	loginURL = srv.URL + "/beemapp/user/login"
	devicesURL = srv.URL + "/beemapp/devices"
	return func() { loginURL = origLogin; devicesURL = origDevices }
}

// TestGetDevices_HappyPath verifies that GetDevices parses a beembox with
// solar equipment and no batteries/energy switches (this project's hardware:
// PnP kit only, no Beem Battery).
func TestGetDevices_HappyPath(t *testing.T) {
	want := DevicesResponse{
		BeemBoxes: []BeemBoxDevice{{
			ID:           45967,
			HouseID:      43122,
			Name:         "Home",
			SerialNumber: "REDACTED",
			Power:        1000,
			SolarEquipments: []SolarEquipment{{
				ID:              68465,
				Type:            "on",
				BoxID:           45967,
				Orientation:     0,
				Tilt:            30,
				PeakPower:       1000,
				GuaranteeStatus: "activated",
			}},
		}},
		Batteries:      []json.RawMessage{},
		EnergySwitches: []json.RawMessage{},
	}

	srv := buildDevicesTestServer(t, loginOK("tok-devices", 3600), devicesOK(want))
	defer srv.Close()

	c := newClientForTest(ClientConfig{Email: "u@example.com", Password: "pw"}, srv)
	defer patchDevicesURLs(srv)()

	ctx := context.Background()
	got, err := c.GetDevices(ctx)
	if err != nil {
		t.Fatalf("GetDevices returned unexpected error: %v", err)
	}
	if len(got.BeemBoxes) != 1 {
		t.Fatalf("len(BeemBoxes) = %d, want 1", len(got.BeemBoxes))
	}
	if got.BeemBoxes[0].Name != "Home" {
		t.Errorf("BeemBoxes[0].Name = %q, want \"Home\"", got.BeemBoxes[0].Name)
	}
	if len(got.BeemBoxes[0].SolarEquipments) != 1 {
		t.Fatalf("len(SolarEquipments) = %d, want 1", len(got.BeemBoxes[0].SolarEquipments))
	}
	if got.BeemBoxes[0].SolarEquipments[0].PeakPower != 1000 {
		t.Errorf("SolarEquipments[0].PeakPower = %v, want 1000", got.BeemBoxes[0].SolarEquipments[0].PeakPower)
	}
	if len(got.Batteries) != 0 {
		t.Errorf("len(Batteries) = %d, want 0 (no Beem Battery hardware)", len(got.Batteries))
	}
	if len(got.EnergySwitches) != 0 {
		t.Errorf("len(EnergySwitches) = %d, want 0 (no Beem Battery hardware)", len(got.EnergySwitches))
	}
}

// TestGetDevices_401TriggersRelogin verifies that a 401 from the devices
// endpoint causes the client to re-authenticate and retry, mirroring
// TestPollSummary_401TriggersRelogin.
func TestGetDevices_401TriggersRelogin(t *testing.T) {
	loginCalls := 0
	devicesCalls := 0

	devicesHandler := func(w http.ResponseWriter, r *http.Request) {
		devicesCalls++
		if devicesCalls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DevicesResponse{
			BeemBoxes:      []BeemBoxDevice{{ID: 1, Name: "Home"}},
			Batteries:      []json.RawMessage{},
			EnergySwitches: []json.RawMessage{},
		})
	}

	loginHandler := func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loginResponse{AccessToken: "tok-refresh", ExpiresIn: 3600})
	}

	srv := buildDevicesTestServer(t, loginHandler, devicesHandler)
	defer srv.Close()

	c := NewClient(ClientConfig{Email: "u@example.com", Password: "pw"})
	c.http = *srv.Client()
	defer patchDevicesURLs(srv)()

	ctx := context.Background()
	got, err := c.GetDevices(ctx)
	if err != nil {
		t.Fatalf("GetDevices returned unexpected error after re-login: %v", err)
	}
	if loginCalls != 2 {
		t.Errorf("loginCalls = %d, want 2 (initial + retry after 401)", loginCalls)
	}
	if devicesCalls != 2 {
		t.Errorf("devicesCalls = %d, want 2 (401 + retry)", devicesCalls)
	}
	if len(got.BeemBoxes) != 1 || got.BeemBoxes[0].Name != "Home" {
		t.Errorf("BeemBoxes = %+v, want one box named \"Home\"", got.BeemBoxes)
	}
}

// realWorldDevicesBody is a redacted copy of an actual Beem API
// GET /beemapp/devices response for this project's hardware (PnP kit only):
// one beembox with one solar equipment array, no batteries, no energy
// switches.
const realWorldDevicesBody = `{"beemboxes":[{"id":45967,"houseId":43122,"name":"Home","serialNumber":"REDACTED","power":1000,"createdAt":"2026-05-30T06:44:43.310Z","solarEquipments":[{"id":68465,"type":"on","boxId":45967,"orientation":0,"tilt":30,"peakPower":1000,"guaranteeStatus":"activated"}],"newUserNotifications":[]}],"batteries":[],"energySwitches":[]}`

// TestGetDevices_RealWorldPayload verifies that GetDevices correctly parses
// the actual shape of a Beem API devices response, including fields
// (newUserNotifications) that DevicesResponse doesn't model.
func TestGetDevices_RealWorldPayload(t *testing.T) {
	devicesHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, realWorldDevicesBody)
	}

	srv := buildDevicesTestServer(t, loginOK("tok-real-devices", 3600), devicesHandler)
	defer srv.Close()

	c := newClientForTest(ClientConfig{Email: "u@example.com", Password: "pw"}, srv)
	defer patchDevicesURLs(srv)()

	ctx := context.Background()
	got, err := c.GetDevices(ctx)
	if err != nil {
		t.Fatalf("GetDevices returned unexpected error: %v", err)
	}
	if len(got.BeemBoxes) != 1 {
		t.Fatalf("len(BeemBoxes) = %d, want 1", len(got.BeemBoxes))
	}
	box := got.BeemBoxes[0]
	if box.ID != 45967 {
		t.Errorf("BeemBoxes[0].ID = %d, want 45967", box.ID)
	}
	if box.HouseID != 43122 {
		t.Errorf("BeemBoxes[0].HouseID = %d, want 43122", box.HouseID)
	}
	if box.Power != 1000 {
		t.Errorf("BeemBoxes[0].Power = %v, want 1000", box.Power)
	}
	wantCreatedAt := time.Date(2026, 5, 30, 6, 44, 43, 310000000, time.UTC)
	if !box.CreatedAt.Equal(wantCreatedAt) {
		t.Errorf("BeemBoxes[0].CreatedAt = %v, want %v", box.CreatedAt, wantCreatedAt)
	}
	if len(box.SolarEquipments) != 1 {
		t.Fatalf("len(SolarEquipments) = %d, want 1", len(box.SolarEquipments))
	}
	if box.SolarEquipments[0].GuaranteeStatus != "activated" {
		t.Errorf("SolarEquipments[0].GuaranteeStatus = %q, want \"activated\"", box.SolarEquipments[0].GuaranteeStatus)
	}
	if len(got.Batteries) != 0 {
		t.Errorf("len(Batteries) = %d, want 0", len(got.Batteries))
	}
	if len(got.EnergySwitches) != 0 {
		t.Errorf("len(EnergySwitches) = %d, want 0", len(got.EnergySwitches))
	}
}
