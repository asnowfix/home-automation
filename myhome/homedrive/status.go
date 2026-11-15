// Package homedrive subscribes to the status documents that the homedrive
// Google Drive sync agent publishes on MQTT, keeps the last known state of
// every instance (in memory and in myhome.db) and classifies it for the kiosk.
//
// The wire contract belongs to github.com/asnowfix/home-drive
// (homedrive/docs/design/status-v2.md, section 5.3 for the schema, section 7
// for consumer guidance). The structs below are a COPY of that schema, not an
// import: importing the home-drive module would pull rclone into myhome's
// dependency graph for a consumer that only decodes JSON. testdata/ pins the
// copy to the contract.
package homedrive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxPayloadBytes is the largest status document accepted. The contract
	// expects about 1.5 KB; anything bigger is not a status.
	MaxPayloadBytes = 16 << 10

	// SupportedMajor is the only schema major this package understands.
	SupportedMajor = 2

	// DefaultPublishInterval is used when a document does not carry a usable
	// publish_interval_s (contract section 5.3 says it is always there).
	DefaultPublishInterval = 30 * time.Second
)

// Errors returned by Parse. Callers use errors.Is.
var (
	ErrTooLarge = errors.New("homedrive status payload too large")
	ErrBadJSON  = errors.New("homedrive status payload is not valid")
)

// Window names of Status.Counts.
const (
	Window1h  = "1h"
	Window24h = "24h"
	Window7d  = "7d"
)

// Windows lists the count windows in display order.
var Windows = []string{Window1h, Window24h, Window7d}

// Known values of Status.State. Anything else is shown raw as degraded.
const (
	StateStarting = "starting"
	StateRunning  = "running"
	StatePaused   = "paused"
	StateError    = "error"
)

// Known values of Auth.State that are not a problem.
const (
	AuthOK      = "ok"
	AuthUnknown = "unknown"
)

// Status is homedrive's status document, schema 2.x (contract section 5.3).
// Absent values are JSON null in the contract, so they are pointers (or nil
// maps) here and render as "--". Unknown fields are ignored.
type Status struct {
	Schema          string             `json:"schema"`
	Ts              Time               `json:"ts"`
	PublishInterval int                `json:"publish_interval_s"`
	Version         string             `json:"version"`
	State           string             `json:"state"`
	StartedAt       *Time              `json:"started_at"`
	UptimeS         *int64             `json:"uptime_s"`
	DryRun          bool               `json:"dry_run"`
	PendingUp       *int64             `json:"pending_up"`
	LastPush        *Time              `json:"last_push"`
	LastPull        *Time              `json:"last_pull"`
	LastActivity    *Time              `json:"last_activity"`
	LastPoll        *Time              `json:"last_poll"`
	Counts          map[string]*Counts `json:"counts"`
	LastError       *LastError         `json:"last_error"`
	Auth            *Auth              `json:"auth"`
	Bisync          *Bisync            `json:"bisync"`
	Local           *Local             `json:"local"`
	QuotaUsedPct    *float64           `json:"quota_used_pct"`

	// Unsupported is set by Parse (not by the wire) when the schema major is
	// not SupportedMajor. Only Schema, Ts and PublishInterval are then filled.
	Unsupported bool `json:"-"`
}

// Counts is one activity window. ErrorsUp/ErrorsDown are the optional
// per-direction split the contract may gain (home-drive#141): present or
// absent, both parse.
type Counts struct {
	FilesUp    int64  `json:"files_up"`
	FilesDown  int64  `json:"files_down"`
	BytesUp    int64  `json:"bytes_up"`
	BytesDown  int64  `json:"bytes_down"`
	Errors     int64  `json:"errors"`
	ErrorsUp   *int64 `json:"errors_up"`
	ErrorsDown *int64 `json:"errors_down"`
	Conflicts  int64  `json:"conflicts"`
	Starts     int64  `json:"starts"`
}

// LastError is the last final failure. Message may contain a file path: it is
// shown on the kiosk (truncated) and must never leave the house.
type LastError struct {
	Ts      Time   `json:"ts"`
	Op      string `json:"op"`
	Message string `json:"message"`
}

// Auth is the Google authorisation state.
type Auth struct {
	State            string     `json:"state"`
	LastRefreshOK    *Time      `json:"last_refresh_ok"`
	LastRefreshError *LastError `json:"last_refresh_error"`
}

// Bisync describes the last completed bisync pass.
type Bisync struct {
	At           Time   `json:"at"`
	Result       string `json:"result"`
	DurationMs   int64  `json:"duration_ms"`
	FilesChanged int64  `json:"files_changed"`
	Error        string `json:"error"`
}

// Local describes the local tree as of the last bisync walk.
type Local struct {
	Files     int64 `json:"files"`
	Bytes     int64 `json:"bytes"`
	ScannedAt *Time `json:"scanned_at"`
}

// Time is a contract timestamp: RFC3339 UTC. It accepts any RFC3339 form
// (fractional seconds, numeric offsets) so a harmless producer change does not
// make the whole document unreadable.
type Time struct{ time.Time }

// UnmarshalJSON implements json.Unmarshaler. A JSON null leaves the zero Time.
func (t *Time) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	t.Time = v.UTC()
	return nil
}

// IntervalOrDefault is the publish interval, 30 s when the document has none.
func (s *Status) IntervalOrDefault() time.Duration {
	if s == nil || s.PublishInterval <= 0 {
		return DefaultPublishInterval
	}
	return time.Duration(s.PublishInterval) * time.Second
}

// Parse decodes a status document. It rejects payloads over MaxPayloadBytes
// (ErrTooLarge) and invalid JSON (ErrBadJSON). A document with a missing or
// foreign schema major is not an error: it is returned with Unsupported set
// and only the liveness fields filled (contract section 7.8), so the caller
// can still judge liveness from ts but never alert from the contents.
func Parse(payload []byte) (*Status, error) {
	if len(payload) > MaxPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(payload))
	}
	// First pass reads only the envelope, leniently, so that a document of a
	// future major whose other fields changed type still yields its liveness.
	var env struct {
		Schema          json.RawMessage `json:"schema"`
		Ts              json.RawMessage `json:"ts"`
		PublishInterval json.RawMessage `json:"publish_interval_s"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	var schema string
	_ = json.Unmarshal(env.Schema, &schema) // not a string: treated as missing
	if major, ok := schemaMajor(schema); !ok || major != SupportedMajor {
		st := &Status{Schema: schema, Unsupported: true}
		_ = json.Unmarshal(env.Ts, &st.Ts)
		_ = json.Unmarshal(env.PublishInterval, &st.PublishInterval)
		return st, nil
	}
	var st Status
	if err := json.Unmarshal(payload, &st); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	if st.Ts.IsZero() {
		return nil, fmt.Errorf("%w: missing ts", ErrBadJSON)
	}
	return &st, nil
}

// schemaMajor extracts N from "N.M".
func schemaMajor(schema string) (int, bool) {
	major, _, _ := strings.Cut(schema, ".")
	n, err := strconv.Atoi(major)
	return n, err == nil
}
