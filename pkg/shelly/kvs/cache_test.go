package kvs

import (
	"context"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/asnowfix/home-automation/pkg/shelly/typestest"
	"github.com/go-logr/logr"
)

// resetKvsCache clears all package-level cache/revision state between tests,
// since FakeDevice instances default to an empty Id() and would otherwise
// share cache entries across test cases within the same test binary.
func resetKvsCache() {
	cacheMu.Lock()
	cache = map[string]cacheEntry{}
	cacheMu.Unlock()

	revMu.Lock()
	lastSeenRev = map[string]uint32{}
	pendingSelf = map[string]uint32{}
	revMu.Unlock()
}

func TestGetValueCachesResult(t *testing.T) {
	resetKvsCache()
	d := typestest.NewFakeDevice()
	d.IdValue = "device-cache-1"
	d.SetResult(string(Get), &GetResponse{Value: "17"})

	if _, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Change the canned result; a cache hit should still return the first value.
	d.SetResult(string(Get), &GetResponse{Value: "99"})
	got, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value != "17" {
		t.Errorf("expected cached value 17, got %q", got.Value)
	}
	if len(d.Calls) != 1 {
		t.Errorf("expected only 1 device call (second Get should hit cache), got %d", len(d.Calls))
	}
}

func TestSetKeyValueInvalidatesCache(t *testing.T) {
	resetKvsCache()
	d := typestest.NewFakeDevice()
	d.IdValue = "device-cache-2"
	d.SetResult(string(Get), &GetResponse{Value: "17"})

	if _, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d.SetResult(string(Set), &Status{})
	if _, err := SetKeyValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k", "18"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d.SetResult(string(Get), &GetResponse{Value: "18"})
	got, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value != "18" {
		t.Errorf("expected fresh value 18 after invalidation, got %q", got.Value)
	}
	if len(d.Calls) != 3 { // Get, Set, Get
		t.Errorf("expected 3 device calls, got %d", len(d.Calls))
	}
}

func TestObserveRevisionSelfWriteDoesNotWipeOtherKeys(t *testing.T) {
	resetKvsCache()
	d := typestest.NewFakeDevice()
	d.IdValue = "device-cache-3"

	// Prime two cached keys.
	d.SetResult(string(Get), &GetResponse{Value: "a-1"})
	if _, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d.SetResult(string(Get), &GetResponse{Value: "b-1"})
	if _, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Establish baseline revision.
	ObserveRevision(logr.Discard(), d.IdValue, 5)

	// Self-caused write to key "a": invalidates "a" directly, and bumps the
	// expected revision by one.
	d.SetResult(string(Set), &Status{})
	if _, err := SetKeyValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "a", "a-2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Device reports the matching revision bump: should NOT wipe "b".
	ObserveRevision(logr.Discard(), d.IdValue, 6)

	if _, ok := cacheGet(d.IdValue, "b"); !ok {
		t.Error("expected key 'b' to remain cached after a self-caused revision bump")
	}
}

func TestObserveRevisionExternalChangeWipesDevice(t *testing.T) {
	resetKvsCache()
	d := typestest.NewFakeDevice()
	d.IdValue = "device-cache-4"

	d.SetResult(string(Get), &GetResponse{Value: "a-1"})
	if _, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ObserveRevision(logr.Discard(), d.IdValue, 5)

	// Revision bumps with no write we made: an external change.
	ObserveRevision(logr.Discard(), d.IdValue, 6)

	if _, ok := cacheGet(d.IdValue, "a"); ok {
		t.Error("expected key 'a' to be evicted after an externally-caused revision bump")
	}
}
