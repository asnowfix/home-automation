package fetchproxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Two hashes, for two different purposes (#465 "Two hashes, and do not
// conflate them"):
//
//  1. changeHash covers URL + headers + transform + interval + topic — every
//     field that defines the subscription. Storage skips the write when this
//     hash is unchanged, to spare the Raspberry Pi's SD card.
//  2. fetchKey covers URL + headers only. The transform must NOT appear here:
//     two devices fetching the same URL with different reductions must share
//     one upstream fetch.
//
// Both canonicalise via encoding/json, whose Marshal sorts map[string]string
// keys before emitting them (documented behaviour of encoding/json, not an
// incidental one) and never inserts whitespace. That makes the JSON stable
// across Go's deliberately randomised map iteration and across daemon
// restarts — hashing a map[string]string directly, e.g. with fmt.Sprintf or a
// hand-rolled loop over range, would not have that guarantee.

// normalizedHeaders returns headers, or an empty (non-nil) map if headers is
// nil, so that "no headers" hashes identically regardless of whether the
// caller passed nil or an empty map.
func normalizedHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return map[string]string{}
	}
	return headers
}

// canonicalChangeSubject is the exact set of fields that define a
// subscription for change-detection purposes. Field order here is
// documentation only — json.Marshal of a struct follows struct field order
// for the fields themselves, and sorts keys within the nested Headers map.
type canonicalChangeSubject struct {
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"`
	Transform       string            `json:"transform"`
	IntervalSeconds int               `json:"interval_seconds"`
	Topic           string            `json:"topic"`
}

// canonicalFetchSubject is the exact set of fields that define a fetch-dedup
// key. Deliberately excludes Transform, IntervalSeconds and Topic — see the
// package-level comment above.
type canonicalFetchSubject struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func hashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("canonicalise for hashing: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ChangeHash computes the change-detection hash for a subscription: it
// changes if and only if the wire behaviour of the subscription changes.
func ChangeHash(sub Subscription) (string, error) {
	return hashJSON(canonicalChangeSubject{
		URL:             sub.URL,
		Headers:         normalizedHeaders(sub.Headers),
		Transform:       sub.Transform,
		IntervalSeconds: sub.IntervalSeconds,
		Topic:           sub.Topic,
	})
}

// FetchKey computes the fetch-dedup key: two subscriptions with the same URL
// and headers share one upstream fetch, regardless of how they each reduce
// the result.
func FetchKey(url string, headers map[string]string) (string, error) {
	return hashJSON(canonicalFetchSubject{
		URL:     url,
		Headers: normalizedHeaders(headers),
	})
}
