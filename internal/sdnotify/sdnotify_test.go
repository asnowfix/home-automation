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
	if d := WatchdogInterval(); d != 10*time.Second {
		t.Fatalf("got %v, want 10s", d)
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

func TestFailureBudget(t *testing.T) {
	g := &failureBudget{max: 3}
	// One failed probe never withholds a pet; the third consecutive one does.
	for i, tc := range []struct{ ok, pet bool }{
		{false, true}, {true, true}, {false, true}, {false, true}, {false, false}, {false, false}, {true, true},
	} {
		if got := g.pet(tc.ok); got != tc.pet {
			t.Fatalf("step %d (ok=%v): pet=%v, want %v", i, tc.ok, got, tc.pet)
		}
	}
}

func TestHeartbeatSurvivesOneFailedCheck(t *testing.T) {
	got, path := listen(t)
	t.Setenv("NOTIFY_SOCKET", path)

	var calls atomic.Int32
	check := func(context.Context) error {
		if calls.Add(1) == 1 {
			return errors.New("one failed probe")
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Heartbeat(ctx, logr.Discard(), 40*time.Millisecond, 3, func() []Check { return []Check{check} })

	// The very first beat fails its check and must still pet.
	select {
	case msg := <-got:
		if msg != "WATCHDOG=1" || calls.Load() > 1 {
			t.Fatalf("got %q after %d checks; want a pet from the failing first beat", msg, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("a single failed probe withheld the heartbeat")
	}
}

func TestHeartbeatWithheldAfterBudget(t *testing.T) {
	got, path := listen(t)
	t.Setenv("NOTIFY_SOCKET", path)
	check := func(context.Context) error { return errors.New("down") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Heartbeat(ctx, logr.Discard(), 20*time.Millisecond, 3, func() []Check { return []Check{check} })

	time.Sleep(300 * time.Millisecond)
	if n := len(got); n != 2 {
		t.Fatalf("pets while permanently failing = %d, want exactly 2 (budget 3)", n)
	}
}

func TestHeartbeatAbandonsHungCheck(t *testing.T) {
	got, path := listen(t)
	t.Setenv("NOTIFY_SOCKET", path)
	release := make(chan struct{})
	defer close(release)
	check := func(context.Context) error { <-release; return nil } // ignores ctx, like a blocking Publish
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Heartbeat(ctx, logr.Discard(), 20*time.Millisecond, 1, func() []Check { return []Check{check} })
	time.Sleep(150 * time.Millisecond)
	if n := len(got); n != 0 {
		t.Fatalf("hung check still petted %d times", n)
	}
}
