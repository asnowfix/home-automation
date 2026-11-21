package mqtt

import (
	"sync"
	"time"

	"github.com/go-logr/logr"
	mochiServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

// dropLogInterval is the minimum delay between two Warn lines for the same client.
const dropLogInterval = time.Minute

// dropLogger is a Mochi hook that turns the broker's silent "client outbound queue full" drops
// into a Warn log line. To keep a stuck client from flooding the log, it logs at most once per
// client per interval, with the number of messages dropped since the previous line.
type dropLogger struct {
	mochiServer.HookBase
	log      logr.Logger
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	clients map[string]*dropState
}

type dropState struct {
	last    time.Time // last time a line was logged
	dropped int       // drops since then (including the one about to be logged)
}

func newDropLogger(log logr.Logger, interval time.Duration, now func() time.Time) *dropLogger {
	return &dropLogger{log: log, interval: interval, now: now, clients: map[string]*dropState{}}
}

func (h *dropLogger) ID() string { return "myhome-drop-logger" }

func (h *dropLogger) Provides(b byte) bool {
	return b == mochiServer.OnPublishDropped || b == mochiServer.OnDisconnect
}

// OnPublishDropped is called with the slow *recipient* client and the packet that did not fit.
func (h *dropLogger) OnPublishDropped(cl *mochiServer.Client, pk packets.Packet) {
	id := cl.ID
	now := h.now()

	h.mu.Lock()
	st, ok := h.clients[id]
	if !ok {
		st = &dropState{}
		h.clients[id] = st
	}
	st.dropped++
	if ok && now.Sub(st.last) < h.interval {
		h.mu.Unlock()
		return
	}
	n := st.dropped
	st.last, st.dropped = now, 0
	h.mu.Unlock()

	// logr has no Warn level and hlog defaults to errors-only, so a plain Info line would be
	// invisible by default; Error(nil, ...) is the only severity that is always shown.
	h.log.Error(nil, "WARN: dropped MQTT message for slow client (outbound queue full)",
		"client_id", id, "topic", pk.TopicName, "dropped_since_last_log", n)
}

// OnDisconnect forgets the client so the map does not grow with churning client ids.
func (h *dropLogger) OnDisconnect(cl *mochiServer.Client, _ error, _ bool) {
	h.mu.Lock()
	delete(h.clients, cl.ID)
	h.mu.Unlock()
}
