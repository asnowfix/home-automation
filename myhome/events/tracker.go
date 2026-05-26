package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type Metric struct {
	DeviceID  string
	Component string
	Metric    string // "tC", "lux", "rh", "kWh"
}

type DayBucket struct {
	Date    string
	Min     float64
	Max     float64
	Sum     float64
	Samples int64
}

type SensorDailyTracker struct {
	mu      sync.Mutex
	buckets map[string]*DayBucket // key: bucketKey(DeviceID, Component, Metric, Date)
	store   *Storage
	log     logr.Logger
}

func NewSensorDailyTracker(log logr.Logger, store *Storage) *SensorDailyTracker {
	return &SensorDailyTracker{
		buckets: make(map[string]*DayBucket),
		store:   store,
		log:     log.WithName("SensorDailyTracker"),
	}
}

func (t *SensorDailyTracker) Start(ctx context.Context) error {
	date := todayDate()
	stats, err := t.store.LoadTodayStats(ctx, date)
	if err != nil {
		t.log.Error(err, "Failed to load today stats on startup")
	} else {
		t.mu.Lock()
		for _, s := range stats {
			key := bucketKey(s.DeviceID, s.Component, s.Metric, s.Date)
			t.buckets[key] = &DayBucket{
				Date:    s.Date,
				Min:     s.MinVal,
				Max:     s.MaxVal,
				Sum:     s.SumVal,
				Samples: s.Samples,
			}
		}
		t.mu.Unlock()
		t.log.Info("Restored daily buckets from DB", "count", len(stats), "date", date)
	}

	t.midnightTimer(ctx)
	return nil
}

func (t *SensorDailyTracker) midnightTimer(ctx context.Context) {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	d := next.Sub(now)
	time.AfterFunc(d, func() {
		if ctx.Err() != nil {
			return
		}
		t.rollover(ctx)
		t.midnightTimer(ctx)
	})
}

func (t *SensorDailyTracker) rollover(ctx context.Context) {
	yesterday := yesterdayDate()
	if err := t.Flush(ctx, yesterday, nil); err != nil {
		t.log.Error(err, "Failed to flush at rollover", "date", yesterday)
	}

	t.mu.Lock()
	for k, b := range t.buckets {
		if b.Date == yesterday {
			delete(t.buckets, k)
		}
	}
	t.mu.Unlock()
}

func (t *SensorDailyTracker) Observe(ctx context.Context, m Metric, value float64) error {
	date := todayDate()
	key := bucketKey(m.DeviceID, m.Component, m.Metric, date)

	t.mu.Lock()
	b, ok := t.buckets[key]
	if !ok {
		b = &DayBucket{Date: date, Min: value, Max: value}
		t.buckets[key] = b
	}
	if value < b.Min {
		b.Min = value
	}
	if value > b.Max {
		b.Max = value
	}
	b.Sum += value
	b.Samples++
	min, max, sum, samples := b.Min, b.Max, b.Sum, b.Samples
	t.mu.Unlock()

	err := t.store.UpsertDailyStat(ctx, DailyStat{
		Date:      date,
		DeviceID:  m.DeviceID,
		Component: m.Component,
		Metric:    m.Metric,
		MinVal:    min,
		MaxVal:    max,
		SumVal:    sum,
		Samples:   samples,
		UpdatedAt: float64(time.Now().Unix()),
	})
	if err != nil {
		t.log.Error(err, "Failed to upsert daily stat", "device_id", m.DeviceID, "metric", m.Metric)
	}
	return nil
}

var metricEventPrefix = map[string]string{
	"tC":  "temperature",
	"lux": "illuminance",
	"rh":  "humidity",
	"kWh": "energy",
}

// Flush emits synthetic daily_min / daily_max event rows for the given date by
// calling emit for each stat loaded from the DB. Stats are already persisted
// continuously via Observe, so no extra DB write is needed here.
// If emit is nil, Flush returns immediately (no events are emitted).
func (t *SensorDailyTracker) Flush(ctx context.Context, date string, emit func(Event) error) error {
	if emit == nil {
		return nil
	}

	stats, err := t.store.LoadTodayStats(ctx, date)
	if err != nil {
		t.log.Error(err, "Failed to load stats for flush", "date", date)
		return err
	}

	now := float64(time.Now().Unix())
	for _, s := range stats {
		prefix, ok := metricEventPrefix[s.Metric]
		if !ok {
			prefix = s.Metric
		}

		minJSON := marshalFloat(s.MinVal)
		maxJSON := marshalFloat(s.MaxVal)

		if err := emit(Event{
			Ts:        now,
			DeviceID:  s.DeviceID,
			Component: s.Component,
			Event:     prefix + ".daily_min",
			Severity:  "info",
			Data:      &minJSON,
		}); err != nil {
			t.log.Error(err, "Failed to emit daily_min event", "device_id", s.DeviceID)
		}

		if err := emit(Event{
			Ts:        now,
			DeviceID:  s.DeviceID,
			Component: s.Component,
			Event:     prefix + ".daily_max",
			Severity:  "info",
			Data:      &maxJSON,
		}); err != nil {
			t.log.Error(err, "Failed to emit daily_max event", "device_id", s.DeviceID)
		}
	}

	return nil
}

func marshalFloat(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func bucketKey(deviceID, component, metric, date string) string {
	return fmt.Sprintf("%s:%s:%s:%s", deviceID, component, metric, date)
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}

func yesterdayDate() string {
	return time.Now().Add(-24 * time.Hour).Format("2006-01-02")
}
