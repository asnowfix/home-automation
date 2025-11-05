package mqtt

import (
	"context"
	"fmt"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/go-logr/logr"
)

// CachedMessage represents a cached MQTT message with metadata
type CachedMessage struct {
	Topic     string
	Payload   []byte
	Timestamp time.Time
}

// Cache manages MQTT message caching with LRU eviction
type Cache struct {
	cache  *ristretto.Cache
	log    logr.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// CacheConfig holds configuration for the MQTT cache
type CacheConfig struct {
	// MaxCost is the maximum cost of cache (in bytes)
	MaxCost int64
	// NumCounters is the number of keys to track frequency (10x MaxCost recommended)
	NumCounters int64
	// BufferItems is the number of keys per Get buffer
	BufferItems int64
}

// DefaultCacheConfig returns sensible defaults for the cache
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxCost:     10 << 20, // 10 MB
		NumCounters: 100000,   // 10x expected items
		BufferItems: 64,
	}
}

// NewCache creates a new MQTT message cache
func NewCache(ctx context.Context, log logr.Logger, config CacheConfig) (*Cache, error) {
	log = log.WithName("mqtt.Cache")

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: config.NumCounters,
		MaxCost:     config.MaxCost,
		BufferItems: config.BufferItems,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ristretto cache: %w", err)
	}

	cacheCtx, cancel := context.WithCancel(ctx)

	c := &Cache{
		cache:  cache,
		log:    log,
		ctx:    cacheCtx,
		cancel: cancel,
	}

	log.Info("MQTT cache initialized", "max_cost_mb", config.MaxCost/(1<<20))
	return c, nil
}

// StartCaching starts a background goroutine that caches all messages from a subscription
// The topic parameter can include wildcards (e.g., "#" for all topics)
func (c *Cache) StartCaching(client *Client, topic string) error {
	c.log.Info("Starting MQTT message caching", "topic", topic)

	// Subscribe to all topics (or specified topic pattern)
	msgChan, err := client.SubscriberWithTopic(c.ctx, topic, 100)
	if err != nil {
		return fmt.Errorf("failed to subscribe for caching: %w", err)
	}

	// Start background goroutine to cache messages
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				c.log.Info("Stopping MQTT message caching")
				return
			case msg, ok := <-msgChan:
				if !ok {
					c.log.Info("Message channel closed, stopping caching")
					return
				}
				c.cacheMessage(msg.Topic(), msg.Payload())
			}
		}
	}()

	c.log.Info("MQTT message caching started", "topic", topic)
	return nil
}

// cacheMessage stores a message in the cache
func (c *Cache) cacheMessage(topic string, payload []byte) {
	cachedMsg := &CachedMessage{
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	// Cost is the size of the payload plus overhead
	cost := int64(len(payload) + len(topic) + 100) // +100 for struct overhead

	// Store in cache (topic is the key)
	if !c.cache.Set(topic, cachedMsg, cost) {
		c.log.V(1).Info("Failed to cache message (buffer full, will retry)", "topic", topic)
	}
}

// Get retrieves the last cached message for a given topic
func (c *Cache) Get(topic string) (*CachedMessage, bool) {
	value, found := c.cache.Get(topic)
	if !found {
		return nil, false
	}

	cachedMsg, ok := value.(*CachedMessage)
	if !ok {
		c.log.Error(nil, "Invalid cached message type", "topic", topic)
		return nil, false
	}

	return cachedMsg, true
}

// Replay republishes the last cached message for a given topic
func (c *Cache) Replay(ctx context.Context, client *Client, topic string) error {
	cachedMsg, found := c.Get(topic)
	if !found {
		return fmt.Errorf("no cached message found for topic: %s", topic)
	}

	c.log.Info("Replaying cached message", "topic", topic, "age", time.Since(cachedMsg.Timestamp))

	err := client.Publish(ctx, topic, cachedMsg.Payload)
	if err != nil {
		return fmt.Errorf("failed to replay message: %w", err)
	}

	return nil
}

// Clear removes all cached messages
func (c *Cache) Clear() {
	c.log.Info("Clearing MQTT cache")
	c.cache.Clear()
}

// Close stops the cache and releases resources
func (c *Cache) Close() {
	c.log.Info("Closing MQTT cache")
	c.cancel()
	c.cache.Close()
}

// Stats returns cache statistics
func (c *Cache) Stats() map[string]interface{} {
	metrics := c.cache.Metrics
	return map[string]interface{}{
		"hits":             metrics.Hits(),
		"misses":           metrics.Misses(),
		"keys_added":       metrics.KeysAdded(),
		"keys_updated":     metrics.KeysUpdated(),
		"keys_evicted":     metrics.KeysEvicted(),
		"cost_added":       metrics.CostAdded(),
		"cost_evicted":     metrics.CostEvicted(),
		"sets_dropped":     metrics.SetsDropped(),
		"sets_rejected":    metrics.SetsRejected(),
		"gets_kept":        metrics.GetsKept(),
		"gets_dropped":     metrics.GetsDropped(),
		"hit_ratio":        metrics.Ratio(),
	}
}
