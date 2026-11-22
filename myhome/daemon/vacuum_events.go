package daemon

import (
	"context"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
)

// vacuumEventRecorder adapts the events service to vacuum.EventRecorder (#860).
type vacuumEventRecorder struct {
	events *events.Service
}

func (r vacuumEventRecorder) RecordEvent(ctx context.Context, deviceID, component, event, severity, data string) error {
	return r.events.Record(ctx, events.Event{
		Ts:        float64(time.Now().Unix()),
		DeviceID:  deviceID,
		Component: component,
		Event:     event,
		Severity:  severity,
		Data:      &data,
	})
}
