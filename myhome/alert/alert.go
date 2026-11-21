// Package alert is the contract between alert producers and delivery (#843).
//
// Producers evaluate a condition with an Evaluator and publish Alert documents on
// <instance>/alert/<rule>/<key> (QoS 1, not retained); delivery only subscribes and calls Decode.
// The two never call each other.
//
// Wiring rule: delivery subscribes before mc.Start(), and producers start after it, so no document
// is published before the subscriber listens.
package alert

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Version is the document version carried in Alert.V.
const Version = 1

const (
	MaxSummary     = 100
	MaxDetail      = 400
	MaxLabels      = 8
	MaxLabelLength = 64
)

type Severity string

const (
	Critical Severity = "critical"
	Warning  Severity = "warning"
)

type State string

const (
	Firing   State = "firing"
	Resolved State = "resolved"
)

// ErrVersion is returned by Decode for a document with an unsupported version; callers log it once and drop.
var ErrVersion = errors.New("alert: unsupported document version")

// Alert is the document published on every transition. Summary and Detail may leave the house (email,
// WhatsApp) and are privacy-checked; Labels stay local and are never sent by a channel.
type Alert struct {
	V           int               `json:"v"`
	Fingerprint string            `json:"fingerprint"` // <rule>/<key>
	Rule        string            `json:"rule"`
	Key         string            `json:"key"`
	Severity    Severity          `json:"severity"`
	State       State             `json:"state"`
	Source      string            `json:"source"`
	Since       int64             `json:"since"` // episode start, unix seconds; (Fingerprint, Since) names the episode
	Ended       int64             `json:"ended"` // resolve time, 0 while firing
	Ts          int64             `json:"ts"`    // document time
	Summary     string            `json:"summary"`
	Detail      string            `json:"detail"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Topic is <instance>/alert/<rule>/<key>.
func Topic(instance, rule, key string) string {
	return instance + "/alert/" + rule + "/" + key
}

// TopicFilter is the subscription filter delivery uses for an instance.
func TopicFilter(instance string) string { return instance + "/alert/#" }

var (
	nameRE = regexp.MustCompile(`^[a-z0-9._-]+$`)

	// Forbidden content in Summary and Detail (text that leaves the house).
	ipv4RE   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	macRE    = regexp.MustCompile(`(?i)(?:\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b|\b[0-9a-f]{2}(?:-[0-9a-f]{2}){5}\b|\b[0-9a-f]{12}\b)`)
	shellyRE = regexp.MustCompile(`(?i)\bshelly[a-z0-9]*-[0-9a-f]{6,12}\b`)
	pathRE   = regexp.MustCompile(`(?:^|[\s"'(=,;:])/[A-Za-z0-9._~-]+|\b[A-Za-z]:[\\/]|\\\\[A-Za-z0-9._$-]+\\`)

	// Heuristic markers of raw Go/OS error text, which must never be pasted into a summary.
	rawErrorMarkers = []string{
		"error:", "panic:", "goroutine ", "dial tcp", "no such host", "connection refused",
		"i/o timeout", "context deadline exceeded", "no such file", "permission denied",
		"unexpected eof", "sql:",
	}
)

// checkText rejects the forbidden content classes in text that may leave the house.
func checkText(field, s string, max int) error {
	if utf8.RuneCountInString(s) > max {
		return fmt.Errorf("alert: %s longer than %d chars", field, max)
	}
	switch {
	case ipv4RE.MatchString(s):
		return fmt.Errorf("alert: %s contains an IP address", field)
	case shellyRE.MatchString(s):
		return fmt.Errorf("alert: %s contains a device ID", field)
	case macRE.MatchString(s):
		return fmt.Errorf("alert: %s contains a MAC address", field)
	case pathRE.MatchString(s):
		return fmt.Errorf("alert: %s contains an absolute path", field)
	}
	low := strings.ToLower(s)
	for _, m := range rawErrorMarkers {
		if strings.Contains(low, m) {
			return fmt.Errorf("alert: %s looks like raw error text", field)
		}
	}
	return nil
}

func checkLabels(labels map[string]string) error {
	if len(labels) > MaxLabels {
		return fmt.Errorf("alert: more than %d labels", MaxLabels)
	}
	for k, v := range labels {
		if utf8.RuneCountInString(k) > MaxLabelLength || utf8.RuneCountInString(v) > MaxLabelLength {
			return fmt.Errorf("alert: label longer than %d chars", MaxLabelLength)
		}
	}
	return nil
}

// Validate checks the schema and the privacy limits. Producers call it before publishing and delivery on receipt.
func (a *Alert) Validate() error {
	if a.V != Version {
		return fmt.Errorf("%w: %d", ErrVersion, a.V)
	}
	if !nameRE.MatchString(a.Rule) || !nameRE.MatchString(a.Key) {
		return fmt.Errorf("alert: rule and key must match %s", nameRE)
	}
	if a.Fingerprint != a.Rule+"/"+a.Key {
		return errors.New("alert: fingerprint must be <rule>/<key>")
	}
	if a.Severity != Critical && a.Severity != Warning {
		return fmt.Errorf("alert: bad severity %q", a.Severity)
	}
	if a.Source == "" {
		return errors.New("alert: empty source")
	}
	if a.Since <= 0 || a.Ts <= 0 {
		return errors.New("alert: since and ts must be set")
	}
	switch a.State {
	case Firing:
		if a.Ended != 0 {
			return errors.New("alert: ended must be 0 while firing")
		}
	case Resolved:
		if a.Ended < a.Since {
			return errors.New("alert: ended must not precede since")
		}
	default:
		return fmt.Errorf("alert: bad state %q", a.State)
	}
	if a.Summary == "" {
		return errors.New("alert: empty summary")
	}
	if err := checkText("summary", a.Summary, MaxSummary); err != nil {
		return err
	}
	if err := checkText("detail", a.Detail, MaxDetail); err != nil {
		return err
	}
	if err := checkLabels(a.Labels); err != nil {
		return err
	}
	return nil
}

// Encode validates and marshals the document.
func Encode(a *Alert) ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(a)
}

// Decode unmarshals and validates a received document. Unknown fields are ignored.
func Decode(payload []byte) (*Alert, error) {
	var a Alert
	if err := json.Unmarshal(payload, &a); err != nil {
		return nil, fmt.Errorf("alert: %w", err)
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return &a, nil
}
