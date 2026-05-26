package events

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
)

func TestTrackerObserveAndRestore(t *testing.T) {
	ctx := context.Background()
	store, err := NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	tracker := NewSensorDailyTracker(logr.Discard(), store)

	m := Metric{DeviceID: "dev-1", Component: "temperature:0", Metric: "tC"}

	if err := tracker.Observe(ctx, m, 20.0); err != nil {
		t.Fatalf("Observe 20.0: %v", err)
	}
	if err := tracker.Observe(ctx, m, 15.0); err != nil {
		t.Fatalf("Observe 15.0: %v", err)
	}
	if err := tracker.Observe(ctx, m, 25.0); err != nil {
		t.Fatalf("Observe 25.0: %v", err)
	}

	key := bucketKey(m.DeviceID, m.Component, m.Metric, todayDate())
	tracker.mu.Lock()
	b := tracker.buckets[key]
	tracker.mu.Unlock()

	if b == nil {
		t.Fatal("bucket not found after Observe")
	}
	if b.Min != 15.0 {
		t.Errorf("Min: want 15.0, got %f", b.Min)
	}
	if b.Max != 25.0 {
		t.Errorf("Max: want 25.0, got %f", b.Max)
	}
	if b.Sum != 60.0 {
		t.Errorf("Sum: want 60.0, got %f", b.Sum)
	}
	if b.Samples != 3 {
		t.Errorf("Samples: want 3, got %d", b.Samples)
	}

	// Simulate restart: new tracker, same store — should restore from DB
	tracker2 := NewSensorDailyTracker(logr.Discard(), store)
	if err := tracker2.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	key2 := bucketKey(m.DeviceID, m.Component, m.Metric, todayDate())
	tracker2.mu.Lock()
	b2 := tracker2.buckets[key2]
	tracker2.mu.Unlock()

	if b2 == nil {
		t.Fatal("bucket not restored after restart")
	}
	if b2.Min != 15.0 {
		t.Errorf("Restored Min: want 15.0, got %f", b2.Min)
	}
	if b2.Max != 25.0 {
		t.Errorf("Restored Max: want 25.0, got %f", b2.Max)
	}
	if b2.Samples != 3 {
		t.Errorf("Restored Samples: want 3, got %d", b2.Samples)
	}
}
