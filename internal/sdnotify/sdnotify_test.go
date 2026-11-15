package sdnotify

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func listen(t *testing.T) (chan string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notify.sock")
	conn, err := net.ListenPacket("unixgram", path)
	if err != nil {
		t.Skipf("unixgram unsupported: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	got := make(chan string, 64)
	go func() {
		buf := make([]byte, 256)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			got <- string(buf[:n])
		}
	}()
	return got, path
}

func TestNotifyNoSocketIsNoop(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	sent, err := Notify("READY=1")
	if sent || err != nil {
		t.Fatalf("Notify = %v, %v; want false, nil", sent, err)
	}
}

func TestNotifySends(t *testing.T) {
	got, path := listen(t)
	t.Setenv("NOTIFY_SOCKET", path)
	if err := Ready(); err != nil {
		t.Fatal(err)
	}
	if msg := <-got; msg != "READY=1" {
		t.Fatalf("got %q", msg)
	}
}

func TestWatchdogInterval(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "60000000")
	t.Setenv("WATCHDOG_PID", "")
	if d := WatchdogInterval(); d != 30*time.Second {
		t.Fatalf("got %v, want 30s", d)
	}
	t.Setenv("WATCHDOG_PID", "1")
	if d := WatchdogInterval(); d != 0 {
		t.Fatalf("foreign WATCHDOG_PID: got %v, want 0", d)
	}
	t.Setenv("WATCHDOG_PID", "")
	t.Setenv("WATCHDOG_USEC", "")
	if d := WatchdogInterval(); d != 0 {
		t.Fatalf("unset: got %v, want 0", d)
	}
}

func TestHeartbeatGatedOnChecks(t *testing.T) {
	got, path := listen(t)
	t.Setenv("NOTIFY_SOCKET", path)

	var failing atomic.Bool
	failing.Store(true)
	check := func(context.Context) error {
		if failing.Load() {
			return errors.New("down")
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Heartbeat(ctx, logr.Discard(), 20*time.Millisecond, func() []Check { return []Check{check} })

	select {
	case msg := <-got:
		t.Fatalf("heartbeat %q sent while check failing", msg)
	case <-time.After(100 * time.Millisecond):
	}
	failing.Store(false)
	select {
	case msg := <-got:
		if msg != "WATCHDOG=1" {
			t.Fatalf("got %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no heartbeat after check recovered")
	}
}
