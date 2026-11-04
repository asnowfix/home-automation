package myhome

// FetchSubscriptionView is the read-only view of one persisted fetch
// subscription (see myhome/fetchproxy), returned by fetch.list. LastSeen and
// LastFetchOK reflect the current daemon process's lifetime only — they are
// deliberately not persisted (#465: persisting them would reintroduce a
// SQLite write on every re-publication and defeat the change-hash's whole
// purpose).
type FetchSubscriptionView struct {
	DeviceID        string  `json:"device_id"`
	Name            string  `json:"name"`
	URL             string  `json:"url"`
	Topic           string  `json:"topic"`
	IntervalSeconds int     `json:"interval_seconds"`
	LastSeen        *string `json:"last_seen,omitempty"` // RFC3339; nil if not seen this process lifetime
	LastFetchOK     bool    `json:"last_fetch_ok"`
}

// FetchListResult is the response to fetch.list.
type FetchListResult struct {
	Subscriptions []FetchSubscriptionView `json:"subscriptions"`
}

// FetchDeleteParams identifies the subscription to remove by fetch.delete.
type FetchDeleteParams struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// FetchDeleteResult confirms which subscription was removed.
type FetchDeleteResult struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}
