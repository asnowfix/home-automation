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
	if e.ReceivedAt == 0 {
		e.ReceivedAt = float64(time.Now().Unix())
	}
	if err := s.store.Record(ctx, e); err != nil {
		return err
	}
	if s.broadcast != nil {
		s.broadcast(e)
	}
	return nil
}
