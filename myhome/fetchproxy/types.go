// Package fetchproxy implements the MQTT fetch-and-transform proxy described
// in issue #465: a device publishes a subscription naming an upstream URL, an
// optional set of HTTP headers, a poll interval, a destination MQTT topic, and
// a JavaScript transform that reduces the response body to the small object
// the device actually needs. The daemon fetches, runs the transform in goja,
// and publishes only the reduced, retained result to the device's topic.
//
// Design decisions (URL+headers+transform+interval+topic hashed for change
// detection, URL+headers-only hashed for fetch dedup, SQLite persistence in
// the shared myhome.db, no re-assertion timer, in-memory-only last_seen) are
// all settled in #465 and are not re-litigated here.
package fetchproxy

// Subscription is the persisted, in-memory representation of one device's
// fetch-and-transform request. It is keyed by (DeviceID, Name).
type Subscription struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`

	// URL is the upstream endpoint to fetch (GET only; v1 has no need for
	// other verbs or a request body).
	URL string `json:"url"`

	// Headers are optional HTTP request headers sent with the fetch. Nil and
	// empty are treated identically for hashing purposes.
	Headers map[string]string `json:"headers,omitempty"`

	// Transform is JS source for a function of one argument (the response
	// body, as a string) that returns an object. It is evaluated in a bare
	// goja runtime with no host objects beyond that one argument: no
	// filesystem, no network, no require.
	Transform string `json:"transform"`

	// IntervalSeconds is the requested poll interval. Zero or negative means
	// "use the default" (DefaultPollInterval); anything below MinPollInterval
	// is raised to the floor. See EffectiveInterval.
	IntervalSeconds int `json:"interval_seconds"`

	// Topic is the MQTT topic the reduced result is published to, retained,
	// with a "ts" field the device uses to age out a stale value.
	Topic string `json:"topic"`
}

// key returns the (device_id, name) identity used throughout storage and the
// in-memory scheduler maps.
func (s Subscription) key() string {
	return s.DeviceID + "/" + s.Name
}
