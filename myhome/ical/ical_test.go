package ical

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// --- test helpers ---

type noopSink struct{}

func (noopSink) Info(int, string, ...interface{})             {}
func (noopSink) Init(logr.RuntimeInfo)                        {}
func (noopSink) Enabled(int) bool                             { return false }
func (noopSink) Error(error, string, ...interface{})          {}
func (n noopSink) WithValues(...interface{}) logr.LogSink     { return n }
func (n noopSink) WithName(string) logr.LogSink               { return n }

func noopLog() logr.Logger { return logr.New(noopSink{}) }

func openDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestFetcher(t *testing.T, mc *mqtt.RecordingMockClient) *Fetcher {
	t.Helper()
	f, err := New(noopLog(), mc, openDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return f
}

func mockHTTP(body string) func(string) (*http.Response, error) {
	return func(string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
}

// --- parseICalSlots tests ---

func TestParseICalSlots_FixtureFile(t *testing.T) {
	f, err := os.Open("testdata/basic.ics")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	// Jan 10 2024 UTC — matches the two events in the fixture
	now := time.Date(2024, 1, 10, 9, 0, 0, 0, time.UTC)
	slots, err := parseICalSlots(f, now)
	if err != nil {
		t.Fatalf("parseICalSlots: %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("expected 2 slots (today only), got %d: %+v", len(slots), slots)
	}
	// DTSTART:20240110T080000Z → 8*60=480
	if slots[0].S != 480 {
		t.Errorf("slot 0 start: got %d, want 480", slots[0].S)
	}
	// DTEND:20240110T100000Z → 10*60=600
	if slots[0].E != 600 {
		t.Errorf("slot 0 end: got %d, want 600", slots[0].E)
	}
}

func TestParseICalSlots_Empty(t *testing.T) {
	ics := "BEGIN:VCALENDAR\nEND:VCALENDAR\n"
	slots, err := parseICalSlots(strings.NewReader(ics), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slots) != 0 {
		t.Errorf("expected no slots, got %d", len(slots))
	}
}

func TestParseICalTime_Formats(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		input     string
		wantHour  int
		wantMin   int
		wantError bool
	}{
		{"20240110T083000Z", 8, 30, false},
		{"20240110T083000", 8, 30, false},
		{"20240110", 0, 0, false},
		{"bad-value", 0, 0, true},
	}
	for _, c := range cases {
		t, err := parseICalTime(c.input, loc)
		if c.wantError {
			if err == nil {
				fmt.Printf("parseICalTime(%q): expected error, got nil\n", c.input)
			}
			continue
		}
		if err != nil {
			fmt.Printf("parseICalTime(%q): unexpected error %v\n", c.input, err)
			continue
		}
		if t.Hour() != c.wantHour || t.Minute() != c.wantMin {
			fmt.Printf("parseICalTime(%q): got %02d:%02d, want %02d:%02d\n",
				c.input, t.Hour(), t.Minute(), c.wantHour, c.wantMin)
		}
	}
}

// --- integration tests ---

func TestRefreshRoom_Success(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	f := newTestFetcher(t, mc)

	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
DTSTART:20240110T080000Z
DTEND:20240110T100000Z
SUMMARY:Work
UID:uid1@test
END:VEVENT
END:VCALENDAR`

	f.httpGet = mockHTTP(ics)

	now := time.Date(2024, 1, 10, 9, 0, 0, 0, time.UTC)
	slots, err := f.fetchAndParse("http://example.com/cal.ics", now)
	if err != nil {
		t.Fatalf("fetchAndParse: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}

	ctx := context.Background()
	if err := f.persist("r1", slots); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if err := f.publish(ctx, "r1", slots); err != nil {
		t.Fatalf("publish: %v", err)
	}

	payloads := mc.Published("myhome/rooms/r1/agenda")
	if len(payloads) == 0 {
		t.Fatal("expected at least one publish")
	}
	var published []Slot
	if err := json.Unmarshal(payloads[0], &published); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(published) != 1 {
		t.Errorf("expected 1 published slot, got %d", len(published))
	}
}

func TestRefreshRoom_FallbackToCache(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	f := newTestFetcher(t, mc)

	// Seed cache
	cached := []Slot{{S: 480, E: 600}}
	data, _ := json.Marshal(cached)
	f.db.Exec(`INSERT INTO room_agenda_cache (room_id, date, slots, fetched_at) VALUES (?, ?, ?, ?)`,
		"r1", "2024-01-10", string(data), time.Now().Unix())

	// Force network error
	f.httpGet = func(string) (*http.Response, error) {
		return nil, fmt.Errorf("network down")
	}

	ctx := context.Background()
	if err := f.refreshRoom(ctx, "r1", "http://example.com/cal.ics"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mc.Published("myhome/rooms/r1/agenda")) == 0 {
		t.Error("expected fallback publish from cache")
	}
}
