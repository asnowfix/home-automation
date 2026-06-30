// Package notice derives human-noteworthy "notice"-severity events that
// aren't already emitted at their source, and emails a daily digest of all
// notice events. Most notices (pool/garden plans, pump actions) already
// arrive as events.Event with severity "notice" via the normal MQTT
// ingestion path (see internal/myhome/shelly/gen2/listener.go severityFor);
// this package only adds the one rule that has no on-device or in-process
// origin yet — motion at night or while the home is unoccupied — plus the
// digest scheduler.
package notice

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/myhome/notify"
	"github.com/go-logr/logr"
)

// OccupancyChecker reports whether the home is currently occupied. The
// production implementation is myhome/occupancy.Service.IsOccupied; the
// interface exists so this package depends only on the one method it needs,
// keeping it testable without wiring MQTT/SFR.
type OccupancyChecker interface {
	IsOccupied(ctx context.Context) bool
}

// Service derives motion notices and sends the daily digest email.
type Service struct {
	log       logr.Logger
	events    *events.Service
	occupancy OccupancyChecker
	mailer    notify.Mailer
	cfg       Config

	// now is overridable in tests; defaults to time.Now.
	now func() time.Time
}

// NewService builds a notice Service. eventsSvc and occ must be non-nil;
// mailer may be a no-op (see notify.New) when email is disabled.
func NewService(log logr.Logger, eventsSvc *events.Service, occ OccupancyChecker, mailer notify.Mailer, cfg Config) *Service {
	return &Service{
		log:       log.WithName("notice"),
		events:    eventsSvc,
		occupancy: occ,
		mailer:    mailer,
		cfg:       cfg.withDefaults(),
		now:       time.Now,
	}
}

// Start launches the daily digest scheduler. It blocks until ctx is
// cancelled, so callers should invoke it via `go svc.Start(ctx)`.
func (s *Service) Start(ctx context.Context) {
	s.log.Info("Notice service started", "night_start", s.cfg.NightStart, "night_end", s.cfg.NightEnd, "digest_hour", s.cfg.DigestHour)
	for {
		wait := s.untilNextDigest(s.now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := s.SendDigest(ctx); err != nil {
			// Never fatal: a failed digest send (offline, bad creds, SMTP
			// outage) must not affect any other daemon function.
			s.log.Error(err, "Failed to send notice digest")
		}
	}
}

// OnEvent implements the motion-notice rule. It is wired into the events
// broadcast hook in myhome/daemon so it runs synchronously whenever any
// event is recorded; it only acts on "motion.detected" and is a no-op for
// everything else, including the derived events it itself records.
func (s *Service) OnEvent(ctx context.Context, e events.Event) {
	if e.Event != "motion.detected" {
		return
	}

	when := s.now()
	if e.Ts != 0 {
		when = time.Unix(int64(e.Ts), 0)
	}

	if !s.occupancy.IsOccupied(ctx) {
		s.recordDerived(ctx, e, "motion.absent", "home unoccupied")
	}
	if s.cfg.isNight(when) {
		s.recordDerived(ctx, e, "motion.night", "outside configured day window")
	}
}

func (s *Service) recordDerived(ctx context.Context, src events.Event, name, reason string) {
	payload, err := json.Marshal(map[string]string{
		"source_event": src.Event,
		"reason":       reason,
	})
	if err != nil {
		s.log.Error(err, "Failed to marshal derived notice data", "event", name)
		return
	}
	data := string(payload)

	derived := events.Event{
		Ts:        src.Ts,
		DeviceID:  src.DeviceID,
		Component: "motion",
		Event:     name,
		Severity:  "notice",
		Data:      &data,
	}
	if err := s.events.Record(ctx, derived); err != nil {
		s.log.Error(err, "Failed to record derived motion notice", "event", name, "device_id", src.DeviceID)
	}
}

// untilNextDigest returns the wait until the next configured DigestHour,
// today if it hasn't passed yet, tomorrow otherwise.
func (s *Service) untilNextDigest(now time.Time) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day(), s.cfg.DigestHour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}

// SendDigest queries the last 24h of notice-severity events and emails a
// summary. Exported so `myhome ctl notice digest --dry-run`-style tooling
// can trigger it on demand without waiting for the scheduler.
func (s *Service) SendDigest(ctx context.Context) error {
	rows, err := s.events.Store().Query(ctx, events.Query{
		Severity: "notice",
		Since:    24 * time.Hour,
		Limit:    1000,
	})
	if err != nil {
		return fmt.Errorf("query notice events: %w", err)
	}

	subject, body := formatDigest(rows, s.now())
	if err := s.mailer.Send(ctx, subject, body); err != nil {
		return fmt.Errorf("send digest email: %w", err)
	}
	s.log.Info("Notice digest sent", "count", len(rows))
	return nil
}
