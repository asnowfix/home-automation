package vacuum

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type recordedEvent struct{ device, component, event, severity, data string }

type fakeRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
	err    error
}

func (f *fakeRecorder) RecordEvent(_ context.Context, device, component, event, severity, data string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, recordedEvent{device, component, event, severity, data})
	return f.err
}

func TestCleanRecordsRequestEvent(t *testing.T) {
	h, _ := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		if v == cloudVer {
			c.onChange(aborted(docked(robotVer), 224))
		} else {
			c.onChange(running(aborted(docked(robotVer), 224)))
		}
	})
	rec := &fakeRecorder{}
	h.svc.SetEventRecorder(rec)
	if _, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 2 {
		t.Fatalf("events = %+v, want one per attempt", rec.events)
	}
	e := rec.events[0]
	if e.event != CleanRequestEvent || e.severity != "info" || e.component != "vacuum" || e.device != "Edmond" {
		t.Fatalf("event = %+v", e)
	}
	want := `{"attempt":1,"map_id":"m1","map_version":"` + cloudVer + `","regions":[{"id":"21","type":"rid"}],"rooms":["Cuisine"]}`
	if e.data != want {
		t.Fatalf("data\n got  %s\n want %s", e.data, want)
	}
	if !strings.Contains(rec.events[1].data, `"attempt":2`) || !strings.Contains(rec.events[1].data, robotVer) {
		t.Fatalf("retry event = %s", rec.events[1].data)
	}
	for _, ev := range rec.events {
		if strings.Contains(ev.data+ev.device, blid) {
			t.Fatal("event leaks the BLID")
		}
	}
}

func TestCleanRecorderErrorDoesNotBlockClean(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		c.onChange(running(docked(robotVer)))
	})
	h.svc.SetEventRecorder(&fakeRecorder{err: errors.New("disk full")})
	res, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err != nil || !res.OK || len(versions(c)) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
