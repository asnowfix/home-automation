package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (noopSink) Info(int, string, ...interface{})            {}
func (noopSink) Init(logr.RuntimeInfo)                  {}
func (noopSink) Enabled(int) bool                       { return false }
func (noopSink) Error(error, string, ...interface{})    {}
func (n noopSink) WithValues(...interface{}) logr.LogSink { return n }
func (n noopSink) WithName(string) logr.LogSink         { return n }

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

func newTestForecaster(t *testing.T, mc *mqtt.RecordingMockClient) *Forecaster {
	t.Helper()
	f, err := New(noopLog(), 48.85, 2.35, mc, openDB(t))
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

func buildAPIResponse(date string, startHour, count int, baseTemp float64) string {
	var times []string
	var temps []float64
	for i := 0; i < count; i++ {
		h := (startHour + i) % 24
		times = append(times, fmt.Sprintf("%sT%02d:00", date, h))
		temps = append(temps, baseTemp+float64(i)*0.5)
	}
	tb, _ := json.Marshal(times)
	vb, _ := json.Marshal(temps)
	return fmt.Sprintf(`{"hourly":{"time":%s,"temperature_2m":%s}}`, tb, vb)
}

// --- tests ---

func TestNew_InvalidCoords(t *testing.T) {
	db := openDB(t)
	mc := mqtt.NewRecordingMockClient()
	cases := []struct{ lat, lon float64 }{
		{-91, 0}, {91, 0}, {0, -181}, {0, 181},
	}
	for _, c := range cases {
		if _, err := New(noopLog(), c.lat, c.lon, mc, db); err == nil {
			t.Errorf("New(%g,%g): want error, got nil", c.lat, c.lon)
		}
	}
}

func TestDistil_PicksNextSlots(t *testing.T) {
	now := time.Date(2024, 1, 10, 9, 45, 0, 0, time.UTC)
	body := buildAPIResponse("2024-01-10", 0, 24, 3.0)
	var om openMeteoResponse
	json.Unmarshal([]byte(body), &om)

	slots, err := distil(om, now, 4)
	if err != nil {
		t.Fatalf("distil: %v", err)
	}
	if len(slots) != 4 {
		t.Fatalf("expected 4 slots, got %d", len(slots))
	}
	if slots[0].H != 9 {
		t.Errorf("first slot hour: got %d, want 9", slots[0].H)
	}
}

func TestDistil_MalformedResponse(t *testing.T) {
	_, err := distil(openMeteoResponse{}, time.Now(), 4)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestDistil_MismatchedLengths(t *testing.T) {
	var om openMeteoResponse
	om.Hourly.Time = []string{"2024-01-10T10:00"}
	om.Hourly.Temperature2m = []float64{3.0, 4.0}
	_, err := distil(om, time.Now(), 4)
	if err == nil {
		t.Error("expected error for mismatched lengths")
	}
}

func TestFetchAndPublish_Success(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	f := newTestForecaster(t, mc)

	now := time.Now()
	f.httpGet = mockHTTP(buildAPIResponse(now.Format("2006-01-02"), now.Hour(), 24, 5.0))
	f.fetchAndPublish(context.Background())

	payloads := mc.Published(mqttTopic)
	if len(payloads) == 0 {
		t.Fatal("expected at least one publish")
	}
	var p payload
	if err := json.Unmarshal(payloads[0], &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(p.Slots) == 0 {
		t.Error("expected non-empty slots")
	}
	if p.Stale {
		t.Error("fresh fetch should not be stale")
	}
}

func TestFetchAndPublish_FallbackToCache(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	f := newTestForecaster(t, mc)

	cached := []Slot{{H: 10, T: 4.0}, {H: 11, T: 3.5}}
	data, _ := json.Marshal(cached)
	f.db.Exec(`INSERT INTO weather_cache (fetched_at, forecast, stale) VALUES (?, ?, 0)`,
		time.Now().Unix(), string(data))

	f.httpGet = func(string) (*http.Response, error) { return nil, fmt.Errorf("network down") }
	f.fetchAndPublish(context.Background())

	if len(mc.Published(mqttTopic)) == 0 {
		t.Fatal("expected fallback publish from cache")
	}
}

func TestFetchAndPublish_StaleCache(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	f := newTestForecaster(t, mc)

	cached := []Slot{{H: 8, T: 2.0}}
	data, _ := json.Marshal(cached)
	f.db.Exec(`INSERT INTO weather_cache (fetched_at, forecast, stale) VALUES (?, ?, 0)`,
		time.Now().Add(-25*time.Hour).Unix(), string(data))

	f.httpGet = func(string) (*http.Response, error) { return nil, fmt.Errorf("still down") }
	f.fetchAndPublish(context.Background())

	payloads := mc.Published(mqttTopic)
	if len(payloads) == 0 {
		t.Fatal("expected stale publish")
	}
	var p payload
	json.Unmarshal(payloads[0], &p)
	if !p.Stale {
		t.Error("expected stale=true for >24h old cache")
	}
}

func TestPersist_ReplacesOldRows(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	f := newTestForecaster(t, mc)

	old := []Slot{{H: 1, T: 1.0}}
	data, _ := json.Marshal(old)
	f.db.Exec(`INSERT INTO weather_cache (fetched_at, forecast, stale) VALUES (?, ?, 0)`, 1, string(data))

	fresh := []Slot{{H: 10, T: 5.0}, {H: 11, T: 4.5}}
	if err := f.persist(fresh); err != nil {
		t.Fatalf("persist: %v", err)
	}

	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM weather_cache`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row after persist, got %d", count)
	}
}
