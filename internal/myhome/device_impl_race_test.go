package myhome

import (
	"sync"
	"testing"

	"github.com/go-logr/logr"
)

// TestDeviceImplConcurrent reproduces the cached-device shape of #762: two
// writers (the MQTT watcher and the startup polarity pass) publish a handle on
// the same shared *Device while a render goroutine reads it. Run with -race.
func TestDeviceImplConcurrent(t *testing.T) {
	d := NewDevice(logr.Discard(), SHELLY, "shellyplus1-aabbcc")
	const n = 2000
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				d.WithImpl(i)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = d.Impl()
		}
	}()
	wg.Wait()
	if d.Impl() == nil {
		t.Fatal("Impl() is nil after writes")
	}
}
