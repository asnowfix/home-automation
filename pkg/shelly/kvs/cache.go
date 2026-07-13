package kvs

import (
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// ttl bounds staleness from KVS writes this cache doesn't observe directly
// (a script running on the device itself, another myhome process, the Shelly
// app). SetKeyValue/DeleteKey invalidate their key immediately, so the TTL is
// only a safety net, not the primary invalidation path.
const ttl = 5 * time.Minute

type cacheEntry struct {
	value   GetResponse
	expires time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func cacheKey(deviceId, key string) string {
	return deviceId + "\x00" + key
}

func cacheGet(deviceId, key string) (GetResponse, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := cache[cacheKey(deviceId, key)]
	if !ok || time.Now().After(e.expires) {
		return GetResponse{}, false
	}
	return e.value, true
}

func cachePut(deviceId, key string, value GetResponse) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[cacheKey(deviceId, key)] = cacheEntry{value: value, expires: time.Now().Add(ttl)}
}

func cacheInvalidate(deviceId, key string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	delete(cache, cacheKey(deviceId, key))
}

func cacheInvalidateDevice(deviceId string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	prefix := deviceId + "\x00"
	for k := range cache {
		if strings.HasPrefix(k, prefix) {
			delete(cache, k)
		}
	}
}

// Shelly's kvs_rev (system.Status.KvsRevision) counts every successful KVS
// write on a device, including ones this process didn't make (a script
// running on the device, the Shelly app, another myhome process). Track it
// per device so an externally-caused bump can invalidate that device's whole
// cache, while our own writes (already precisely invalidated by
// SetKeyValue/DeleteKey) don't trigger a redundant full wipe.
var (
	revMu       sync.Mutex
	lastSeenRev = map[string]uint32{}
	pendingSelf = map[string]uint32{}
)

// noteSelfWrite records a KVS write this process just made on a device, so a
// matching kvs_rev bump reported later by ObserveRevision can be recognized
// as self-caused rather than external.
func noteSelfWrite(deviceId string) {
	revMu.Lock()
	defer revMu.Unlock()
	pendingSelf[deviceId]++
}

// ObserveRevision reports a device's current kvs_rev counter, as seen in a
// NotifyStatus/NotifyFullStatus "sys" update. Assumes kvs_rev increments by
// exactly one per successful write (mirroring cfg_rev's documented
// behavior). If it moved by more than our own pending writes account for,
// something else changed a KVS entry on this device, so the entire
// per-device cache is dropped.
func ObserveRevision(log logr.Logger, deviceId string, rev uint32) {
	revMu.Lock()
	defer revMu.Unlock()

	prev, known := lastSeenRev[deviceId]
	lastSeenRev[deviceId] = rev
	if !known || rev == prev {
		return
	}

	delta := rev - prev
	if delta <= pendingSelf[deviceId] {
		pendingSelf[deviceId] -= delta
		log.V(1).Info("KVS revision bump matches our own writes, cache kept", "device", deviceId, "prev_rev", prev, "rev", rev)
		return
	}
	pendingSelf[deviceId] = 0
	log.Info("KVS revision changed externally, invalidating device cache", "device", deviceId, "prev_rev", prev, "rev", rev)
	cacheInvalidateDevice(deviceId)
}
