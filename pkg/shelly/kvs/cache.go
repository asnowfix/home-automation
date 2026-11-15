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
// only a safety net, not the primary invalidation path. This also bounds
// "key not found" results: a device simply not having a given KVS key
// configured (common — not every switch has "normally-closed" set) is a
// stable fact, not a transient failure, so it's as cachable as a real value —
// ObserveRevision already invalidates it immediately if the key is later set.
// Before this, a failed Get was never cached at all, so it paid a live RPC
// round trip on every single request, forever.
const ttl = 5 * time.Minute

type cacheEntry struct {
	value    GetResponse
	notFound bool
	// definitive is meaningful only with notFound: true when the device
	// itself answered "not found", false when the lookup merely failed
	// (timeout, transport error, an unrecognised device error). Both are
	// cached as misses, but only the first may be replayed as ErrNotFound —
	// otherwise a transient failure would later read back as a fact about the
	// key (#663).
	definitive bool
	expires    time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func cacheKey(deviceId, key string) string {
	return deviceId + "\x00" + key
}

func cacheGet(deviceId, key string) (cacheEntry, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := cache[cacheKey(deviceId, key)]
	if !ok || time.Now().After(e.expires) {
		return cacheEntry{}, false
	}
	return e, true
}

func cachePut(deviceId, key string, value GetResponse) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[cacheKey(deviceId, key)] = cacheEntry{value: value, expires: time.Now().Add(ttl)}
}

func cachePutNotFound(deviceId, key string, definitive bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	k := cacheKey(deviceId, key)
	// A failed lookup says nothing about the key: do not let it erase a fact the
	// device already answered (a value, or a definitive "not found"). That entry
	// stays until SetKeyValue/DeleteKey/kvs_rev invalidates it; Peek ignores its
	// expiry, so a stale-but-known polarity keeps rendering.
	if old, ok := cache[k]; ok && !definitive && (!old.notFound || old.definitive) {
		return
	}
	cache[k] = cacheEntry{notFound: true, definitive: definitive, expires: time.Now().Add(ttl)}
}

// PeekState is what Peek knows about a key without contacting the device.
type PeekState int

const (
	PeekUnknown PeekState = iota // nothing cached, or an earlier lookup failed transiently
	PeekFound                    // the device answered with a value
	PeekAbsent                   // the device answered that the key does not exist
)

// Peek reads the cache only and never calls the device, so it is safe on a
// request path (#732). Unlike GetValue it ignores the TTL: an entry stays valid
// until SetKeyValue/DeleteKey or a kvs_rev change invalidates it, and a later
// GetValue (the device refresh) renews it. Returns PeekUnknown when there is
// nothing trustworthy to report.
func Peek(deviceId, key string) (GetResponse, PeekState) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := cache[cacheKey(deviceId, key)]
	switch {
	case !ok:
		return GetResponse{}, PeekUnknown
	case !e.notFound:
		return e.value, PeekFound
	case e.definitive:
		return GetResponse{}, PeekAbsent
	}
	return GetResponse{}, PeekUnknown
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
//
// It returns true when the device's cache was dropped, so the caller can re-read
// what it depends on (off the calling goroutine) instead of waiting for the next
// refresh.
func ObserveRevision(log logr.Logger, deviceId string, rev uint32) (invalidated bool) {
	revMu.Lock()
	defer revMu.Unlock()

	prev, known := lastSeenRev[deviceId]
	lastSeenRev[deviceId] = rev
	if !known || rev == prev {
		return false
	}

	delta := rev - prev
	if delta <= pendingSelf[deviceId] {
		pendingSelf[deviceId] -= delta
		log.V(1).Info("KVS revision bump matches our own writes, cache kept", "device", deviceId, "prev_rev", prev, "rev", rev)
		return false
	}
	pendingSelf[deviceId] = 0
	log.Info("KVS revision changed externally, invalidating device cache", "device", deviceId, "prev_rev", prev, "rev", rev)
	cacheInvalidateDevice(deviceId)
	return true
}
