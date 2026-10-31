package vacuum

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/roomba"
	"github.com/go-logr/logr"
)

func wakeService(cfg Config, wake func(ctx context.Context, host string) (roomba.WakeResult, error)) *Service {
	return &Service{ctx: context.Background(), log: logr.Discard(), cfg: cfg, wake: wake}
}

func TestWakeProbeOffByDefault(t *testing.T) {
	s := wakeService(Config{BLID: blid, Password: "pw"}, nil)
	if s.beforeRetry("192.0.2.50") != nil {
		t.Fatal("wake probe enabled without MYHOME_VACUUM_WAKE")
	}
}

func TestWakeProbeTargetsTheRobot(t *testing.T) {
	var got string
	var n atomic.Int32
	s := wakeService(Config{BLID: blid, Password: "pw", Wake: true}, func(ctx context.Context, host string) (roomba.WakeResult, error) {
		got = host
		n.Add(1)
		return roomba.WakeResult{Sent: 1, Replied: true, RTT: 5 * time.Millisecond}, nil
	})
	hook := s.beforeRetry("192.0.2.50")
	if hook == nil {
		t.Fatal("no hook with MYHOME_VACUUM_WAKE set")
	}
	hook(context.Background())
	hook(context.Background())
	if got != "192.0.2.50" || n.Load() != 2 {
		t.Fatalf("probed %q %d times", got, n.Load())
	}
}

func TestWakeProbeIsBounded(t *testing.T) {
	saved := wakeBudget
	wakeBudget = 100 * time.Millisecond
	t.Cleanup(func() { wakeBudget = saved })
	s := wakeService(Config{Wake: true}, func(ctx context.Context, host string) (roomba.WakeResult, error) {
		<-ctx.Done() // a robot that never answers
		return roomba.WakeResult{Sent: 3}, nil
	})
	start := time.Now()
	s.beforeRetry("192.0.2.50")(context.Background())
	if d := time.Since(start); d > time.Second {
		t.Fatalf("wake probe held the retry for %v (bound %v)", d, wakeBudget)
	}
}

func TestWakeProbeStopsWhenNotPermitted(t *testing.T) {
	var n atomic.Int32
	s := wakeService(Config{Wake: true}, func(ctx context.Context, host string) (roomba.WakeResult, error) {
		n.Add(1)
		return roomba.WakeResult{}, roomba.ErrWakeNotPermitted
	})
	hook := s.beforeRetry("192.0.2.50")
	hook(context.Background())
	hook(context.Background())
	if n.Load() != 1 {
		t.Fatalf("probe tried %d times without the privilege, want 1", n.Load())
	}
}

// TestProductionClientGetsTheHook checks NewService wires the probe into the
// real roomba.Client it builds. That the client runs the hook after each failed
// attempt and before the next, and not after a success, is tested where the
// retry loop lives (pkg/roomba, TestBeforeRetryRunsAfterAFailedAttemptOnly).
func TestProductionClientGetsTheHook(t *testing.T) {
	for _, on := range []bool{false, true} {
		s, err := NewService(context.Background(), logr.Discard(), nil, Config{Wake: on}) // not Enabled: no connection
		if err != nil {
			t.Fatal(err)
		}
		c, ok := s.newClient(logr.Discard(), blid, "pw", "192.0.2.50", nil).(*roomba.Client)
		if !ok {
			t.Fatal("production client is not a *roomba.Client")
		}
		if (c.BeforeRetry != nil) != on {
			t.Fatalf("MYHOME_VACUUM_WAKE=%v but hook set = %v", on, c.BeforeRetry != nil)
		}
	}
}
