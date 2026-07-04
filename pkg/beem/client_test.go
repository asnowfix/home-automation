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

// summaryOK returns a valid summary handler.
func summaryOK(solarW, dailyWh, monthlyWh float64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summaryResponse{
			InstantPower: solarW,
			DayEnergy:    dailyWh,
			MonthEnergy:  monthlyWh,
		})
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
		json.NewEncoder(w).Encode(summaryResponse{
			InstantPower: 500,
			DayEnergy:    1000,
			MonthEnergy:  5000,
		})
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
