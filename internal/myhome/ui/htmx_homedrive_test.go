package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/myhome/homedrive"
	"github.com/go-logr/logr"
)

// The documents are the myhome/homedrive fixtures, built from the contract.
const driveFixtureDir = "../../../myhome/homedrive/testdata"

var driveT0 = time.Date(2026, 11, 15, 14, 32, 10, 0, time.UTC) // 10 s after the fixtures' ts

func driveDoc(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(driveFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// driveSource is a real homedrive.Cache fed through its MQTT handler (no
// broker), counting Snapshot calls.
type driveSource struct {
	*homedrive.Cache
	calls int
}

func (s *driveSource) Snapshot(now time.Time) []homedrive.View {
	s.calls++
	return s.Cache.Snapshot(now)
}

func newDriveSource(t *testing.T, docs ...[]byte) *driveSource {
	t.Helper()
	c := homedrive.NewCache(homedrive.Config{Log: logr.Discard(), Now: func() time.Time { return driveT0 }})
	for _, d := range docs {
		if err := c.HandleMessage("homedrive/testhost/testuser/status", d, ""); err != nil {
			t.Fatal(err)
		}
	}
	return &driveSource{Cache: c}
}

func renderDrive(t *testing.T, src HomedriveSource, now time.Time) string {
	t.Helper()
	h := NewHTMXHandler(context.Background(), logr.Discard(), nil, nil, nil, nil)
	h.SetHomedriveSource(src)
	h.now = func() time.Time { return now }
	rec := httptest.NewRecorder()
	h.HomedrivePanel(rec, httptest.NewRequest("GET", "/htmx/homedrive", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func TestHomedrivePanel_States(t *testing.T) {
	running := string(driveDoc(t, "running.json"))
	withState := func(s string) []byte {
		return []byte(strings.Replace(running, `"state": "running"`, `"state": "`+s+`"`, 1))
	}
	tests := []struct {
		name  string
		doc   []byte
		after time.Duration // clock advance since the cache started
		class string
		text  string
		want  []string
	}{
		{"online", driveDoc(t, "running.json"), 0, "is-success", "Online", []string{"testuser@testhost", "Status 10 s ago (14:32)", "Version v0.2.0", "up 3 d 2 h", "State running", "Google auth ok (refreshed 13:40)"}},
		{"starting", withState("starting"), 0, "is-light", "Starting", nil},
		{"paused", withState("paused"), 0, "is-light", "Paused", nil},
		{"stale", driveDoc(t, "running.json"), 2 * time.Minute, "is-light", "Stale", []string{"Status 2 min ago"}},
		{"offline", driveDoc(t, "running.json"), 16 * time.Minute, "is-danger", "Offline", []string{"Status 16 min ago", "Last error push"}},
		{"state error", driveDoc(t, "error.json"), 0, "is-danger", "Error", []string{"startup"}},
		{"auth problem", driveDoc(t, "auth_token_invalid.json"), 0, "is-danger", "Auth problem", []string{"Google auth token_invalid", "has-text-danger", "Dry run"}},
		{"stale with a state error", driveDoc(t, "error.json"), 2 * time.Minute, "is-danger", "Error (stale)", nil},
		{"stale with an auth problem", driveDoc(t, "auth_token_invalid.json"), 2 * time.Minute, "is-danger", "Auth problem (stale)", nil},
		{"unsupported", driveDoc(t, "schema_3_0.json"), 0, "is-light", "Unsupported schema", []string{"Unsupported homedrive status schema", "schema 3.0"}},
		{"degraded", driveDoc(t, "schema_2_7_extra.json"), 0, "is-warning", "quota_blocked", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newDriveSource(t, tt.doc)
			html := renderDrive(t, src, driveT0.Add(tt.after))
			if !strings.Contains(html, `tag `+tt.class+`">`+tt.text+`<`) {
				t.Errorf("pill %s %q not found in:\n%s", tt.class, tt.text, html)
			}
			for _, w := range tt.want {
				if !strings.Contains(html, w) {
					t.Errorf("missing %q", w)
				}
			}
			if strings.Contains(html, "title=") {
				t.Error("information must not be hover-only (title=)")
			}
			if tt.name == "unsupported" && (strings.Contains(html, "Uploaded") || strings.Contains(html, "Google auth")) {
				t.Error("an unsupported document's contents must not be shown")
			}
			if src.calls != 1 {
				t.Errorf("Snapshot called %d times, want 1", src.calls)
			}
		})
	}
}

func TestHomedrivePanel_Windows(t *testing.T) {
	html := renderDrive(t, newDriveSource(t, driveDoc(t, "running.json")), driveT0)
	nb := " "
	for _, w := range []string{
		`<th scope="col">1 h</th><th scope="col">24 h</th><th scope="col">7 d</th>`,
		`<th scope="row">Uploaded</th><td>3 · 1.2 MB</td><td>140 · 2.1 GB</td><td>900 · 9.4 GB</td>`,
		`<th scope="row">Downloaded</th><td>0 · 0 B</td><td>12 · 30.0 MB</td><td>80 · 410.0 MB</td>`,
		`<th scope="row">Errors</th><td>0</td><td>2</td><td>5</td>`,
		`<th scope="row">Conflicts</th><td>0</td><td>0</td><td>1</td>`,
		`<th scope="row">Starts</th><td>1</td><td>1</td><td>2</td>`,
		"Pending uploads 0 · Last push 14:20 · Last pull 14:05 · Last poll 14:32",
		"Last bisync 13:00 · ok · 42 s · 3 files",
		"Local 12" + nb + "345 files · 81.0 GB · Quota 63 %",
	} {
		if !strings.Contains(html, w) {
			t.Errorf("missing %q", w)
		}
	}
}

func TestHomedrivePanel_NullRendersDashes(t *testing.T) {
	html := renderDrive(t, newDriveSource(t, driveDoc(t, "error.json")), driveT0)
	for _, w := range []string{
		"Pending uploads 0 · Last push -- · Last pull -- · Last poll --",
		"Last bisync --",
		"Local -- · Quota --",
		"Google auth unknown",
	} {
		if !strings.Contains(html, w) {
			t.Errorf("missing %q", w)
		}
	}
	html = renderDrive(t, newDriveSource(t, driveDoc(t, "schema_2_7_extra.json")), driveT0)
	if !strings.Contains(html, `<th scope="row">Uploaded</th><td>1 · 10 B</td><td>--</td><td>1 · 10 B</td>`) {
		t.Errorf("a null window must render --:\n%s", html)
	}
	if !strings.Contains(html, "Last error none") {
		t.Error("a null last_error is shown as none")
	}
}

func TestHomedrivePanel_ErrorTextTruncatedAndEscaped(t *testing.T) {
	doc := strings.Replace(string(driveDoc(t, "running.json")),
		"googleapi: Error 403: rateLimitExceeded for /data/photos/2026/img_0001.jpg",
		"<script>alert(1)</script> "+strings.Repeat("x", 300), 1)
	html := renderDrive(t, newDriveSource(t, []byte(doc)), driveT0)
	if strings.Contains(html, "<script>") {
		t.Error("error text must be escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") || !strings.Contains(html, "…") {
		t.Error("error text must be shown, truncated with an ellipsis")
	}
	if strings.Count(html, "x") > 200 {
		t.Errorf("error text not truncated to %d chars", maxErrorDisplayLen)
	}
}

func TestHomedrivePanel_ClockSkewAndProblem(t *testing.T) {
	future := strings.Replace(string(driveDoc(t, "running.json")), "2026-11-15T14:32:00Z", "2026-11-15T15:32:00Z", 1)
	html := renderDrive(t, newDriveSource(t, []byte(future)), driveT0)
	if !strings.Contains(html, "Clock skew") {
		t.Error("clock skew tag missing")
	}
	src := newDriveSource(t, driveDoc(t, "running.json"), []byte(`{nope`))
	html = renderDrive(t, src, driveT0)
	if !strings.Contains(html, "last status payload was invalid") || !strings.Contains(html, "Online") {
		t.Error("a rejected payload is flagged while the last good document stays shown")
	}
}

func TestHomedrivePanel_NeverSeenAndNilSource(t *testing.T) {
	html := renderDrive(t, newDriveSource(t), driveT0)
	if !strings.Contains(html, "Never seen") || strings.Contains(html, "mh-drive-table") {
		t.Errorf("never seen state: %s", html)
	}
	if html = renderDrive(t, nil, driveT0); !strings.Contains(html, "Drive sync status not available") {
		t.Errorf("nil source: %s", html)
	}
}

func TestHomedrivePanel_Route(t *testing.T) {
	ctx := context.WithValue(context.Background(), global.VersionKey, "9.9.9-test")
	src := newDriveSource(t, driveDoc(t, "running.json"))
	mux := newMuxWithHomedrive(ctx, logr.Discard(), nil, nil, nil, nil, "", nil, nil, src)
	rec := get(mux, "/htmx/homedrive")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "testuser@testhost") {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	post := httptest.NewRecorder()
	mux.ServeHTTP(post, httptest.NewRequest("POST", "/htmx/homedrive", nil))
	if post.Code == http.StatusOK || strings.Contains(post.Body.String(), "testuser@testhost") {
		t.Errorf("only GET serves the fragment, POST = %d", post.Code)
	}
	// without a source the route still exists and says so
	rec = get(testMux(), "/htmx/homedrive")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "not available") {
		t.Errorf("nil source: %d %s", rec.Code, rec.Body)
	}
}

func TestShell_HomedriveBlockInStatusSheet(t *testing.T) {
	html := get(testMux(), "/").Body.String()
	i := strings.Index(html, `id="status-sheet"`)
	if i < 0 {
		t.Fatal("status sheet has no id")
	}
	sheet := html[i:]
	for _, w := range []string{
		`hx-get="/htmx/homedrive"`,
		`hx-trigger="load, refresh, every 30s [kiosk.online &amp;&amp; kiosk.statusOpen()]"`,
	} {
		if !strings.Contains(sheet, w) && !strings.Contains(sheet, strings.ReplaceAll(w, "&amp;", "&")) {
			t.Errorf("status sheet lacks %s", w)
		}
	}
	js, err := os.ReadFile("static/myhome.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), "statusOpen: function()") {
		t.Error("kiosk.statusOpen missing")
	}
}

func TestDriveFormatting(t *testing.T) {
	now := time.Date(2026, 11, 15, 14, 32, 0, 0, time.UTC)
	tests := []struct{ got, want string }{
		{ago(-time.Second), "0 s ago"},
		{ago(12 * time.Second), "12 s ago"},
		{duration(500 * time.Millisecond), "500 ms"},
		{duration(0), "0 s"},
		{duration(3 * time.Minute), "3 min"},
		{duration(2*time.Hour + 10*time.Minute), "2 h 10 min"},
		{duration(2 * time.Hour), "2 h"},
		{duration(3*24*time.Hour + 4*time.Hour), "3 d 4 h"},
		{duration(2 * 24 * time.Hour), "2 d"},
		{bytesText(0), "0 B"},
		{bytesText(999), "999 B"},
		{bytesText(1000), "1.0 kB"},
		{bytesText(1_200_000), "1.2 MB"},
		{bytesText(5_000_000_000_000_000), "5000.0 TB"},
		{group(0), "0"},
		{group(1234567), "1 234 567"},
		{group(-1234), "-1 234"},
		{clock(time.Date(2026, 11, 15, 9, 5, 0, 0, time.UTC), now), "09:05"},
		{clock(time.Date(2026, 11, 14, 9, 5, 0, 0, time.UTC), now), "14 Nov 09:05"},
		{optClock(nil, now), "--"},
		{lastErrorText(nil, now), "none"},
		{lastErrorText(&homedrive.LastError{}, now), "--"},
		{authText(nil, now), "--"},
		{bisyncText(&homedrive.Bisync{Result: "error", Error: "boom", DurationMs: 900}, now), "-- · error · 900 ms · 0 files · boom"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestDriveCard_UptimeFallbacksAndPill(t *testing.T) {
	ts := homedrive.Time{Time: time.Date(2026, 11, 15, 14, 32, 0, 0, time.UTC)}
	started := homedrive.Time{Time: ts.Add(-90 * time.Minute)}
	st := &homedrive.Status{Schema: "2.0", Ts: ts, StartedAt: &started, State: homedrive.StateRunning}
	if d, ok := uptime(st); !ok || d != 90*time.Minute {
		t.Errorf("uptime from started_at = %v %v", d, ok)
	}
	if _, ok := uptime(&homedrive.Status{}); ok {
		t.Error("no uptime without started_at/uptime_s")
	}
	c := newDriveCard(homedrive.View{Host: "h", User: "u", Liveness: homedrive.Online, LastTs: ts.Time}, ts.Time)
	if c.PillText != "Unknown" || c.PillClass != "is-light" {
		t.Errorf("view without a status: %s %s", c.PillClass, c.PillText)
	}
	// an offline instance does not claim an uptime
	c = newDriveCard(homedrive.View{Host: "h", User: "u", Liveness: homedrive.Offline, LastTs: ts.Time, Status: st}, ts.Time.Add(time.Hour))
	if c.Uptime != "--" || c.PillText != "Offline" {
		t.Errorf("offline card = %+v", c)
	}
}
