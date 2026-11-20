package blu

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

// fakeClock is an injectable clock so the dedup window is tested without sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// relayPayload builds a publisher payload. relay=="" omits the key, as old
// publishers do. Synthetic address/ids/rssi only.
func relayPayload(pid int, relay string, rssi int) []byte {
	m := map[string]any{
		"address": "aa:bb:cc:00:11:22",
		"pid":     pid,
		"motion":  1,
		"rssi":    rssi,
	}
	if relay != "" {
		m["relay"] = relay
	}
	b, _ := json.Marshal(m)
	return b
}

type recordedData struct {
	RSSI   int            `json:"rssi"`
	Relays map[string]int `json:"relays"`
}

func motionRows(t *testing.T, svc *events.Service) []recordedData {
	t.Helper()
	evts, err := svc.Store().Query(context.Background(), events.Query{EventType: "motion.detected"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var out []recordedData
	for _, e := range evts {
		if e.Data == nil {
			t.Fatalf("event without data: %+v", e)
		}
		var d recordedData
		if err := json.Unmarshal([]byte(*e.Data), &d); err != nil {
			t.Fatalf("bad data %q: %v", *e.Data, err)
		}
		out = append(out, d)
	}
	return out
}

func feed(svc *events.Service, dd *relayDeduper, clk *fakeClock, payload []byte, at time.Duration) {
	clk.Advance(at)
	handleBLUEventBridge(context.Background(), logr.Discard(), payload, "", svc, nil, dd)
}

func newDedupFixture(t *testing.T) (*events.Service, *relayDeduper, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	return newTestEventsService(t), newRelayDeduper(DefaultDedupWindow, clk.Now), clk
}

func TestRelayDedup_TwoRelaysOneRow(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	feed(svc, dd, clk, relayPayload(7, "relay-a", -72), 0)
	feed(svc, dd, clk, relayPayload(7, "relay-b", -90), time.Second)

	rows := motionRows(t, svc)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if got := rows[0].Relays; len(got) != 2 || got["relay-a"] != -72 || got["relay-b"] != -90 {
		t.Errorf("relays = %v, want both relays with their rssi", got)
	}
	if rows[0].RSSI != -72 {
		t.Errorf("rssi = %d, want best (-72)", rows[0].RSSI)
	}
}

func TestRelayDedup_SingleRelay(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	feed(svc, dd, clk, relayPayload(7, "relay-a", -72), 0)

	rows := motionRows(t, svc)
	if len(rows) != 1 || len(rows[0].Relays) != 1 || rows[0].Relays["relay-a"] != -72 {
		t.Fatalf("want one row with relay-a:-72, got %+v", rows)
	}
}

func TestRelayDedup_PidWrapAroundIsDifferentEvent(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	feed(svc, dd, clk, relayPayload(255, "relay-a", -70), 0)
	feed(svc, dd, clk, relayPayload(0, "relay-a", -70), time.Second)

	if rows := motionRows(t, svc); len(rows) != 2 {
		t.Fatalf("pid 255 then 0 must be two events, got %d rows", len(rows))
	}
}

func TestRelayDedup_SamePidOutsideWindowIsNewEvent(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	feed(svc, dd, clk, relayPayload(7, "relay-a", -70), 0)
	// The counter wrapped and came back to 7 after the window closed.
	feed(svc, dd, clk, relayPayload(7, "relay-a", -70), DefaultDedupWindow+time.Second)

	if rows := motionRows(t, svc); len(rows) != 2 {
		t.Fatalf("same pid outside window must be two rows, got %d", len(rows))
	}
}

func TestRelayDedup_MissingRelayKeyStillDeduped(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	feed(svc, dd, clk, relayPayload(7, "", -80), 0)
	feed(svc, dd, clk, relayPayload(7, "", -75), time.Second)
	feed(svc, dd, clk, relayPayload(7, "relay-a", -90), time.Second)

	rows := motionRows(t, svc)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0].Relays
	if len(got) != 2 || got[UnknownRelay] != -75 || got["relay-a"] != -90 {
		t.Errorf("relays = %v, want {%s:-75, relay-a:-90}", got, UnknownRelay)
	}
}

func TestRelayDedup_NoPidIsNeverMerged(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	b, _ := json.Marshal(map[string]any{"address": "aa:bb:cc:00:11:22", "motion": 1, "rssi": -70})
	feed(svc, dd, clk, b, 0)
	feed(svc, dd, clk, b, 2*time.Second)

	if rows := motionRows(t, svc); len(rows) != 2 {
		t.Fatalf("packets without pid must not merge, got %d rows", len(rows))
	}
}

func TestRelayDedup_PendingMapIsBounded(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	for i := 0; i < 300; i++ {
		feed(svc, dd, clk, relayPayload(i%256, "relay-a", -70), 2*time.Second)
	}
	dd.mu.Lock()
	n := len(dd.pending)
	dd.mu.Unlock()
	if n > 4 {
		t.Errorf("pending map holds %d entries, want it pruned to the window (<=3)", n)
	}
}

func TestRelayDedup_Concurrent(t *testing.T) {
	svc, dd, clk := newDedupFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			handleBLUEventBridge(context.Background(), logr.Discard(), relayPayload(9, fmt.Sprintf("relay-%d", i), -60-i), "", svc, nil, dd)
		}(i)
	}
	wg.Wait()
	_ = clk
	rows := motionRows(t, svc)
	if len(rows) != 1 || len(rows[0].Relays) != 8 {
		t.Fatalf("want 1 row with 8 relays, got %+v", rows)
	}
}
