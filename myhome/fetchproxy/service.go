package fetchproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
	"golang.org/x/sync/singleflight"
)

const (
	// SubscribeTopic is the single, shared MQTT topic devices publish a
	// subscription request to. Modelled on "myhome/rpc" (internal/myhome):
	// one well-known topic, JSON payload carries the identity (device_id,
	// name) rather than the topic path. Unlike myhome/rpc this is fire-and-
	// forget — the device does not wait for a response; the first published,
	// retained result on its own Topic is the acknowledgement.
	SubscribeTopic = "myhome/fetch/subscribe"

	// DefaultPollInterval and MinPollInterval implement #465 decision 4:
	// "Refresh interval: 60 minutes by default... with a 15-minute floor".
	DefaultPollInterval = 60 * time.Minute
	MinPollInterval     = 15 * time.Minute

	// jitterFraction spreads subscriptions so they do not all fire in
	// lockstep (#465). Up to 10% of the interval, added on top of it.
	jitterFraction = 0.10

	// DefaultHTTPTimeout bounds a single upstream fetch. The daemon must
	// remain internet-optional (CLAUDE.md): a hung upstream must not block
	// anything else, so every fetch runs under this timeout regardless of
	// what the caller's context allows.
	DefaultHTTPTimeout = 10 * time.Second
)

// httpDoer is the minimal surface Service needs from an HTTP client,
// satisfied by *http.Client and easily faked in tests.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// fetchState is the in-memory-only diagnostic state for one subscription.
// None of this is persisted — see the package comment in storage.go on why
// last_seen must not become a column.
type fetchState struct {
	lastSeen time.Time
	lastOK   bool
}

// Service is the daemon-side fetch-and-transform proxy: it owns the
// subscription registry, the upstream fetch/dedup, the goja transform
// evaluator, and the retained, timestamped publish of results.
type Service struct {
	ctx      context.Context
	log      logr.Logger
	mc       mqtt.Client
	storage  *Storage
	limits   EvalLimits
	http     httpDoer
	fetchGrp singleflight.Group

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // subscription key -> poll-loop cancel
	states  map[string]*fetchState        // subscription key -> in-memory diagnostics
}

// NewService creates the fetch proxy, resumes polling every subscription
// already in storage (so a daemon restart does not silently stop a feed —
// see #465 "Subscription lifecycle across a daemon restart"), and subscribes
// to SubscribeTopic to receive new/updated registrations. Errors along the
// way are logged, not fatal: a broken subscribe or a single bad stored row
// must not prevent the rest of the daemon from starting.
func NewService(ctx context.Context, log logr.Logger, mc mqtt.Client, storage *Storage) *Service {
	s := &Service{
		ctx:     ctx,
		log:     log.WithName("fetchproxy.Service"),
		mc:      mc,
		storage: storage,
		limits:  DefaultLimits(),
		http:    &http.Client{Timeout: DefaultHTTPTimeout},
		cancels: make(map[string]context.CancelFunc),
		states:  make(map[string]*fetchState),
	}

	subs, err := storage.List()
	if err != nil {
		s.log.Error(err, "Failed to load persisted fetch subscriptions")
	}
	for _, sub := range subs {
		s.startPoll(sub)
	}
	s.log.Info("Fetch proxy resumed subscriptions", "count", len(subs))

	if err := mc.SubscribeWithHandler(ctx, SubscribeTopic, 32, "fetchproxy.subscribe", s.handleIngest); err != nil {
		s.log.Error(err, "Failed to subscribe to fetch subscription topic", "topic", SubscribeTopic)
	}

	return s
}

// RegisterHandlers registers the fetch.list / fetch.delete RPC methods used
// by `myhome ctl fetch`.
func (s *Service) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.FetchList, func(ctx context.Context, params any) (any, error) {
		return s.HandleList(ctx)
	})
	myhome.RegisterMethodHandler(myhome.FetchDelete, func(ctx context.Context, params any) (any, error) {
		return s.HandleDelete(ctx, params.(*myhome.FetchDeleteParams))
	})
}

// handleIngest is the SubscribeWithHandler callback for SubscribeTopic. A
// malformed or invalid message is logged and dropped, never returned as an
// error up the MQTT dispatch loop — one bad publish from a misbehaving
// device must not take down message delivery for every other subscriber.
func (s *Service) handleIngest(_ string, payload []byte, _ string) error {
	var sub Subscription
	if err := json.Unmarshal(payload, &sub); err != nil {
		s.log.Error(err, "Failed to parse fetch subscription", "payload", string(payload))
		return nil
	}
	if err := validateSubscription(sub); err != nil {
		s.log.Error(err, "Rejecting invalid fetch subscription", "device_id", sub.DeviceID, "name", sub.Name)
		return nil
	}
	s.Register(sub)
	return nil
}

func validateSubscription(sub Subscription) error {
	if sub.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if sub.Name == "" {
		return fmt.Errorf("name is required")
	}
	if sub.URL == "" {
		return fmt.Errorf("url is required")
	}
	if sub.Transform == "" {
		return fmt.Errorf("transform is required")
	}
	if sub.Topic == "" {
		return fmt.Errorf("topic is required")
	}
	return nil
}

// Register persists sub (skipping the write if its change-hash is
// unchanged — #465 SD-card wear) and ensures a poll loop is running for it.
// Exported so both the MQTT ingest path and tests can drive it directly.
func (s *Service) Register(sub Subscription) {
	modified, err := s.storage.Upsert(sub)
	if err != nil {
		s.log.Error(err, "Failed to persist fetch subscription", "device_id", sub.DeviceID, "name", sub.Name)
		return
	}
	if modified {
		s.log.Info("Fetch subscription registered", "device_id", sub.DeviceID, "name", sub.Name, "url", sub.URL, "topic", sub.Topic)
		s.startPoll(sub)
		return
	}
	s.log.V(1).Info("Fetch subscription unchanged, not re-persisted", "device_id", sub.DeviceID, "name", sub.Name)
	s.mu.Lock()
	_, active := s.cancels[sub.key()]
	s.mu.Unlock()
	if !active {
		// Boot-time resume already starts every stored subscription, so
		// this only matters for a row that exists in storage but somehow
		// has no active loop (e.g. a previous startPoll failed to run).
		s.startPoll(sub)
	}
}

// Stop cancels the poll loop for (deviceID, name) and discards its in-memory
// diagnostics. It does not touch storage — HandleDelete calls both.
func (s *Service) Stop(deviceID, name string) {
	key := deviceID + "/" + name
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.cancels[key]; ok {
		cancel()
		delete(s.cancels, key)
	}
	delete(s.states, key)
}

// startPoll (re)starts the poll loop for sub, cancelling any previous loop
// for the same key first so an updated subscription takes effect
// immediately rather than waiting out the old interval.
func (s *Service) startPoll(sub Subscription) {
	key := sub.key()
	s.mu.Lock()
	if cancel, ok := s.cancels[key]; ok {
		cancel()
	}
	pctx, cancel := context.WithCancel(s.ctx)
	s.cancels[key] = cancel
	s.mu.Unlock()

	go s.pollLoop(pctx, sub)
}

// pollLoop fetches and publishes sub immediately, then repeats at its
// effective interval plus jitter until pctx is cancelled (subscription
// deleted, updated, or daemon shutting down).
func (s *Service) pollLoop(pctx context.Context, sub Subscription) {
	s.runOnce(pctx, sub)
	interval := EffectiveInterval(sub.IntervalSeconds)
	for {
		wait := addJitter(interval)
		select {
		case <-pctx.Done():
			return
		case <-time.After(wait):
			s.runOnce(pctx, sub)
		}
	}
}

// EffectiveInterval applies the default and the floor from #465 decision 4.
func EffectiveInterval(requestedSeconds int) time.Duration {
	if requestedSeconds <= 0 {
		return DefaultPollInterval
	}
	d := time.Duration(requestedSeconds) * time.Second
	if d < MinPollInterval {
		return MinPollInterval
	}
	return d
}

func addJitter(d time.Duration) time.Duration {
	jitterMax := int64(float64(d) * jitterFraction)
	if jitterMax <= 0 {
		return d
	}
	return d + time.Duration(rand.Int63n(jitterMax+1))
}

// runOnce performs one fetch-transform-publish cycle for sub. Any failure —
// fetch error, transform error, publish error — is logged and leaves the
// previously published retained value untouched: this function only ever
// calls publish() on the success path.
func (s *Service) runOnce(ctx context.Context, sub Subscription) {
	fetchKey, err := FetchKey(sub.URL, sub.Headers)
	if err != nil {
		s.recordFailure(sub, fmt.Errorf("compute fetch key: %w", err))
		return
	}

	// Deduplicate upstream fetches by URL+headers (#465 decision 4): a
	// singleflight.Group collapses concurrent runOnce calls across
	// subscriptions that share a fetchKey into one HTTP request.
	bodyAny, err, _ := s.fetchGrp.Do(fetchKey, func() (interface{}, error) {
		return s.fetch(ctx, sub.URL, sub.Headers)
	})
	if err != nil {
		s.recordFailure(sub, fmt.Errorf("fetch failed: %w", err))
		return
	}
	body := bodyAny.([]byte)

	out, err := Evaluate(sub.Transform, body, s.limits)
	if err != nil {
		s.recordFailure(sub, fmt.Errorf("transform failed: %w", err))
		return
	}

	s.publish(ctx, sub, out)
}

// fetch performs the actual upstream HTTP GET, bounded by DefaultHTTPTimeout
// regardless of ctx's own deadline, and by limits.MaxInputBytes.
func (s *Service) fetch(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	fctx, cancel := context.WithTimeout(ctx, DefaultHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %s", resp.Status)
	}

	maxBytes := s.limits.MaxInputBytes
	limited := io.LimitReader(resp.Body, int64(maxBytes)+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(b) > maxBytes {
		return nil, fmt.Errorf("response body exceeds input cap %d bytes", maxBytes)
	}
	return b, nil
}

// publish adds a "ts" (unix seconds) field to the transform's output and
// publishes it retained to sub.Topic. The daemon owns serialisation end to
// end, so a transform can only ever hand back the shape it was given: an
// object (#465 decision 6).
func (s *Service) publish(ctx context.Context, sub Subscription, transformed json.RawMessage) {
	var obj map[string]interface{}
	if err := json.Unmarshal(transformed, &obj); err != nil {
		s.recordFailure(sub, fmt.Errorf("re-decode transform output: %w", err))
		return
	}
	obj["ts"] = time.Now().Unix()

	payload, err := json.Marshal(obj)
	if err != nil {
		s.recordFailure(sub, fmt.Errorf("marshal published payload: %w", err))
		return
	}

	if err := s.mc.Publish(ctx, sub.Topic, payload, mqtt.AtLeastOnce, true /* retained */, "fetchproxy"); err != nil {
		s.recordFailure(sub, fmt.Errorf("publish failed: %w", err))
		return
	}
	s.recordSuccess(sub)
}

func (s *Service) recordSuccess(sub Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[sub.key()] = &fetchState{lastSeen: time.Now(), lastOK: true}
}

func (s *Service) recordFailure(sub Subscription, err error) {
	s.log.Error(err, "Fetch-and-transform cycle failed; retained value left untouched", "device_id", sub.DeviceID, "name", sub.Name)
	s.mu.Lock()
	defer s.mu.Unlock()
	prevSeen := time.Time{}
	if st, ok := s.states[sub.key()]; ok {
		prevSeen = st.lastSeen
	}
	s.states[sub.key()] = &fetchState{lastSeen: prevSeen, lastOK: false}
}

// HandleList handles fetch.list: every persisted subscription plus its
// in-memory (this-process-lifetime-only) diagnostics.
func (s *Service) HandleList(ctx context.Context) (*myhome.FetchListResult, error) {
	subs, err := s.storage.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list fetch subscriptions: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	views := make([]myhome.FetchSubscriptionView, 0, len(subs))
	for _, sub := range subs {
		view := myhome.FetchSubscriptionView{
			DeviceID:        sub.DeviceID,
			Name:            sub.Name,
			URL:             sub.URL,
			Topic:           sub.Topic,
			IntervalSeconds: int(EffectiveInterval(sub.IntervalSeconds).Seconds()),
		}
		if st, ok := s.states[sub.key()]; ok {
			view.LastFetchOK = st.lastOK
			if !st.lastSeen.IsZero() {
				ts := st.lastSeen.UTC().Format(time.RFC3339)
				view.LastSeen = &ts
			}
		}
		views = append(views, view)
	}
	return &myhome.FetchListResult{Subscriptions: views}, nil
}

// HandleDelete handles fetch.delete: the explicit, no-TTL cleanup path
// (#465 "Cleanup is explicit").
func (s *Service) HandleDelete(ctx context.Context, params *myhome.FetchDeleteParams) (*myhome.FetchDeleteResult, error) {
	if params.DeviceID == "" || params.Name == "" {
		return nil, fmt.Errorf("device_id and name are required")
	}

	found, err := s.storage.Delete(params.DeviceID, params.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to delete fetch subscription: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("fetch subscription not found: %s/%s", params.DeviceID, params.Name)
	}

	s.Stop(params.DeviceID, params.Name)
	s.log.Info("Fetch subscription deleted", "device_id", params.DeviceID, "name", params.Name)

	return &myhome.FetchDeleteResult{DeviceID: params.DeviceID, Name: params.Name}, nil
}
