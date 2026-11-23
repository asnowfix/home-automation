package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func TestTracker_FlushEmitsDailyMinMax(t *testing.T) {
	store := newTestStorage(t)
	tr := NewSensorDailyTracker(logr.Discard(), store)
	ctx := context.Background()

	for _, v := range []float64{20.5, 18, 22} {
		if err := tr.Observe(ctx, Metric{DeviceID: "d1", Component: "temperature:0", Metric: "tC"}, v); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	if err := tr.Observe(ctx, Metric{DeviceID: "d1", Component: "x:0", Metric: "custom"}, 3); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	var got []Event
	err := tr.Flush(ctx, todayDate(), func(e Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("emitted %d events, want 4: %+v", len(got), got)
	}
	byName := map[string]string{}
	for _, e := range got {
		if e.Severity != "info" || e.Data == nil {
			t.Errorf("bad event %+v", e)
			continue
		}
		byName[e.Event] = *e.Data
	}
	want := map[string]string{
		"temperature.daily_min": "18",
		"temperature.daily_max": "22",
		"custom.daily_min":      "3", // unknown metric keeps its own name as prefix
		"custom.daily_max":      "3",
	}
	for k, v := range want {
		if byName[k] != v {
			t.Errorf("%s = %q, want %q", k, byName[k], v)
		}
	}
}

func TestTracker_FlushNilEmitAndEmitErrors(t *testing.T) {
	store := newTestStorage(t)
	tr := NewSensorDailyTracker(logr.Discard(), store)
	ctx := context.Background()
	_ = tr.Observe(ctx, Metric{DeviceID: "d", Component: "c", Metric: "lux"}, 5)

	if err := tr.Flush(ctx, todayDate(), nil); err != nil {
		t.Errorf("Flush(nil emit) = %v, want nil", err)
	}
	calls := 0
	err := tr.Flush(ctx, todayDate(), func(Event) error { calls++; return errors.New("boom") })
	if err != nil {
		t.Errorf("an emit error must be logged, not returned: %v", err)
	}
	if calls != 2 {
		t.Errorf("emit called %d times, want 2 (min and max are both attempted)", calls)
	}
}

func TestTracker_FlushClosedStoreReturnsError(t *testing.T) {
	store := newTestStorage(t)
	tr := NewSensorDailyTracker(logr.Discard(), store)
	store.Close()
	if err := tr.Flush(context.Background(), todayDate(), func(Event) error { return nil }); err == nil {
		t.Error("Flush on a closed store must return an error")
	}
}

func TestTracker_RolloverDropsYesterdayBuckets(t *testing.T) {
	store := newTestStorage(t)
	tr := NewSensorDailyTracker(logr.Discard(), store)
	y := yesterdayDate()
	tr.buckets[bucketKey("d", "c", "tC", y)] = &DayBucket{Date: y}
	tr.buckets[bucketKey("d", "c", "tC", todayDate())] = &DayBucket{Date: todayDate()}

	tr.rollover(context.Background())

	if len(tr.buckets) != 1 {
		t.Fatalf("buckets = %d, want only today's", len(tr.buckets))
	}
	if _, ok := tr.buckets[bucketKey("d", "c", "tC", todayDate())]; !ok {
		t.Error("today's bucket must be kept")
	}
}

func TestDateHelpers(t *testing.T) {
	if got, want := todayDate(), time.Now().Format("2006-01-02"); got != want {
		t.Errorf("todayDate = %s, want %s", got, want)
	}
	if todayDate() == yesterdayDate() {
		t.Error("yesterdayDate must differ from todayDate")
	}
	if got := marshalFloat(1.5); got != "1.5" {
		t.Errorf("marshalFloat = %q", got)
	}
}

func TestService_StartStopsOnCancelAndStoreAccessor(t *testing.T) {
	store := newTestStorage(t)
	svc := NewService(logr.Discard(), store, nil, nil, time.Hour)
	if svc.Store() != store {
		t.Error("Store() must return the underlying storage")
	}
	if store.DB() == nil {
		t.Error("DB() must not be nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := svc.Start(ctx); err != nil {
		t.Errorf("Start on a cancelled context = %v, want nil", err)
	}
}
