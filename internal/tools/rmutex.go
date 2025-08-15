package tools

import (
	"context"
	"sync"
)

type tokenKey struct{}
type Token struct{}

func WithToken(ctx context.Context) context.Context {
	t := &Token{}
	return context.WithValue(ctx, tokenKey{}, t)
}

func tokenFrom(ctx context.Context) *Token {
	if v, ok := ctx.Value(tokenKey{}).(*Token); ok {
		return v
	}
	// Auto-provision a token if caller didn't add one.
	// Prefer WithToken at call-site so the same token is shared across calls.
	t := &Token{}
	return t
}

type ReentrantMutex struct {
	mu    sync.Mutex // the underlying exclusion
	state sync.Mutex // protects owner/rec
	owner *Token
	rec   int
}

// Lock blocks unless held by the same token (re-entrant).
func (rm *ReentrantMutex) Lock(ctx context.Context) {
	tok := tokenFrom(ctx)

	// Fast-path: already owned by this token?
	rm.state.Lock()
	if rm.owner == tok {
		rm.rec++
		rm.state.Unlock()
		return
	}
	rm.state.Unlock()

	// Acquire underlying lock, then become owner.
	rm.mu.Lock()
	rm.state.Lock()
	rm.owner = tok
	rm.rec = 1
	rm.state.Unlock()
}

func (rm *ReentrantMutex) Unlock(ctx context.Context) {
	tok := tokenFrom(ctx)

	rm.state.Lock()
	defer rm.state.Unlock()

	if rm.owner != tok || rm.rec == 0 {
		panic("rmutex: unlock by non-owner")
	}
	rm.rec--
	if rm.rec == 0 {
		rm.owner = nil
		rm.mu.Unlock()
	}
}

// Optional: TryLock returns true on success.
func (rm *ReentrantMutex) TryLock(ctx context.Context) bool {
	tok := tokenFrom(ctx)

	rm.state.Lock()
	if rm.owner == tok {
		rm.rec++
		rm.state.Unlock()
		return true
	}
	rm.state.Unlock()

	locked := rm.mu.TryLock() // Go 1.18+ (mu.TryLock added in 1.18? If not, emulate.)
	if !locked {
		return false
	}
	rm.state.Lock()
	rm.owner = tok
	rm.rec = 1
	rm.state.Unlock()
	return true
}
