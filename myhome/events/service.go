package events

import (
	"context"
	"time"

	"github.com/go-logr/logr"
)

type Service struct {
	log       logr.Logger
	store     *Storage
	tracker   *SensorDailyTracker
	broadcast func(Event)
	retention time.Duration
}

func NewService(log logr.Logger, store *Storage, tracker *SensorDailyTracker, broadcast func(Event), retention time.Duration) *Service {
	return &Service{
		log:       log.WithName("EventService"),
		store:     store,
		tracker:   tracker,
		broadcast: broadcast,
		retention: retention,
	}
}

func (s *Service) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cutoff := time.Now().Add(-s.retention)
			n, err := s.store.Purge(ctx, cutoff)
			if err != nil {
				s.log.Error(err, "Failed to purge old events")
			} else if n > 0 {
				s.log.Info("Purged old events", "count", n, "cutoff", cutoff)
			}
		}
	}
}

// Store returns the underlying storage for direct queries.
func (s *Service) Store() *Storage {
	return s.store
}

func (s *Service) Record(ctx context.Context, e Event) error {
	_, err := s.RecordID(ctx, e)
	return err
}

// RecordID is Record that also returns the new row's id (0 if the insert was
// ignored as a duplicate), so the caller can later amend it with UpdateData.
// Only the initial insert is broadcast; later amendments are not.
func (s *Service) RecordID(ctx context.Context, e Event) (int64, error) {
	if e.ReceivedAt == 0 {
		e.ReceivedAt = float64(time.Now().Unix())
	}
	if e.Ts == 0 {
		e.Ts = e.ReceivedAt
	}
	id, err := s.store.RecordID(ctx, e)
	if err != nil {
		return 0, err
	}
	if s.broadcast != nil {
		s.broadcast(e)
	}
	return id, nil
}

// UpdateData replaces the data of a previously recorded event.
func (s *Service) UpdateData(ctx context.Context, id int64, data string) error {
	return s.store.UpdateData(ctx, id, data)
}
