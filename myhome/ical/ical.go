package ical

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const (
	refreshInterval = 24 * time.Hour
	mqttTopicFmt    = "myhome/rooms/%s/agenda"
)

// Slot is a busy period in minutes-since-midnight.
type Slot struct {
	S int `json:"s"` // start (0–1439)
	E int `json:"e"` // end   (0–1439)
}

// agendaCacheRow mirrors the room_agenda_cache DB table.
type agendaCacheRow struct {
	RoomID    string `db:"room_id"`
	Date      string `db:"date"`
	Slots     string `db:"slots"`
	FetchedAt int64  `db:"fetched_at"`
}

// Fetcher fetches, parses, caches, and publishes per-room iCal agendas.
type Fetcher struct {
	log     logr.Logger
	mc      mqtt.Client
	db      *sqlx.DB
	httpGet func(url string) (*http.Response, error)
}

// New creates a Fetcher.
func New(log logr.Logger, mc mqtt.Client, db *sqlx.DB) (*Fetcher, error) {
	f := &Fetcher{
		log:     log.WithName("ical.Fetcher"),
		mc:      mc,
		db:      db,
		httpGet: http.Get,
	}
	if err := f.createTable(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Fetcher) createTable() error {
	_, err := f.db.Exec(`
	CREATE TABLE IF NOT EXISTS room_agenda_cache (
		room_id    TEXT    NOT NULL PRIMARY KEY,
		date       TEXT    NOT NULL,
		slots      TEXT    NOT NULL DEFAULT '[]',
		fetched_at INTEGER NOT NULL
	)`)
	return err
}

// Refresh fetches and publishes agendas for all rooms with iCal URLs.
// rooms is a map of roomID -> iCalURL; empty URLs are skipped.
func (f *Fetcher) Refresh(ctx context.Context, rooms map[string]string) {
	for roomID, url := range rooms {
		if url == "" {
			continue
		}
		if err := f.refreshRoom(ctx, roomID, url); err != nil {
			f.log.Error(err, "Failed to refresh agenda", "room_id", roomID)
		}
	}
}

// Run fetches all room agendas immediately then every 24 hours.
// roomsFn is called each iteration to get the current room→URL map.
// Blocks until ctx is cancelled.
func (f *Fetcher) Run(ctx context.Context, roomsFn func() map[string]string) {
	f.Refresh(ctx, roomsFn())

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.Refresh(ctx, roomsFn())
		}
	}
}

func (f *Fetcher) refreshRoom(ctx context.Context, roomID, url string) error {
	slots, err := f.fetchAndParse(url, time.Now())
	if err != nil {
		f.log.Error(err, "iCal fetch failed; serving from cache", "room_id", roomID)
		return f.publishFromCache(ctx, roomID)
	}

	if err := f.persist(roomID, slots); err != nil {
		f.log.Error(err, "Failed to persist agenda", "room_id", roomID)
	}

	return f.publish(ctx, roomID, slots)
}

// fetchAndParse downloads an iCal URL and returns today's busy slots.
func (f *Fetcher) fetchAndParse(url string, now time.Time) ([]Slot, error) {
	resp, err := f.httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ical status %d", resp.StatusCode)
	}

	return parseICalSlots(resp.Body, now)
}

// parseICalSlots extracts today's busy slots from an iCal stream.
// All events in the calendar are treated as busy; no PARTSTAT filtering needed
// since the URL is a per-room shared calendar that only contains relevant events.
func parseICalSlots(r io.Reader, now time.Time) ([]Slot, error) {
	todayDate := now.Format("20060102")
	loc := now.Location()

	var slots []Slot
	var inEvent bool
	var dtstart, dtend string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")

		switch {
		case line == "BEGIN:VEVENT":
			inEvent = true
			dtstart, dtend = "", ""

		case line == "END:VEVENT":
			if inEvent && dtstart != "" && dtend != "" {
				s, e, err := parseEventSlot(dtstart, dtend, todayDate, loc)
				if err == nil {
					slots = append(slots, Slot{S: s, E: e})
				}
			}
			inEvent = false

		case inEvent && strings.HasPrefix(line, "DTSTART"):
			dtstart = extractValue(line)

		case inEvent && strings.HasPrefix(line, "DTEND"):
			dtend = extractValue(line)
		}
	}

	return slots, scanner.Err()
}

// extractValue returns the value from a "KEY:value" or "KEY;params:value" iCal line.
func extractValue(line string) string {
	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

// parseEventSlot converts DTSTART/DTEND to minutes-since-midnight for today.
// Returns an error if the event does not fall on today's date.
func parseEventSlot(dtstart, dtend, todayDate string, loc *time.Location) (int, int, error) {
	s, err := parseICalTime(dtstart, loc)
	if err != nil {
		return 0, 0, err
	}
	e, err := parseICalTime(dtend, loc)
	if err != nil {
		return 0, 0, err
	}

	if s.Format("20060102") != todayDate {
		return 0, 0, fmt.Errorf("event not today")
	}

	return s.Hour()*60 + s.Minute(), e.Hour()*60 + e.Minute(), nil
}

// parseICalTime parses DTSTART/DTEND values.
// Handles: 20060102T150405Z (UTC), 20060102T150405 (local), 20060102 (all-day).
func parseICalTime(v string, loc *time.Location) (time.Time, error) {
	formats := []struct {
		layout string
		isUTC  bool
	}{
		{"20060102T150405Z", true},
		{"20060102T150405", false},
		{"20060102", false},
	}

	for _, f := range formats {
		t, err := time.Parse(f.layout, v)
		if err != nil {
			continue
		}
		if f.isUTC {
			return t.In(loc), nil
		}
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
	}

	return time.Time{}, fmt.Errorf("unrecognised iCal time %q", v)
}

func (f *Fetcher) persist(roomID string, slots []Slot) error {
	data, err := json.Marshal(slots)
	if err != nil {
		return err
	}
	_, err = f.db.Exec(`
	INSERT INTO room_agenda_cache (room_id, date, slots, fetched_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(room_id) DO UPDATE SET
		date       = excluded.date,
		slots      = excluded.slots,
		fetched_at = excluded.fetched_at`,
		roomID, time.Now().Format("2006-01-02"), string(data), time.Now().Unix())
	return err
}

func (f *Fetcher) publishFromCache(ctx context.Context, roomID string) error {
	var row agendaCacheRow
	err := f.db.Get(&row, `SELECT room_id, date, slots, fetched_at FROM room_agenda_cache WHERE room_id = ?`, roomID)
	if err == sql.ErrNoRows {
		f.log.Info("No cached agenda", "room_id", roomID)
		return nil
	}
	if err != nil {
		return err
	}

	var slots []Slot
	if err := json.Unmarshal([]byte(row.Slots), &slots); err != nil {
		return err
	}

	return f.publish(ctx, roomID, slots)
}

func (f *Fetcher) publish(ctx context.Context, roomID string, slots []Slot) error {
	data, err := json.Marshal(slots)
	if err != nil {
		return err
	}

	topic := fmt.Sprintf(mqttTopicFmt, roomID)
	if err := f.mc.Publish(ctx, topic, data, mqtt.AtLeastOnce, true, "ical"); err != nil {
		return err
	}

	f.log.Info("Published room agenda", "room_id", roomID, "slots", len(slots))
	return nil
}
