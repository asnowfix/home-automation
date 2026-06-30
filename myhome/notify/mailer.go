// Package notify provides an outbound email abstraction used by the notice
// digest (see myhome/notice). It is intentionally provider-agnostic: the
// Mailer interface has a single implementation today (Gmail via an App
// Password, myhome/notify/gmail.go) but nothing in callers depends on Gmail
// specifically, so a future SFR or transactional-provider implementation can
// be added without touching call sites.
package notify

import (
	"context"
	"time"

	"github.com/go-logr/logr"
)

// Mailer sends a single notification email. Implementations must respect
// ctx's deadline/cancellation and must never block the daemon indefinitely —
// per this project's internet-optional resilience rule, a Mailer is always
// allowed to fail (timeout, DNS, auth) without taking the daemon down; only
// the digest scheduler's log line should record the failure.
type Mailer interface {
	Send(ctx context.Context, subject, body string) error
}

// Config holds outbound SMTP configuration. From is the on/off switch: when
// From is empty, New returns a no-op Mailer regardless of every other field.
// This mirrors how myhome already gates Beem/SFR integrations on credential
// presence (see myhome/daemon/run.go), and lets callers (the notice digest
// scheduler) call Send() unconditionally without checking "is email
// configured" themselves.
type Config struct {
	Host     string // SMTP host, e.g. "smtp.gmail.com"
	Port     int    // SMTP port, e.g. 587 (STARTTLS submission)
	Username string // SMTP auth username
	Password string // SMTP auth password (for Gmail: an App Password)
	From     string // envelope/header From address; empty disables email entirely
	To       string // recipient address, or comma-separated list of addresses

	// Timeout bounds the entire dial+auth+send sequence when ctx has no
	// deadline of its own. Defaults to 10s when zero.
	Timeout time.Duration
}

// New builds the Mailer described by cfg. See Config.From for the
// enable/disable rule.
func New(log logr.Logger, cfg Config) Mailer {
	log = log.WithName("notify")
	if cfg.From == "" {
		return &noopMailer{log: log}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &gmailMailer{log: log, cfg: cfg}
}
