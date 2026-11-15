package homedrive

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

const (
	// MaxInstances bounds the cache: a ninth new host/user is dropped.
	MaxInstances = 8

	// StaleFactor: an instance is stale when its document is older than
	// StaleFactor x publish_interval_s (contract section 7.1).
	StaleFactor = 2

	// OfflineAfter: no document younger than this for this long means offline
	// (contract section 7.1), counted from process start when nothing arrived.
	OfflineAfter = 15 * time.Minute

	// SkewTolerance: a ts further in the future than this is clock skew
	// (contract section 7.2).
	SkewTolerance = 120 * time.Second

	// PersistEvery throttles the myhome.db writes per instance.
	PersistEvery = 5 * time.Minute

	// ForgetAfter drops an instance unseen for this long (design open
	// question 2, default).
	ForgetAfter = 30 * 24 * time.Hour

	// logEvery throttles the "invalid payload" log line per instance.
	logEvery = time.Hour

	maxNameLen    = 128
	maxLogTracked = 32
)

// TopicFilter is the one subscription: homedrive/<host>/<user>/status.
const TopicFilter = "homedrive/+/+/status"

// Key identifies one homedrive instance.
type Key struct{ Host, User string }

// Liveness is the age-based part of an instance's state.
type Liveness string

const (
	Online  Liveness = "online"
	Stale   Liveness = "stale"
	Offline Liveness = "offline"
)

// Problems recorded when the last payload of a known instance was rejected.
const (
	ProblemInvalid  = "last status payload was invalid"
	ProblemTooLarge = "last status payload was too large"
)

// View is one instance as the UI sees it, already classified. Status is shared
// with the cache and must be treated as read-only.
type View struct {
	Host, User string
	Liveness   Liveness
	LastTs     time.Time     // payload ts, or receipt time under clock skew; zero if unknown
	Age        time.Duration // now - LastTs
	Status     *Status       // nil when a persisted row no longer parses

	Unsupported bool   // schema major not understood: liveness only
	ClockSkew   bool   // ts was in the future; receipt time used for liveness
	Degraded    bool   // state is not one of the known values
	StateError  bool   // state == "error"
	AuthProblem bool   // auth.state not in {ok, unknown}
	Problem     string // the last payload was rejected (see Problem*)
}

type instance struct {
	key       Key
	lastTs    time.Time
	interval  time.Duration
	status    *Status
	raw       []byte
	clockSkew bool
	problem   string
	lastWrite time.Time // zero: not written since this process started
}

// Config configures a Cache. Now defaults to time.Now. Store may be nil
// (memory only).
type Config struct {
	Log   logr.Logger
	Now   func() time.Time
	Store *Store
}

// Cache holds the last status of every homedrive instance.
type Cache struct {
	log   logr.Logger
	now   func() time.Time
	store *Store
	start time.Time // process start, for the "offline counted from start" rule

	mu        sync.Mutex
	instances map[Key]*instance
	invalid   int64 // rejected payloads, all instances
	logged    map[Key]time.Time
	capLogged bool
}

// NewCache builds a Cache and loads the persisted instances, so a dead agent is
// still known (and shown offline) after a daemon restart.
func NewCache(cfg Config) *Cache {
	c := &Cache{
		log:       cfg.Log.WithName("homedrive"),
		now:       cfg.Now,
		store:     cfg.Store,
		instances: map[Key]*instance{},
		logged:    map[Key]time.Time{},
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.start = c.now()
	c.load()
	return c
}

func (c *Cache) load() {
	if c.store == nil {
		return
	}
	rows, err := c.store.Load()
	if err != nil {
		c.log.Error(err, "cannot load persisted homedrive instances")
		return
	}
	for _, r := range rows { // newest first
		if len(c.instances) >= MaxInstances {
			break
		}
		in := &instance{key: Key{r.Host, r.User}, lastTs: time.Unix(r.LastTs, 0).UTC(), raw: []byte(r.LastStatus)}
		if st, err := Parse(in.raw); err == nil {
			in.status, in.interval = st, st.IntervalOrDefault()
		} else {
			in.interval = DefaultPublishInterval
		}
		c.instances[in.key] = in
	}
}

// HandleMessage is the MQTT handler for TopicFilter; its signature matches
// mqtt.Client.SubscribeWithHandler. It never returns an error for a bad
// payload (the client would only log it): bad payloads are counted.
func (c *Cache) HandleMessage(topic string, payload []byte, _ string) error {
	key, ok := ParseTopic(topic)
	if !ok {
		return nil
	}
	now := c.now()
	st, err := Parse(payload)

	c.mu.Lock()
	in := c.instances[key]
	if err != nil {
		c.reject(key, in, err, now)
		c.mu.Unlock()
		return nil
	}
	if in == nil {
		if len(c.instances) >= MaxInstances {
			if !c.capLogged {
				c.capLogged = true
				c.log.Info("homedrive instance limit reached, ignoring new instance", "limit", MaxInstances, "host", key.Host, "user", key.User)
			}
			c.mu.Unlock()
			return nil
		}
		in = &instance{key: key}
		c.instances[key] = in
	}
	in.status, in.interval, in.problem = st, st.IntervalOrDefault(), ""
	in.raw = append(in.raw[:0], payload...)
	switch {
	case st.Ts.IsZero(): // unsupported schema without a ts: liveness unchanged
	case st.Ts.After(now.Add(SkewTolerance)):
		// Clock skew (contract 7.2): judge liveness by receipt time until it clears.
		in.lastTs, in.clockSkew = now, true
	default:
		in.lastTs, in.clockSkew = st.Ts.Time, false
	}
	persist := c.dueLocked(in, now)
	var row Row
	if persist {
		row = in.row()
	}
	c.mu.Unlock()

	if persist {
		c.save(row)
	}
	return nil
}

// reject records a bad payload. Caller holds c.mu.
func (c *Cache) reject(key Key, in *instance, err error, now time.Time) {
	c.invalid++
	if in != nil {
		in.problem = ProblemInvalid
		if errors.Is(err, ErrTooLarge) {
			in.problem = ProblemTooLarge
		}
	}
	last, seen := c.logged[key]
	if seen && now.Sub(last) < logEvery {
		return
	}
	if !seen && len(c.logged) >= maxLogTracked {
		return // a spoofer must not grow this map
	}
	c.logged[key] = now
	c.log.Info("ignoring invalid homedrive status", "host", key.Host, "user", key.User, "err", err.Error())
}

// dueLocked reports whether in must be written now and stamps the write.
func (c *Cache) dueLocked(in *instance, now time.Time) bool {
	if c.store == nil || in.lastTs.IsZero() {
		return false
	}
	if !in.lastWrite.IsZero() && now.Sub(in.lastWrite) < PersistEvery {
		return false
	}
	in.lastWrite = now
	return true
}

func (c *Cache) save(r Row) {
	if err := c.store.Save(r); err != nil {
		// In-memory operation continues; the worst case is a stale row after a restart.
		c.log.Error(err, "cannot persist homedrive instance", "host", r.Host, "user", r.User)
	}
}

func (in *instance) row() Row {
	return Row{Host: in.key.Host, User: in.key.User, LastTs: in.lastTs.Unix(), LastStatus: string(in.raw)}
}

// Snapshot returns every known instance classified at now, sorted by host then
// user. Empty means "never seen". It reads memory only.
func (c *Cache) Snapshot(now time.Time) []View {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]View, 0, len(c.instances))
	for _, in := range c.instances {
		out = append(out, c.view(in, now))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].User < out[j].User
	})
	return out
}

// Invalid is the number of rejected payloads since start.
func (c *Cache) Invalid() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invalid
}

func (c *Cache) view(in *instance, now time.Time) View {
	v := View{Host: in.key.Host, User: in.key.User, LastTs: in.lastTs, Status: in.status, ClockSkew: in.clockSkew, Problem: in.problem}
	v.Liveness = Classify(in.lastTs, in.interval, c.start, now)
	if !in.lastTs.IsZero() {
		v.Age = now.Sub(in.lastTs)
	}
	if st := in.status; st != nil {
		v.Unsupported = st.Unsupported
		if !st.Unsupported {
			v.StateError = st.State == StateError
			v.Degraded = !knownState(st.State)
			v.AuthProblem = st.Auth != nil && !authFine(st.Auth.State)
		}
	}
	return v
}

// Classify is the one liveness classification (contract section 7.1).
//   - offline: no document newer than OfflineAfter, counted from processStart
//     when the last document is older than the process;
//   - stale: older than StaleFactor x interval;
//   - online otherwise.
//
// lastTs is the payload ts (receipt time under clock skew), never the time a
// retained message was delivered.
func Classify(lastTs time.Time, interval time.Duration, processStart, now time.Time) Liveness {
	if interval <= 0 {
		interval = DefaultPublishInterval
	}
	ref := lastTs
	if processStart.After(ref) {
		ref = processStart
	}
	if now.Sub(ref) > OfflineAfter {
		return Offline
	}
	if lastTs.IsZero() || now.Sub(lastTs) > StaleFactor*interval {
		return Stale
	}
	return Online
}

func knownState(s string) bool {
	switch s {
	case StateStarting, StateRunning, StatePaused, StateError:
		return true
	}
	return false
}

func authFine(s string) bool { return s == AuthOK || s == AuthUnknown || s == "" }

// Prune forgets instances unseen for ForgetAfter, in memory and in the store.
func (c *Cache) Prune(now time.Time) {
	var gone []Key
	c.mu.Lock()
	for k, in := range c.instances {
		if !in.lastTs.IsZero() && now.Sub(in.lastTs) > ForgetAfter {
			delete(c.instances, k)
			gone = append(gone, k)
		}
	}
	c.mu.Unlock()
	for _, k := range gone {
		c.log.Info("homedrive instance no longer tracked", "host", k.Host, "user", k.User)
		if c.store != nil {
			if err := c.store.Delete(k.Host, k.User); err != nil {
				c.log.Error(err, "cannot delete homedrive instance", "host", k.Host, "user", k.User)
			}
		}
	}
}

// Flush writes every instance now, best effort (shutdown).
func (c *Cache) Flush() {
	if c.store == nil {
		return
	}
	c.mu.Lock()
	rows := make([]Row, 0, len(c.instances))
	for _, in := range c.instances {
		if !in.lastTs.IsZero() {
			rows = append(rows, in.row())
		}
	}
	c.mu.Unlock()
	for _, r := range rows {
		c.save(r)
	}
}

// Run prunes at start and hourly until ctx ends, then flushes.
func (c *Cache) Run(ctx context.Context) {
	c.Prune(c.now())
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Flush()
			return
		case <-t.C:
			c.Prune(c.now())
		}
	}
}

// ParseTopic extracts host and user from homedrive/<host>/<user>/status.
func ParseTopic(topic string) (Key, bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[0] != "homedrive" || parts[3] != "status" {
		return Key{}, false
	}
	h, u := parts[1], parts[2]
	if h == "" || u == "" || len(h) > maxNameLen || len(u) > maxNameLen {
		return Key{}, false
	}
	return Key{h, u}, true
}
