package ratelimit

import (
	"context"
	"sync"
	"time"
)

// RateLimiter provides per-device rate limiting with queuing.
// It ensures that commands to a specific device are spaced by at least
// the configured minimum interval.
type RateLimiter struct {
	minInterval time.Duration
	devices     sync.Map // map[string]*deviceLimiter
}

// deviceLimiter handles rate limiting for a single device
type deviceLimiter struct {
	mu       sync.Mutex
	lastCall time.Time
}

var (
	globalLimiter *RateLimiter
	once          sync.Once
)

// Init initializes the global rate limiter with the given minimum interval.
// If interval is 0, rate limiting is disabled.
func Init(interval time.Duration) {
	once.Do(func() {
		globalLimiter = &RateLimiter{
			minInterval: interval,
		}
	})
	// Allow updating the interval even after initialization
	if globalLimiter != nil {
		globalLimiter.minInterval = interval
	}
}

// GetLimiter returns the global rate limiter.
// Returns nil if not initialized.
func GetLimiter() *RateLimiter {
	return globalLimiter
}

// Wait blocks until it's safe to send a command to the specified device.
// It respects the minimum interval between commands and queues requests
// if they arrive too quickly. The interval is measured from when the previous
// command completed (via Done()) to when the next command starts.
// Returns immediately if rate limiting is disabled (interval <= 0).
func (rl *RateLimiter) Wait(ctx context.Context, deviceId string) error {
	if rl == nil || rl.minInterval <= 0 {
		return nil
	}

	// Get or create device limiter
	dl := rl.getDeviceLimiter(deviceId)

	// Lock to serialize access for this device
	dl.mu.Lock()
	defer dl.mu.Unlock()

	// Calculate how long to wait since the last command completed
	elapsed := time.Since(dl.lastCall)
	if elapsed < rl.minInterval {
		waitDuration := rl.minInterval - elapsed

		// Wait with context support
		timer := time.NewTimer(waitDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Continue
		}
	}

	return nil
}

// Done marks the completion of a command to the specified device.
// This should be called after the command response is received.
// The rate limit interval is measured from this point to the next Wait().
func (rl *RateLimiter) Done(deviceId string) {
	if rl == nil || rl.minInterval <= 0 {
		return
	}

	dl := rl.getDeviceLimiter(deviceId)
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.lastCall = time.Now()
}

// getDeviceLimiter returns the limiter for a specific device, creating one if needed
func (rl *RateLimiter) getDeviceLimiter(deviceId string) *deviceLimiter {
	if dl, ok := rl.devices.Load(deviceId); ok {
		return dl.(*deviceLimiter)
	}

	// Create new limiter for this device
	newDl := &deviceLimiter{}
	actual, _ := rl.devices.LoadOrStore(deviceId, newDl)
	return actual.(*deviceLimiter)
}

// MinInterval returns the configured minimum interval between commands
func (rl *RateLimiter) MinInterval() time.Duration {
	if rl == nil {
		return 0
	}
	return rl.minInterval
}
