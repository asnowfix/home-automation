package roomba

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func TestBeforeRetryRunsAfterAFailedAttemptOnly(t *testing.T) {
	c := NewClient(logr.Discard(), testBLID, "pw", "192.0.2.50", nil)
	var mu sync.Mutex
	var events []string
	attempts := 0
	c.dial = func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		events = append(events, "dial")
		if attempts < 3 {
			return errors.New("connection refused")
		}
		return nil
	}
	c.BeforeRetry = func(ctx context.Context) {
		mu.Lock()
		events = append(events, "wake")
		mu.Unlock()
	}
	if err := c.Connect(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	want := []string{"dial", "wake", "dial", "wake", "dial"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

func TestBeforeRetryNotCalledOnSuccess(t *testing.T) {
	c := NewClient(logr.Discard(), testBLID, "pw", "192.0.2.50", nil)
	c.dial = func(ctx context.Context) error { return nil }
	called := false
	c.BeforeRetry = func(ctx context.Context) { called = true }
	if err := c.Connect(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("wake probe ran although the first connect succeeded")
	}
}

func TestEchoRequestChecksum(t *testing.T) {
	b := echoRequest(0x1234, 7)
	if b[0] != 8 || b[1] != 0 {
		t.Fatalf("not an echo request: % x", b)
	}
	// A correct checksum makes the one's-complement sum of the message 0xffff.
	if icmpChecksum(b) != 0 {
		t.Fatalf("checksum does not verify: % x", b)
	}
	reply := append([]byte(nil), b...)
	reply[0] = 0
	if id, seq, ok := parseEchoReply(reply); !ok || id != 0x1234 || seq != 7 {
		t.Fatalf("parse = %x %d %v", id, seq, ok)
	}
	if _, _, ok := parseEchoReply(b); ok {
		t.Fatal("an echo request parsed as a reply")
	}
}

func TestWakeRejectsNonIPv4(t *testing.T) {
	if _, err := Wake(context.Background(), "roomba.local"); err == nil {
		t.Fatal("a hostname was accepted")
	}
}

// TestWakeLoopback runs the real probe against 127.0.0.1 (no traffic leaves the
// machine). It needs root or CAP_NET_RAW, so it skips otherwise.
func TestWakeLoopback(t *testing.T) {
	start := time.Now()
	res, err := Wake(context.Background(), "127.0.0.1")
	if errors.Is(err, ErrWakeNotPermitted) {
		t.Skip("no raw ICMP socket here (needs root or CAP_NET_RAW)")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replied || res.Sent < 1 || res.Sent > wakeCount {
		t.Fatalf("result = %+v", res)
	}
	if d := time.Since(start); d > WakeBudget+time.Second {
		t.Fatalf("wake took %v, bound is %v", d, WakeBudget)
	}
}
