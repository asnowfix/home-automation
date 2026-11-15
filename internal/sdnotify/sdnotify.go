// Package sdnotify speaks the small part of systemd's sd_notify protocol the
// daemon needs (READY, WATCHDOG, STOPPING), without a dependency: a state
// string is one datagram written to the unix socket named by $NOTIFY_SOCKET.
//
// Everything is a no-op when NOTIFY_SOCKET is unset (running from a shell,
// on macOS, on Windows), so callers never need to check the platform.
package sdnotify

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
)

// Notify sends one state string (e.g. "READY=1"). It returns false and a nil
// error when no notify socket is configured.
func Notify(state string) (bool, error) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return false, nil
	}
	// An abstract socket is spelled "@name" in the environment, "\0name" on the wire.
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.Dial("unixgram", addr)
	if err != nil {
		return false, fmt.Errorf("sd_notify dial: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(state)); err != nil {
		return false, fmt.Errorf("sd_notify write: %w", err)
	}
	return true, nil
}

// Ready tells systemd that start-up is finished (Type=notify).
func Ready() error { _, err := Notify("READY=1"); return err }

// Stopping tells systemd that shutdown has begun.
func Stopping() error { _, err := Notify("STOPPING=1"); return err }

// Watchdog pets the systemd watchdog.
func Watchdog() error { _, err := Notify("WATCHDOG=1"); return err }

// WatchdogInterval returns how often to pet the watchdog: a sixth of the
// WatchdogSec systemd advertises in $WATCHDOG_USEC. sd_watchdog_enabled(3)
// suggests half, but with a pet every half-deadline one withheld or late pet
// already risks the deadline; a sixth leaves room for several (see Heartbeat).
// Zero means the watchdog is not enabled for this process.
func WatchdogInterval() time.Duration {
	usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec <= 0 {
		return 0
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0
	}
	return time.Duration(usec) * time.Microsecond / 6
}

// Check reports whether one part of the daemon is alive. It must honour ctx.
type Check func(ctx context.Context) error

// failureBudget tolerates up to max-1 consecutive failed beats: a pet is
// withheld only from the max-th consecutive failure on. One transient failure
// (the MQTT client re-subscribing during its periodic reconnect, a slow
// broker) must not cost the daemon its life.
type failureBudget struct{ fails, max int }

func (b *failureBudget) pet(ok bool) bool {
	if ok {
		b.fails = 0
		return true
	}
	b.fails++
	return b.fails < b.max
}

// runCheck runs a check, but gives up on it when ctx expires even if the check
// itself ignores ctx (an MQTT publish waits on its own timeout, not ours). The
// abandoned goroutine ends when the check does.
func runCheck(ctx context.Context, check Check) error {
	res := make(chan error, 1)
	go func() { res <- check(ctx) }()
	select {
	case err := <-res:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Heartbeat pets the watchdog every interval, unless the checks have failed
// maxFailures beats in a row, so systemd restarts a daemon that is running but
// not working. interval <= 0 disables it. Each beat gives its checks half the
// interval to answer.
//
// Tolerance with the daemon's settings (WatchdogSec=60, interval 10 s,
// maxFailures 3): a single failed probe never misses a pet. A permanent
// failure pets at the 1st and 2nd failed beats, stops at the 3rd, and systemd
// acts WatchdogSec after the last pet: about 80 to 85 s after the last good
// probe.
//
// Checks are re-read on every beat through the checks func, so the alert
// evaluator (#712) can register its own later.
func Heartbeat(ctx context.Context, log logr.Logger, interval time.Duration, maxFailures int, checks func() []Check) {
	if interval <= 0 {
		return
	}
	budget := &failureBudget{max: maxFailures}
	beat := func() {
		cctx, cancel := context.WithTimeout(ctx, interval/2)
		defer cancel()
		ok := true
		for i, check := range checks() {
			if err := runCheck(cctx, check); err != nil {
				log.Error(err, "Health check failed", "check", i, "consecutive_failed_beats", budget.fails+1)
				ok = false
				break
			}
		}
		if !budget.pet(ok) {
			log.Error(nil, "Health checks keep failing, withholding watchdog heartbeat", "max_failures", maxFailures)
			return
		}
		if err := Watchdog(); err != nil {
			log.Error(err, "Watchdog heartbeat failed")
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	beat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			beat()
		}
	}
}
