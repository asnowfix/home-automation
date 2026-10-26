package fetchproxy

import "testing"

func sampleSubscription() Subscription {
	return Subscription{
		DeviceID: "pump-1",
		Name:     "forecast",
		URL:      "https://api.open-meteo.com/v1/forecast?hourly=temperature_2m",
		Headers: map[string]string{
			"Accept":       "application/json",
			"X-My-Header":  "value",
			"Another-One":  "z",
			"Zzz-Last-Key": "a",
		},
		Transform:       "function(body){return {t: JSON.parse(body).hourly.temperature_2m[0]};}",
		IntervalSeconds: 3600,
		Topic:           "myhome/fetch/pump-1/forecast",
	}
}

// TestChangeHashStableAcrossCalls proves the hash is a pure function of the
// subscription's content: calling it repeatedly on an identical value never
// changes the result.
func TestChangeHashStableAcrossCalls(t *testing.T) {
	sub := sampleSubscription()
	first, err := ChangeHash(sub)
	if err != nil {
		t.Fatalf("ChangeHash: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := ChangeHash(sub)
		if err != nil {
			t.Fatalf("ChangeHash: %v", err)
		}
		if got != first {
			t.Fatalf("ChangeHash not stable across calls: %q vs %q", first, got)
		}
	}
}

// TestChangeHashStableAcrossMapIteration is the regression guard called out
// explicitly in #465: hashing a map[string]string directly (e.g. via a
// hand-rolled range loop or fmt.Sprintf) gives a different result depending
// on Go's deliberately randomised map iteration order. Building the same
// logical header set via different insertion sequences must hash identically
// — that is what canonicalisation through encoding/json (which sorts map
// keys) buys us.
func TestChangeHashStableAcrossMapIteration(t *testing.T) {
	base := sampleSubscription()

	// Same key/value pairs, built via a different insertion order and a
	// different map literal each time. If canonicalisation were broken this
	// would be the test that catches it (though, being a hash-of-sorted-JSON
	// approach, it is deterministic on every single run regardless of map
	// internals — there is no iteration-order dependency to "get lucky" on).
	variant := base
	variant.Headers = map[string]string{
		"Zzz-Last-Key": "a",
		"Another-One":  "z",
		"X-My-Header":  "value",
		"Accept":       "application/json",
	}

	h1, err := ChangeHash(base)
	if err != nil {
		t.Fatalf("ChangeHash(base): %v", err)
	}
	h2, err := ChangeHash(variant)
	if err != nil {
		t.Fatalf("ChangeHash(variant): %v", err)
	}
	if h1 != h2 {
		t.Fatalf("ChangeHash depends on header map insertion order: %q vs %q", h1, h2)
	}
}

// TestChangeHashNilVsEmptyHeaders proves nil and an empty map hash the same,
// so a subscription round-tripped through JSON (where an absent "headers"
// key decodes to nil) does not spuriously appear "changed".
func TestChangeHashNilVsEmptyHeaders(t *testing.T) {
	sub := sampleSubscription()
	sub.Headers = nil
	withNil, err := ChangeHash(sub)
	if err != nil {
		t.Fatalf("ChangeHash(nil headers): %v", err)
	}
	sub.Headers = map[string]string{}
	withEmpty, err := ChangeHash(sub)
	if err != nil {
		t.Fatalf("ChangeHash(empty headers): %v", err)
	}
	if withNil != withEmpty {
		t.Fatalf("nil and empty headers hash differently: %q vs %q", withNil, withEmpty)
	}
}

// TestChangeHashDetectsEveryField proves the change-hash covers all five
// fields named in #465 — URL, headers, transform, interval, topic — not just
// URL+headers+transform. A device re-subscribing with the same three but a
// different interval or topic must NOT be silently treated as unchanged.
func TestChangeHashDetectsEveryField(t *testing.T) {
	base := sampleSubscription()
	baseHash, err := ChangeHash(base)
	if err != nil {
		t.Fatalf("ChangeHash(base): %v", err)
	}

	mutations := map[string]func(*Subscription){
		"url":       func(s *Subscription) { s.URL = s.URL + "&extra=1" },
		"headers":   func(s *Subscription) { s.Headers["New-Header"] = "x" },
		"transform": func(s *Subscription) { s.Transform = s.Transform + " " },
		"interval":  func(s *Subscription) { s.IntervalSeconds = s.IntervalSeconds + 60 },
		"topic":     func(s *Subscription) { s.Topic = s.Topic + "/v2" },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated := sampleSubscription()
			mutate(&mutated)
			got, err := ChangeHash(mutated)
			if err != nil {
				t.Fatalf("ChangeHash(mutated %s): %v", name, err)
			}
			if got == baseHash {
				t.Fatalf("mutating %s did not change the hash", name)
			}
		})
	}
}

// TestFetchKeyExcludesTransform is the other half of "two hashes, and do not
// conflate them": FetchKey must be identical for two subscriptions that
// differ only in their transform (and interval, and topic) — so devices
// sharing a URL with different reductions share one upstream fetch — but
// must differ when the URL or headers differ.
func TestFetchKeyExcludesTransform(t *testing.T) {
	a := sampleSubscription()
	b := sampleSubscription()
	b.DeviceID = "heater-1"
	b.Name = "forecast-full"
	b.Transform = "function(body){return {all: body};}"
	b.IntervalSeconds = 900
	b.Topic = "myhome/fetch/heater-1/forecast-full"

	keyA, err := FetchKey(a.URL, a.Headers)
	if err != nil {
		t.Fatalf("FetchKey(a): %v", err)
	}
	keyB, err := FetchKey(b.URL, b.Headers)
	if err != nil {
		t.Fatalf("FetchKey(b): %v", err)
	}
	if keyA != keyB {
		t.Fatalf("FetchKey differs for subscriptions sharing URL+headers: %q vs %q", keyA, keyB)
	}

	// And the two subscriptions' ChangeHash-es must differ, precisely
	// because ChangeHash does look at the transform/interval/topic.
	changeA, err := ChangeHash(a)
	if err != nil {
		t.Fatalf("ChangeHash(a): %v", err)
	}
	changeB, err := ChangeHash(b)
	if err != nil {
		t.Fatalf("ChangeHash(b): %v", err)
	}
	if changeA == changeB {
		t.Fatalf("ChangeHash conflates two subscriptions that differ in transform/interval/topic")
	}
}

// TestFetchKeyDiffersOnURLOrHeaders is the negative case: a different URL or
// a different header set must not dedupe together.
func TestFetchKeyDiffersOnURLOrHeaders(t *testing.T) {
	base := sampleSubscription()
	baseKey, err := FetchKey(base.URL, base.Headers)
	if err != nil {
		t.Fatalf("FetchKey(base): %v", err)
	}

	differentURL, err := FetchKey(base.URL+"&other=1", base.Headers)
	if err != nil {
		t.Fatalf("FetchKey(differentURL): %v", err)
	}
	if differentURL == baseKey {
		t.Fatalf("FetchKey did not change with a different URL")
	}

	differentHeaders := map[string]string{"Accept": "text/plain"}
	withDifferentHeaders, err := FetchKey(base.URL, differentHeaders)
	if err != nil {
		t.Fatalf("FetchKey(differentHeaders): %v", err)
	}
	if withDifferentHeaders == baseKey {
		t.Fatalf("FetchKey did not change with different headers")
	}
}
