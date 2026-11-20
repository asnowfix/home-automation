package blu

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

// DefaultDedupWindow bounds how long copies of one BLE advertisement, heard by
// several relays (blu-publisher.js instances), are merged into one events.db
// row. BTHome's pid is an 8-bit counter that wraps at 256, so the window must
// stay short: a sensor emitting >256 packets inside it could alias.
const DefaultDedupWindow = 5 * time.Second

// UnknownRelay is the key under which copies from publishers that predate the
// "relay" payload key are recorded in the "relays" map. All such copies share
// the key, so it holds the best rssi among them and counts as ONE relay.
const UnknownRelay = "unknown"

// dedupKey identifies one physical event: a packet (address, pid) and the
// event row derived from it (one packet can yield motion + battery.low).
type dedupKey struct {
	address string
	pid     int
	event   string
}

type dedupEntry struct {
	id     int64 // events.db row id; 0 if the row already existed and cannot be amended
	seen   time.Time
	fields map[string]any // data of the row; "relays" and "rssi" are kept current
	relays map[string]int
}

// relayDeduper merges copies of the same advertisement into a single events.db
// row. The first copy is inserted immediately (so the event log and its SSE
// broadcast are not delayed); later copies inside the window amend that row's
// data. It only shapes what is recorded: it never gates device behaviour.
type relayDeduper struct {
	mu      sync.Mutex
	window  time.Duration
	now     func() time.Time
	pending map[dedupKey]*dedupEntry
}

func newRelayDeduper(window time.Duration, now func() time.Time) *relayDeduper {
	if now == nil {
		now = time.Now
	}
	return &relayDeduper{window: window, now: now, pending: make(map[dedupKey]*dedupEntry)}
}

// clock returns the deduper's notion of now (time.Now when d is nil), so the
// receive-time fallback timestamp and the window share one injectable clock.
func (d *relayDeduper) clock() time.Time {
	if d == nil {
		return time.Now()
	}
	return d.now()
}

// prune drops entries older than the window. Called on every record, so the
// map holds at most the events of the last window. Caller holds d.mu.
func (d *relayDeduper) prune(now time.Time) {
	for k, e := range d.pending {
		if now.Sub(e.seen) > d.window {
			delete(d.pending, k)
		}
	}
}

// record inserts ev (or merges it into the row of an earlier copy). fields is
// the event's data without "relays". pid==nil disables merging for this packet.
// A nil d records without merging, but still writes the "relays" key.
// The lock is held across the DB write so two concurrent copies cannot both
// insert; the insert is a single local SQLite statement.
func (d *relayDeduper) record(ctx context.Context, log logr.Logger, svc *events.Service, key dedupKey, pid *int, ev events.Event, fields map[string]any, relay string, rssi int) {
	if relay == "" {
		relay = UnknownRelay
	}
	if d == nil || pid == nil {
		fields["relays"] = map[string]int{relay: rssi}
		ev.Data = bluData(fields)
		if err := svc.Record(ctx, ev); err != nil {
			log.Error(err, "Failed to record event", "device_id", ev.DeviceID, "event", ev.Event)
		}
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.prune(now)

	if e, ok := d.pending[key]; ok {
		if prev, heard := e.relays[relay]; !heard || rssi > prev {
			e.relays[relay] = rssi
		}
		best := rssi
		for _, v := range e.relays {
			if v > best {
				best = v
			}
		}
		e.fields["rssi"] = best
		e.fields["relays"] = e.relays
		if b, err := json.Marshal(e.fields); err == nil && e.id != 0 {
			if err := svc.UpdateData(ctx, e.id, string(b)); err != nil {
				log.Error(err, "Failed to merge relay copy into event", "id", e.id)
			}
		}
		return
	}

	relays := map[string]int{relay: rssi}
	fields["relays"] = relays
	ev.Data = bluData(fields)
	id, err := svc.RecordID(ctx, ev)
	if err != nil {
		log.Error(err, "Failed to record event", "device_id", ev.DeviceID, "event", ev.Event)
		return
	}
	// id==0: the row already existed (same device/component/event/ts), so there
	// is nothing to amend; still remember the key so copies are not re-inserted.
	d.pending[key] = &dedupEntry{id: id, seen: now, fields: fields, relays: relays}
}
