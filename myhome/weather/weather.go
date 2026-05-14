package weather

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const (
	mqttTopic     = "myhome/weather/forecast"
	fetchInterval = 6 * time.Hour
	staleCutoff   = 24 * time.Hour
	slotsPerFetch = 4
	apiURLFmt     = "https://api.open-meteo.com/v1/forecast?latitude=%g&longitude=%g&hourly=temperature_2m&forecast_days=2&timezone=auto"
)

// Slot is one hourly weather data point published to devices.
type Slot struct {
	H int     `json:"h"` // hour of day (0–23)
	T float64 `json:"t"` // temperature in °C
}

// payload is what we publish to MQTT.
type payload struct {
	Slots []Slot `json:"slots"`
	Stale bool   `json:"stale,omitempty"`
}

// cacheRow mirrors the weather_cache DB table.
type cacheRow struct {
	FetchedAt int64  `db:"fetched_at"`
	Forecast  string `db:"forecast"`
	Stale     int    `db:"stale"`
}

// openMeteoResponse is the subset of the Open-Meteo API response we need.
type openMeteoResponse struct {
	Hourly struct {
		Time          []string  `json:"time"`
		Temperature2m []float64 `json:"temperature_2m"`
	} `json:"hourly"`
}

// Forecaster fetches weather from Open-Meteo, persists it, and publishes to MQTT.
type Forecaster struct {
	lat, lon float64
	log      logr.Logger
	mc       mqtt.Client
	db       *sqlx.DB
	httpGet  func(url string) (*http.Response, error) // injectable for tests
}

// New creates a Forecaster. lat and lon are decimal degrees.
func New(log logr.Logger, lat, lon float64, mc mqtt.Client, db *sqlx.DB) (*Forecaster, error) {
	if lat < -90 || lat > 90 {
		return nil, fmt.Errorf("latitude %g out of range [-90, 90]", lat)
	}
	if lon < -180 || lon > 180 {
		return nil, fmt.Errorf("longitude %g out of range [-180, 180]", lon)
	}

	f := &Forecaster{
		lat:     lat,
		lon:     lon,
		log:     log.WithName("weather.Forecaster"),
		mc:      mc,
		db:      db,
		httpGet: http.Get,
	}

	if err := f.createTable(); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *Forecaster) createTable() error {
	_, err := f.db.Exec(`
	CREATE TABLE IF NOT EXISTS weather_cache (
		fetched_at INTEGER NOT NULL,
		forecast   TEXT    NOT NULL DEFAULT '[]',
		stale      INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

// Run fetches immediately then every 6 hours. Blocks until ctx is cancelled.
func (f *Forecaster) Run(ctx context.Context) {
	f.fetchAndPublish(ctx)

	ticker := time.NewTicker(fetchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.fetchAndPublish(ctx)
		}
	}
}

// fetchAndPublish fetches, persists, and publishes. Falls back to cache on error.
func (f *Forecaster) fetchAndPublish(ctx context.Context) {
	slots, err := f.fetch()
	if err != nil {
		f.log.Error(err, "Forecast fetch failed; falling back to cache")
		f.publishFromCache(ctx)
		return
	}

	if err := f.persist(slots); err != nil {
		f.log.Error(err, "Failed to persist forecast")
	}

	f.publish(ctx, slots, false)
}

// fetch calls Open-Meteo and returns the next slotsPerFetch hourly readings.
func (f *Forecaster) fetch() ([]Slot, error) {
	url := fmt.Sprintf(apiURLFmt, f.lat, f.lon)
	resp, err := f.httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var om openMeteoResponse
	if err := json.Unmarshal(body, &om); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return distil(om, time.Now(), slotsPerFetch)
}

// distil picks the next n hourly slots at or after the current hour.
func distil(om openMeteoResponse, now time.Time, n int) ([]Slot, error) {
	times := om.Hourly.Time
	temps := om.Hourly.Temperature2m

	if len(times) == 0 || len(times) != len(temps) {
		return nil, fmt.Errorf("malformed Open-Meteo response")
	}

	nowHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())

	var slots []Slot
	for i, tStr := range times {
		t, err := time.ParseInLocation("2006-01-02T15:04", tStr, now.Location())
		if err != nil {
			continue
		}
		if !t.Before(nowHour) {
			slots = append(slots, Slot{H: t.Hour(), T: temps[i]})
			if len(slots) == n {
				break
			}
		}
	}

	if len(slots) == 0 {
		return nil, fmt.Errorf("no future slots in Open-Meteo response")
	}

	return slots, nil
}

// persist saves a fresh forecast and removes old rows.
func (f *Forecaster) persist(slots []Slot) error {
	data, err := json.Marshal(slots)
	if err != nil {
		return err
	}

	tx, err := f.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM weather_cache`); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO weather_cache (fetched_at, forecast, stale) VALUES (?, ?, 0)`,
		time.Now().Unix(), string(data),
	); err != nil {
		return err
	}

	return tx.Commit()
}

// publishFromCache reads the latest cached row and publishes it, marking stale if needed.
func (f *Forecaster) publishFromCache(ctx context.Context) {
	var row cacheRow
	err := f.db.Get(&row, `SELECT fetched_at, forecast, stale FROM weather_cache ORDER BY fetched_at DESC LIMIT 1`)
	if err == sql.ErrNoRows {
		f.log.Info("No cached forecast available")
		return
	}
	if err != nil {
		f.log.Error(err, "Failed to read forecast cache")
		return
	}

	var slots []Slot
	if err := json.Unmarshal([]byte(row.Forecast), &slots); err != nil {
		f.log.Error(err, "Failed to parse cached forecast")
		return
	}

	stale := time.Since(time.Unix(row.FetchedAt, 0)) > staleCutoff
	f.publish(ctx, slots, stale)
}

// publish serialises and publishes slots to MQTT.
func (f *Forecaster) publish(ctx context.Context, slots []Slot, stale bool) {
	p := payload{Slots: slots, Stale: stale}
	data, err := json.Marshal(p)
	if err != nil {
		f.log.Error(err, "Failed to marshal forecast")
		return
	}

	if err := f.mc.Publish(ctx, mqttTopic, data, mqtt.AtLeastOnce, true, "weather"); err != nil {
		f.log.Error(err, "Failed to publish forecast")
		return
	}

	f.log.Info("Published weather forecast", "slots", len(slots), "stale", stale)
}
